---
name: sync-fxrates-api-contract
description: Synchronize and verify the FX Rates HTTP API across openapi.yaml, Go routes, handlers, DTOs, handler tests, and api.http. Use when adding or changing endpoints, parameters, headers, request or response bodies, status or error codes, examples, or when investigating contract drift.
---

# Sync FX Rates API Contract

Keep the documented public contract, implementation, tests, and request examples aligned without hiding ambiguous or breaking changes.

## Contract Authority

- Treat `openapi.yaml` as the intended public contract, not as a mechanical dump of current handler behavior.
- Use the user's requested behavior as the source of truth for an explicit API change.
- When existing code and the specification disagree and intent is unclear, report the conflict and ask which behavior is intended before editing.
- Preserve unrelated descriptions, examples, and formatting. Do not regenerate or broadly rewrite the specification.

## Workflow

1. Inspect the requested change and relevant diff. Read:
   - `openapi.yaml`
   - `internal/transport/httpapi/handler.go` for route registration, validation, response headers, and status codes
   - `internal/transport/httpapi/contracts.go` and `models.go` for interfaces and JSON DTOs
   - `internal/transport/httpapi/handler_test.go` for asserted behavior
   - `api.http` for manual examples
2. Build an operation matrix for every affected endpoint. Compare:
   - HTTP method and path
   - path, query, and header parameters, including requiredness and formats
   - accepted media types, body requiredness, fields, enums, and unknown-field behavior
   - success and error statuses, response headers, JSON names, nullability, and omitted fields
   - error envelope and documented error conditions
3. Classify each difference as implementation drift, specification drift, example/test drift, or an intentional change. Flag breaking changes explicitly, including removed operations or fields, new required inputs, narrowed accepted values, and incompatible response changes.
4. Choose the action from the request:
   - For verification or review, make no edits. Report only evidence-backed mismatches with file and line references.
   - For synchronization, apply the smallest coherent change across the specification, Go implementation, tests, and `api.http` according to the stated intent.
   - For ambiguous behavior, stop before choosing a side and describe the decision required.
5. If Go files changed, run `gofmt` on them and run the focused HTTP package tests. Then run `make check` from the repository root. If a required tool is unavailable, report the failed command and continue with independent checks that can run.

## Output

Return a concise summary containing:

- synchronized files or detected mismatches
- breaking-change assessment
- validation commands and results
- unresolved decisions or checks that could not run
