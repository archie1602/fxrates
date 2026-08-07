package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

type Server struct {
	httpServer      *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
}

func NewServer(
	addr string,
	shutdownTimeout time.Duration,
	service QuoteUpdateRequester,
	readiness ReadinessChecker,
	logger *slog.Logger,
) *Server {
	handler := NewHandler(service, readiness, logger)

	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           handler.Routes(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
		shutdownTimeout: shutdownTimeout,
		logger:          logger,
	}
}

func (s *Server) Run(ctx context.Context) error {
	serverErr := make(chan error, 1)

	go func() {
		s.logger.Info("HTTP server started", "address", s.httpServer.Addr)
		serverErr <- s.httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()

	s.logger.Info("shutting down HTTP server")
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	err := <-serverErr
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}
