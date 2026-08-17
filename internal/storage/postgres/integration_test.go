package postgres_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	storagepostgres "fxrates/internal/storage/postgres"
)

const integrationQueryTimeout = 3 * time.Second

var integrationDB *pgxpool.Pool

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(m.Run())
	}

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		os.Exit(m.Run())
	}

	os.Exit(runIntegrationTests(m, databaseURL))
}

func runIntegrationTests(m *testing.M, databaseURL string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "create PostgreSQL integration pool: %v\n", err)
		return 1
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "connect to PostgreSQL integration database: %v\n", err)
		return 1
	}

	var databaseName string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "read PostgreSQL integration database name: %v\n", err)
		return 1
	}
	if !strings.HasSuffix(databaseName, "_test") {
		_, _ = fmt.Fprintf(
			os.Stderr,
			"refusing to run PostgreSQL integration tests against database %q: name must end with _test\n",
			databaseName,
		)
		return 1
	}

	integrationDB = pool
	testCode := m.Run()

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	if _, err := pool.Exec(cleanupCtx, "TRUNCATE TABLE quote_updates CASCADE"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "clean PostgreSQL integration database: %v\n", err)
		if testCode == 0 {
			return 1
		}
	}

	return testCode
}

func integrationRepository(t *testing.T) *storagepostgres.QuoteUpdateRepository {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	if integrationDB == nil {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := integrationDB.Exec(ctx, "TRUNCATE TABLE quote_updates CASCADE"); err != nil {
		t.Fatalf("reset PostgreSQL integration database: %v", err)
	}

	repository, err := storagepostgres.NewQuoteUpdateRepository(
		integrationDB,
		integrationQueryTimeout,
	)
	if err != nil {
		t.Fatalf("create PostgreSQL integration repository: %v", err)
	}

	return repository
}
