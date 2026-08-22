# ADR 0001: Go and one Workgraph binary

- Status: Accepted
- Date: 2026-08-21

## Decision

Implement Workgraph in Go as one `workgraph` binary. Keep CLI and future MCP/UI adapters thin over
the same application layer. Use the standard library unless a focused dependency removes material
platform or correctness risk.

## Consequences

Go gives straightforward cross-platform builds and a small operational surface. The domain must
remain explicit rather than relying on framework behavior. This milestone ships only `workgraph
init`; additional commands stay outside scope.
