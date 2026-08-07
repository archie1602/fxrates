package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewQuoteUpdateRepositoryValidatesDependencies(t *testing.T) {
	t.Run("requires database", func(t *testing.T) {
		if _, err := NewQuoteUpdateRepository(nil, time.Second); err == nil {
			t.Fatal("NewQuoteUpdateRepository returned nil error for a nil database")
		}
	})

	t.Run("requires positive query timeout", func(t *testing.T) {
		if _, err := NewQuoteUpdateRepository(new(pgxpool.Pool), 0); err == nil {
			t.Fatal("NewQuoteUpdateRepository returned nil error for a zero query timeout")
		}
	})

	t.Run("accepts valid dependencies", func(t *testing.T) {
		repository, err := NewQuoteUpdateRepository(new(pgxpool.Pool), time.Second)
		if err != nil {
			t.Fatalf("NewQuoteUpdateRepository returned unexpected error: %v", err)
		}
		if repository.queryTimeout != time.Second {
			t.Errorf("query timeout = %v, want %v", repository.queryTimeout, time.Second)
		}
	})
}

func TestQueryContextUsesConfiguredTimeout(t *testing.T) {
	timeout := time.Minute
	repository := &QuoteUpdateRepository{queryTimeout: timeout}
	startedAt := time.Now()

	ctx, cancel := repository.queryContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("query context does not have a deadline")
	}
	if deadline.Before(startedAt.Add(timeout)) || deadline.After(time.Now().Add(timeout)) {
		t.Errorf("query deadline = %v, want approximately %v after start", deadline, timeout)
	}
}
