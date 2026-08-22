# ADR 0003: Embedded transactional migrations

- Status: Accepted
- Date: 2026-08-21

## Decision

Embed ordered SQL files in the binary. A small runner parses the numeric filename prefix, rejects
duplicate versions, applies each unapplied file in one transaction, and records it in
`schema_migrations` in that same transaction. Migration 0001 also inserts deterministic built-in
OutputProfile rows. Reopening validates applied version/name pairs against the embedded history and
rejects unknown future versions or renamed entries.

## Consequences

Initialization and reopening use the same path and are idempotent. Failed migrations roll back
their schema, seed, and tracking changes together. Migration history is forward-only; downgrade
support is not part of this milestone.
