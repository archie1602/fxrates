package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"fxrates/internal/domain"
)

type updateResponseDTO struct {
	ID        string              `json:"update_id"`
	Pair      domain.Pair         `json:"pair"`
	Status    domain.UpdateStatus `json:"status"`
	CreatedAt time.Time           `json:"created_at"`
}

type quoteUpdateResponseDTO struct {
	UpdateID     string              `json:"update_id"`
	Pair         domain.Pair         `json:"pair"`
	Status       domain.UpdateStatus `json:"status"`
	UpdatedAt    time.Time           `json:"updated_at"`
	ErrorMessage *string             `json:"error_message,omitempty"`
	Rate         *domain.Rate        `json:"rate"`
	RateDate     *string             `json:"rate_date"`
	FetchedAt    *time.Time          `json:"fetched_at"`
}

type quoteResponseDTO struct {
	UpdateID  string      `json:"update_id"`
	Pair      domain.Pair `json:"pair"`
	Rate      domain.Rate `json:"rate"`
	RateDate  string      `json:"rate_date"`
	FetchedAt time.Time   `json:"fetched_at"`
}

type createQuoteUpdateRequestDTO struct {
	Pair string `json:"pair"` // EUR/MXN
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func mapUpdateResponseToDTO(update domain.QuoteUpdate) updateResponseDTO {
	return updateResponseDTO{
		ID:        update.ID.String(),
		Pair:      update.Pair,
		Status:    update.Status,
		CreatedAt: update.CreatedAt,
	}
}

func mapQuoteUpdateResultToDTO(result domain.QuoteUpdateResult) quoteUpdateResponseDTO {
	response := quoteUpdateResponseDTO{
		UpdateID:  result.Update.ID.String(),
		Pair:      result.Update.Pair,
		Status:    result.Update.Status,
		UpdatedAt: result.Update.UpdatedAt,
	}
	if result.Update.Error != "" {
		response.ErrorMessage = &result.Update.Error
	}

	if result.Quote != nil {
		response.Rate = &result.Quote.Rate
		rateDate := result.Quote.RateDate.Format(time.DateOnly)
		response.RateDate = &rateDate
		response.FetchedAt = &result.Quote.FetchedAt
	}

	return response
}

func mapQuoteToDTO(quote domain.Quote) quoteResponseDTO {
	return quoteResponseDTO{
		UpdateID:  quote.UpdateID.String(),
		Pair:      quote.Pair,
		Rate:      quote.Rate,
		RateDate:  quote.RateDate.Format(time.DateOnly),
		FetchedAt: quote.FetchedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{
		Error: apiError{
			Code:    code,
			Message: message,
		},
	})
}
