# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`AGENTS.md` holds the contribution conventions (structure, coding style, commit/PR expectations). This file covers the architecture and the invariants that are only visible when reading several files together.

## Commands

```bash
make test                                  # go test -short ./...
make check                                 # fast CI checks, including short race tests
TEST_DATABASE_URL=postgres://.../fxrates_test?sslmode=disable make test-integration
go test ./internal/service -run TestQuoteUpdateWorkerProcessesPendingUpdate -v   # single test
make vet                                   # go vet ./...
make fmt                                   # gofmt -w over all non-vendor Go files
make build                                 # -> bin/fxrates
make run                                   # go run ./cmd/api (needs DATABASE_URL exported)

make docker-up                             # postgres + migrate + app; API on :8080, PG on :15432
make docker-logs                           # docker compose logs -f app
make docker-down                           # add -v manually to drop the volume

make migrate-create NAME=add_field         # golang-migrate, -seq numbering
make migrate-up                            # requires DATABASE_URL and the golang-migrate CLI

npx --yes @redocly/cli@2.46.0 lint openapi.yaml --skip-rule info-license-strict --skip-rule no-server-example.com
```

CI (`.github/workflows/ci.yml`) additionally fails on any file that `gofmt -l` reports, so run `make fmt` before pushing.

## Request lifecycle

`POST /api/v1/quote-updates` never calls the upstream provider. It validates, inserts a `pending` row, and returns `202` with the id. Everything else happens in background goroutines started alongside the HTTP server by an `errgroup` in `cmd/api/main.go`:

1. `QuoteUpdateWorker` calls `TakeNextPendingUpdate`, a single CTE that claims one row with `FOR UPDATE SKIP LOCKED` and flips it to `processing`. Multiple instances can share the queue because of this.
2. The Frankfurter call happens **after** that short transaction has committed — never hold a database transaction across the HTTP call.
3. Every claim increments `processing_version`, which is returned to the worker as a fencing token. `CompleteUpdate` writes the `exchange_rates` row and flips the status in one statement; both completion and failure require the row to still be `processing` with the claimed version. A zero affected-row count means the lease is stale, so the worker discards its result instead of overwriting a newer claim.
4. `QuoteUpdateRecoveryWorker` moves `processing` rows older than `PROCESSING_TIMEOUT` back to `pending`.

This is at-least-once on purpose: a crash between the provider call and `CompleteUpdate` re-runs the provider `GET`. PostgreSQL is deliberately used as the durable queue instead of a Go channel or a broker — see the README for the reasoning. Keep this shape when changing the workers.

**Config invariant:** `config.Validate` rejects a `PROCESSING_TIMEOUT` that does not exceed `FRANKFURTER_MAX_ATTEMPTS × FRANKFURTER_TIMEOUT` plus the maximum delays between attempts (`processingTimeoutCoversRetries`). The fencing token prevents stale writes, while this timeout still avoids needlessly reclaiming active work. Touching any retry or processing-timeout setting means re-checking that arithmetic.

## Dependency direction

`domain` depends on nothing. `service` owns the interfaces it consumes — `QuoteUpdateRepository`, `QuoteUpdateProcessorRepository`, `QuoteUpdateRecoveryRepository`, `RateProvider`, `TimeProvider`, `UUIDGenerator` — and `storage/postgres` and `provider/frankfurter` implement them, asserted at compile time with `var _ service.X = (*T)(nil)` at the bottom of each file. `transport/httpapi` declares its own consumer interfaces (`QuoteUpdateRequester`, `ReadinessChecker`) rather than importing the concrete service type. New collaborators follow the same pattern: declare the interface where it is used, keep `domain` and `service` free of `net/http` and `pgx`.

## Domain invariants

- `domain.Rate` is a validated decimal **string**, never a float. `ParseRate` enforces what `numeric(30, 12)` can hold exactly, and it is applied in both directions — on values coming from Frankfurter and on values read back out of PostgreSQL.
- `domain.ParsePair` normalizes and restricts the pair to `USD`/`EUR`/`MXN`. The `quote_updates` CHECK constraints mirror the format independently. Adding a currency means updating `internal/domain/pair.go`, `openapi.yaml`, and the README together.
- Service-level failures are sentinel errors (`ErrQuoteUpdateNotFound`, `ErrQuoteNotFound`, `ErrIdempotencyKeyConflict`) mapped to status codes in one place, `Handler.writeServiceError`. A new failure mode needs a sentinel, a case there, and an `openapi.yaml` entry; everything unmapped becomes a logged `500`.
- Idempotency lives in `CreateOrGet`: `ON CONFLICT (idempotency_key) DO NOTHING ... RETURNING`, then a fallback select on conflict. The service compares the stored pair with the requested one and returns `ErrIdempotencyKeyConflict` (→ 422) if they differ.
- Every repository method wraps the caller's context with `DATABASE_QUERY_TIMEOUT` via `queryContext`.

## Tests

PostgreSQL integration tests run against a real database created from `migrations/`; `TEST_DATABASE_URL` must point to a disposable database whose name ends with `_test`. The suite truncates shared tables between scenarios, so its tests must not call `t.Parallel`. Unit tests use hand-written stubs rather than a mocking library: stub `TimeProvider` and `UUIDGenerator` for determinism, `httptest.NewServer` for the Frankfurter client, and `httptest.NewRecorder` for handlers. Keep new tests in that style.

## Keep in sync

A change to the HTTP surface touches `openapi.yaml` and `api.http`; a change to configuration touches `.env.example` and the `app` service environment in `compose.yaml`; a migration always ships both `*.up.sql` and `*.down.sql`.
