# ADR 0009: Explicit MCP workspace routing

- Status: Superseded by [ADR 0016](0016-workspace-identity-and-registry.md) and [ADR 0017](0017-request-scoped-streamable-http-mcp.md)
- Date: 2026-08-21

> **2026-08-25:** `OBJ-WORKSPACE-ROUTING` replaced the single-configured-workspace allowlist
> described below with a per-user registry mapping a logical `workspace_id` (not a
> filesystem path) to a provider-neutral target, resolved fresh on every request by a
> `WorkspaceRouter`. `workspace_id` is required on every call — there is no default,
> single-workspace omission form. See ADR 0016 and ADR 0017; the full accepted specification
> is in [docs/product/workspace-routing-spec.md](../product/workspace-routing-spec.md). The
> content below is preserved as the historical record of the milestone-4 decision it
> documents.

## Context

One MCP server may eventually expose multiple independent Throughline workspaces. Routing by the
server process's current directory or mutable MCP-session state would make tool calls difficult to
audit and could send a mutation to the wrong authoritative SQLite database. Accepting arbitrary
filesystem or database paths from tool inputs would also let a client escape the server's intended
scope.

## Decision

An MCP server has an explicit allowlist mapping stable `workspace_id` values to configured
Throughline roots. Tool calls routed through a multi-workspace server require `workspace_id`; the
server resolves it before invoking the shared application layer. Tool calls never accept an
arbitrary workspace or database path.

A server configured with exactly one workspace may use that workspace as its default and allow the
field to be omitted. The resolved `workspace_id` is still returned in every response. If no default
exists, omission returns `workspace_required`; an unknown identifier returns `workspace_not_found`.
There is no mutable "current workspace" selection stored in an MCP connection or session.

Each workspace retains its own configuration and authoritative SQLite database. Two clients routed
to the same workspace collaborate on the same graph; clients routed to different workspaces are
isolated. Cross-workspace reads, writes, links, and transactions are not implicit.

Milestone 4 may initially ship only the single-workspace form. Loading multiple allowlisted
workspaces and defining any explicit cross-workspace operation remain later work, but MCP request
schemas must not prevent the routing contract above.

## Consequences

- Multi-workspace routing is explicit and auditable on each request.
- Single-workspace installations keep concise tool calls without relying on hidden session state.
- Workspace identifiers are portable; local filesystem paths remain server configuration.
- The server, not the model, controls which roots and databases are reachable.
- Per-workspace SQLite transactions and change cursors remain independent.
