# ADR 0012: MCP stdio adapter with explicit single-workspace routing

- Status: Accepted
- Date: 2026-08-22

## Decision

Expose the existing application services through the official Go MCP SDK over stdio. The initial
server opens exactly one workspace selected at process start, identifies it as `local` in every
successful response, and accepts only an omitted or `local` `workspace_id`. It never accepts a
filesystem or database path in a tool call.

MCP responses use structured JSON envelopes. Failures are tool errors with a deterministic
`error.code`, `message`, and `requirements` list. Input decoding rejects unknown fields after the
MCP schema is checked. The adapter records no external effects; action tools only record proposal,
approval, authorization, execution, result, and evidence through application services.

## Consequences

The server remains a thin adapter over application transactions and SQLite stays authoritative.
Multi-workspace allowlists and network transports remain deferred as specified by ADR 0009.
