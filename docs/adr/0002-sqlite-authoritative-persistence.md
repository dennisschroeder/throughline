# ADR 0002: SQLite is authoritative persistence

- Status: Accepted
- Date: 2026-08-21

## Decision

Use one SQLite file as the authoritative state store through `database/sql` and the CGo-free
`modernc.org/sqlite` driver. Enable foreign keys, WAL, and a 5000 ms busy timeout on every opened
connection. No adapter writes tables outside the application/repository boundary.

## Consequences

Workspaces remain local, portable, and serverless. WAL supports concurrent readers and one writer
on a local host, but ordinary network filesystems and cross-host sharing are unsupported. The
driver and its `modernc.org/libc` dependency are pinned.
