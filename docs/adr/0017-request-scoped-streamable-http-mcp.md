# ADR 0017: Request-scoped Streamable HTTP MCP adapter

- Status: Accepted
- Date: 2026-08-25

## Context

ADR 0012 bound the MCP adapter to one workspace opened at process start and served over
stdio. `OBJ-WORKSPACE-ROUTING` requires one globally configured daemon that resolves
`workspace_id` independently on every request, per ADR 0016's registry and the
`WorkspaceRouter` decision. The transport itself had to change too: stdio is inherently
one-process-per-workspace, which is exactly the topology this objective replaces.

## Decision

`internal/mcp` no longer holds a pre-resolved `*app.Service`. `adapter` now holds a
`*router.Router`. Every workspace-scoped tool's input type embeds `workspaceInput`, and
`schemaFor` unconditionally marks `workspace_id` required — the one exception is
`get_semantic_model`, registered through `addWorkspaceless` and not `add`, because it reads
the embedded, provider-independent semantic model and touches no workspace persistence.

`add`, the single central request wrapper, is the only place a workspace is selected: it
reads `workspace_id` from the raw arguments first (failing closed with `workspace_required`
before general schema validation, so a missing identifier always reports that specific
code), validates the full input schema, resolves a `*app.Service` through
`router.Router.Service`, and only then invokes the handler with that service. No handler
holds or caches a service across calls. Router/registry resolution failures map to the
stable codes from ADR 0016's decision: `workspace_required`, `workspace_invalid`,
`workspace_not_found`, `workspace_pending`, `workspace_unavailable`,
`workspace_registry_conflict`, `provider_unsupported`, `provider_unavailable`,
`workspace_busy`; none carries a path, locator, or credential.

`mcp.Handler(router)` wraps `NewServer(router)` in `mcp.NewStreamableHTTPHandler`, returning
the same `*mcp.Server` for every request — safe per the SDK's own documentation, since no
per-connection workspace state exists to isolate. `throughline mcp` (`internal/cli`) now
takes no workspace argument; it opens the registry, builds a `router.Router` with the
production `SQLiteProvider`, and serves that handler on `/mcp` at a loopback address
(default `127.0.0.1:43121`), shutting down gracefully on context cancellation. The `mcp`
subcommand no longer accepts a positional workspace directory and no longer spawns a stdio
subprocess; the legacy `CommandTransport`-based integration test is replaced with an
`httptest`-backed Streamable HTTP one exercising two independent client sessions against one
shared `Router`.

Two schema-generation defects surfaced and were fixed in the same pass, since they blocked
routine tool calls: `jsonschema.For` emits `"type": ["null","array"]` for a required
non-pointer slice field, which some MCP clients mis-encode; `schemaFor` now collapses that
union to a plain `"array"` for every generated schema (`dropNullableArrayUnion`), since these
commands treat "absent", "null", and "empty array" identically. `validateJSONSchema`'s own
type check also only handled a single-string `"type"`, silently skipping validation for any
union-typed field; it now checks the input against every alternative in a type array.
Separately, the built-in output profile seeded by migration `0001_initial.sql` carries a
`capabilities_do_not_grant_authority` semantics field the governed output schema did not
allow, breaking `list_output_profiles`/`get_output_profile` outright; the governed
`semantics` schema now permits it.

## Consequences

- Every workspace-scoped MCP call is independently routable; a stale or malicious client
  cannot select a workspace via connection, session, CWD, or environment state.
- `ready`, `show`, and other CLI subcommands still resolve a workspace by local file
  discovery directly against SQLite, not through the daemon — turning them into daemon HTTP
  clients is later work (`WR-12`), not this change's.
- Full authentication, Host/Origin validation, and redacted diagnostics are still open
  (`WR-05`); this change only makes the transport and resolution request-scoped.
- Process supervision (auto-start, crash-restart, launchd/systemd) is not implemented here;
  `throughline mcp` runs in the foreground until a later work item wraps it.
