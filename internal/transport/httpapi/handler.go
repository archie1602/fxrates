package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"fxrates/internal/domain"
	"fxrates/internal/service"
)

type Handler struct {
	service   QuoteUpdateRequester
	readiness ReadinessChecker
	logger    *slog.Logger
}

func NewHandler(
	service QuoteUpdateRequester,
	readiness ReadinessChecker,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		service:   service,
		readiness: readiness,
		logger:    logger,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)

	mux.HandleFunc("POST /api/v1/quote-updates", h.createQuoteUpdate)
	mux.HandleFunc("GET /api/v1/quote-updates/{id}", h.getQuoteUpdate)
	mux.HandleFunc("GET /api/v1/quotes/latest", h.getLatest)

	return recoverMiddleware(h.logger, mux)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	if err := h.readiness.Ready(r.Context()); err != nil {
		h.logger.Warn("readiness check failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "not_ready", "service is not ready")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) createQuoteUpdate(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}

	idempotencyKey, err := parseIdempotencyKey(r.Header)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			"invalid_idempotency_key",
			"Idempotency-Key must contain one non-zero UUID",
		)
		return
	}

	var request createQuoteUpdateRequestDTO
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must contain one valid JSON object")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must contain one valid JSON object")
		return
	}

	pair, err := domain.ParsePair(request.Pair)
	if err != nil {
		code := "invalid_currency_pair"
		if errors.Is(err, domain.ErrUnsupportedCurrency) {
			code = "unsupported_currency"
		}
		writeError(w, http.StatusBadRequest, code, err.Error())
		return
	}

	update, err := h.service.CreateQuoteUpdate(r.Context(), pair, idempotencyKey)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.Header().Set("Location", "/api/v1/quote-updates/"+update.ID.String())
	writeJSON(w, http.StatusAccepted, mapUpdateResponseToDTO(update))
}

func parseIdempotencyKey(header http.Header) (*uuid.UUID, error) {
	values := header.Values("Idempotency-Key")
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) != 1 {
		return nil, errors.New("multiple Idempotency-Key values")
	}

	key, err := uuid.Parse(strings.TrimSpace(values[0]))
	if err != nil || key == uuid.Nil {
		return nil, errors.New("invalid Idempotency-Key")
	}

	return &key, nil
}

func (h *Handler) getQuoteUpdate(w http.ResponseWriter, r *http.Request) {
	updateID, err := uuid.Parse(strings.TrimSpace(r.PathValue("id")))
	if err != nil || updateID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "invalid_update_id", "update id must be a valid UUID")
		return
	}

	result, err := h.service.GetQuoteUpdate(r.Context(), updateID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, mapQuoteUpdateResultToDTO(result))
}

func (h *Handler) getLatest(w http.ResponseWriter, r *http.Request) {
	pair, err := domain.ParsePair(r.URL.Query().Get("pair"))
	if err != nil {
		code := "invalid_currency_pair"
		if errors.Is(err, domain.ErrUnsupportedCurrency) {
			code = "unsupported_currency"
		}
		writeError(w, http.StatusBadRequest, code, err.Error())
		return
	}

	quote, err := h.service.GetLatest(r.Context(), pair)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, mapQuoteToDTO(quote))
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrIdempotencyKeyConflict) {
		writeError(
			w,
			http.StatusUnprocessableEntity,
			"idempotency_key_conflict",
			service.ErrIdempotencyKeyConflict.Error(),
		)
		return
	}

	if errors.Is(err, service.ErrQuoteUpdateNotFound) {
		writeError(w, http.StatusNotFound, "quote_update_not_found", err.Error())
		return
	}

	if errors.Is(err, service.ErrQuoteNotFound) {
		writeError(w, http.StatusNotFound, "quote_not_found", err.Error())
		return
	}

	h.logger.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}
