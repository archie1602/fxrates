package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"fxrates/internal/config"
	"fxrates/internal/provider/frankfurter"
	"fxrates/internal/service"
	postgresstore "fxrates/internal/storage/postgres"
	"fxrates/internal/transport/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("application stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}

	databaseCtx, cancelDatabase := context.WithTimeout(context.Background(), cfg.DatabaseTimeout)
	defer cancelDatabase()

	database, err := pgxpool.New(databaseCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("configure PostgreSQL pool: %w", err)
	}
	defer database.Close()

	if err := database.Ping(databaseCtx); err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	cancelDatabase()

	updateRepository, err := postgresstore.NewQuoteUpdateRepository(database, cfg.DatabaseQueryTimeout)
	if err != nil {
		return fmt.Errorf("configure quote update repository: %w", err)
	}
	timeProvider := service.SystemTimeProvider{}

	rateProvider, err := frankfurter.New(
		&http.Client{Timeout: cfg.FrankfurterTimeout},
		cfg.FrankfurterBaseURL,
		frankfurter.RetryPolicy{
			MaxAttempts: cfg.FrankfurterMaxAttempts,
			Delay:       cfg.FrankfurterRetryDelay,
		},
	)
	if err != nil {
		return fmt.Errorf("configure Frankfurter client: %w", err)
	}

	quoteService := service.NewQuoteService(
		updateRepository,
		timeProvider,
		service.UUIDv7Generator{},
	)
	quoteUpdateWorker, err := service.NewQuoteUpdateWorker(
		updateRepository,
		rateProvider,
		timeProvider,
		logger,
		cfg.WorkerPollInterval,
	)
	if err != nil {
		return fmt.Errorf("configure quote update worker: %w", err)
	}
	quoteUpdateRecoveryWorker, err := service.NewQuoteUpdateRecoveryWorker(
		updateRepository,
		timeProvider,
		logger,
		cfg.RecoveryInterval,
		cfg.ProcessingTimeout,
	)
	if err != nil {
		return fmt.Errorf("configure quote update recovery worker: %w", err)
	}

	server := httpapi.NewServer(
		cfg.HTTPAddr,
		cfg.ShutdownTimeout,
		quoteService,
		updateRepository,
		logger,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return server.Run(groupCtx)
	})
	group.Go(func() error {
		return quoteUpdateWorker.Run(groupCtx)
	})
	group.Go(func() error {
		return quoteUpdateRecoveryWorker.Run(groupCtx)
	})

	if err := group.Wait(); err != nil {
		return fmt.Errorf("run application: %w", err)
	}

	return nil
}
