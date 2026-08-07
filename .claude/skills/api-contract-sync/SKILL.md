---
name: api-contract-sync
description: Use when changing HTTP handlers, routes, DTOs, or configuration in this service - verifies that openapi.yaml, api.http, .env.example, and compose.yaml still agree with the Go code and reports the exact mismatches.
---

# API contract sync

The HTTP surface and the configuration of this service are mirrored in files
the compiler never sees, so they drift silently. Walk both lists and report
each mismatch as `file:line` with the one-line fix. Report only; do not edit
anything unless asked.

Run `git diff` first, and keep drift introduced by the current change separate
from drift that was already there.

## HTTP surface

Truth is spread across three packages: `transport/httpapi` holds the routes,
DTOs, and status codes, `internal/service` decides which sentinel error each
method can return, and `internal/domain` owns the currency and status enums.

1. Every pattern registered in `Handler.Routes()` has a matching path and
   method in `openapi.yaml`, and every documented path is registered. The
   route table in `README.md` lists the same set.
2. Each operation documents exactly the statuses it can produce. Collect them
   from the `writeJSON` and `writeError` calls that handler reaches.
   `writeServiceError` is shared by all three handlers, so count only the
   branches whose sentinel error that handler's service method actually
   returns. Add the 500 from `recoverMiddleware`, and a 405 wherever the mux
   answers a method mismatch.
3. Every error `code` passed to `writeError` is listed in an `enum` on
   `APIError.code`. If that enum is absent, report it once with the full list
   of codes, not once per code.
4. Requiredness follows `omitempty`, not pointer-ness. A field tagged
   `omitempty` must be absent from `required`; every other field must be
   present in it, and a pointer among them must be `nullable: true`, because
   `encoding/json` still emits it as `null`.
5. The `Location` header set in `createQuoteUpdate` is documented on the 202,
   and `additionalProperties: false` on the request schema still matches
   `DisallowUnknownFields()`.
6. The `CurrencyPair` and `UpdateStatus` enums match `supportedCurrencies` in
   `domain/pair.go` and the `UpdateStatus` constants in `domain/quote.go`.
7. `api.http` targets routes that exist, with headers the handlers accept.

## Configuration

Truth is `internal/config/config.go`.

8. Every key read through `os.Getenv` in `Load` appears in `.env.example` and
   in `app.environment` in `compose.yaml`. A key compose hardcodes as a
   literal instead of `${VAR:-value}` is not overridable through `.env`,
   whatever `.env.example` implies - say so when you find one.
9. Each `default*` constant matches both the compose fallback and the value
   in `.env.example`.
10. Every bound `Validate` enforces is stated where the setting is declared,
    as a comment in `.env.example`, not only in prose in `README.md`. That
    covers the `FRANKFURTER_MAX_ATTEMPTS` range and the rule that
    `PROCESSING_TIMEOUT` must exceed the whole Frankfurter retry window.

## Verify

Finish with the check from `.github/workflows/ci.yml`, so a spec edit cannot
break the build:

```bash
npx --yes @redocly/cli@2.46.0 lint openapi.yaml \
  --skip-rule info-license-strict --skip-rule no-server-example.com
```

Report "no drift" only after every point above has been walked.
