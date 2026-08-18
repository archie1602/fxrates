---
description: Review a SQL migration pair for reversibility, data safety, and agreement with the domain rules
argument-hint: "[migration name or sequence number; defaults to the newest pair]"
---

# Review migration

Report only. Do not edit any file, start Docker, or connect to a database.

Review the `.up.sql` and `.down.sql` pair matching `$ARGUMENTS` in
`migrations/`, where files are named `<seq>_<name>.up.sql` and either part
identifies the pair. Without an argument, review the pair with the highest
sequence number.

This project has a PostgreSQL integration suite. Review the migration statically first, then
identify which existing integration scenarios cover it and which migration-specific behavior
still needs a test. Be concrete: quote the statement you object to and say what it does at runtime.

## Reversibility

- The `.up.sql` has a `.down.sql` with the same sequence number.
- The down migration undoes the up migration. Match every table, column,
  index, and constraint by name, and name the ones left behind.
- Drops are ordered so foreign keys never block them: a referencing table
  goes before the table it references.
- Statements are wrapped in `BEGIN;` and `COMMIT;`, as every migration here
  does. A statement that cannot run inside a transaction is the exception -
  `CREATE INDEX CONCURRENTLY` above all - and such a file holds that
  statement alone rather than being a defect.
- `make migrate-down` steps down one migration at a time, so each pair has to
  round-trip on its own.

## Data safety

- A `NOT NULL` column added to a populated table follows
  `000002_add_rate_date.up.sql`: add it nullable, backfill it, then
  `ALTER COLUMN ... SET NOT NULL`. A bare `NOT NULL` without a default fails
  on a non-empty table.
- A backfill is re-runnable and derives from columns that are already
  populated. Date arithmetic states its time zone explicitly.
- After down then up, does the backfill restore the original values or
  substitute an approximation? Say which. A value that originally came from
  outside the database cannot be rebuilt from other columns.
- Say plainly when a down migration destroys data.
- A nullable `UNIQUE` column relies on Postgres treating NULLs as distinct.
  Confirm it is not declared `NULLS NOT DISTINCT`, and that the code really
  does need many NULL rows to coexist.

## Agreement with the code

- A CHECK constraint may be looser than the domain rule it mirrors, which is
  defense in depth, but never stricter, or it rejects values the application
  accepts. Compare the `pair` format with `domain.ParsePair`, the status list
  with the `UpdateStatus` constants in `domain/quote.go`, and `rate` with
  `domain.ParseRate`: for `numeric(p, s)` require `p - s` to equal
  `maxRateIntegerDigits` and `s` to equal `maxRateFractionalDigits`.
- Name the index that serves each query in
  `internal/storage/postgres/quote_update_repository.go`, or state that none
  does. Cover every query in the file, including the recovery worker's
  requeue - not only the claim and read paths.
- A partial index is usable only when the planner can prove the query's
  predicate implies the index predicate, which needs a literal in the query
  rather than a bound parameter. Flag a query that filters a partial index's
  column through `$n`.
- Every added column is used: selected, filtered on, or relied on as an
  `ON CONFLICT` arbiter. A lookup key that never appears in a SELECT list is
  fine; a column touched nowhere is not.

## Output

Order findings by severity - blocking, worth fixing, nit - and label each.
Then list, briefly, what you checked and deliberately accepted, so a reader
can tell a real defect from a design decision you already weighed.
