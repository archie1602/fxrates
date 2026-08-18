# Repository Guidelines

## Project Structure & Module Organization

`cmd/api` wires the executable. `internal` is organized by responsibility: `domain` for core models, `service` for use cases and workers, `provider/frankfurter` for the external API, `storage/postgres` for persistence, and `transport/httpapi` for HTTP routes and contracts. Pair SQL changes in `migrations/*.up.sql` and `*.down.sql`. Tests live beside their packages as `*_test.go`; `openapi.yaml` and `api.http` document the public API.

## Architecture Guardrails

`POST /api/v1/quote-updates` must only validate and persist work; external API calls belong in the background worker. Preserve the PostgreSQL queue's at-least-once behavior and `FOR UPDATE SKIP LOCKED` claim mechanism. Keep `PROCESSING_TIMEOUT` longer than the provider's retry window so recovery does not reclaim active work. Keep domain and service independent of HTTP and PostgreSQL.

## Build, Test, and Development Commands

- `make build` compiles `./cmd/api` to `bin/fxrates`.
- `make run` starts the API with the environment.
- `make test` runs fast unit tests; `make fmt` formats Go source.
- `make check` mirrors CI: it verifies modules and OpenAPI, checks formatting, vets, runs unit tests with the race detector, and executes `govulncheck`.
- `make test-integration` applies migrations and runs PostgreSQL integration tests. It requires `TEST_DATABASE_URL` to point to a disposable database whose name ends with `_test`.
- `make docker-up` builds and starts PostgreSQL, migrations, and the API. Inspect them with `make docker-ps` and `make docker-logs`.
- `make migrate-create NAME=add_field` creates a numbered migration. `make migrate-up` requires `DATABASE_URL`.

Copy `.env.example` to `.env`, export it, apply migrations, and run the service. Never commit credentials.

## Coding Style & Naming Conventions

Use the Go version declared in `go.mod` and run `gofmt` before committing. Follow idiomatic Go naming: exported identifiers use `PascalCase`, internal identifiers use `camelCase`, and package names are short and lowercase. Wrap errors with context and preserve sentinel errors for `errors.Is` checks.

## Testing Guidelines

Use Go's `testing` package, table-driven cases, and descriptive `TestFunctionScenario` names. Prefer deterministic stubs for time, UUIDs, repositories, and providers; use `httptest` for HTTP behavior. PostgreSQL integration tests use a real migrated database and must not run in parallel because they reset shared tables between scenarios. Before review, run `make check` and, when changing persistence or migrations, `make test-integration`.

## Changes & Review

Keep commits focused and use short imperative English subjects, for example `add quote validation` or `fix stale worker recovery`. Conventional Commit prefixes such as `feat:`, `fix:`, and `test:` are welcome but optional.

PRs should describe the problem and solution, link an issue when available, and list verification commands. Call out API, configuration, database, or operational effects. Keep `openapi.yaml`, `.env.example`, migrations, and `api.http` consistent with implementation changes; include request and response examples for HTTP changes. Remove unrelated changes and ensure CI passes before review.
