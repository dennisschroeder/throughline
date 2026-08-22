# ADR 0005: Application transactions and SQLite concurrency

- Status: Accepted
- Date: 2026-08-21

## Decision

The application layer defines each mutation boundary with `WithinTransaction`; repositories cannot
commit independently. This milestone opens one long-lived SQLite connection per process, which
serializes local writes and guarantees that every operation uses the configured pragmas. Structured
multi-query retrieval runs in one read transaction so projections represent one database snapshot.

## Consequences

Each implemented create operation is atomic. A bounded read pool, optimistic versions,
idempotency records, claims, contention tests, and multi-process coordination are deferred to their
explicit milestones; the current design leaves those concerns at the port and schema boundaries.
