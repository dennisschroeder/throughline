# ADR 0016: Workspace identity and the global registry

- Status: Accepted
- Date: 2026-08-25

## Context

ADR 0009 and ADR 0012 bound `workspace_id` to a single server-configured workspace opened at
process start. `OBJ-WORKSPACE-ROUTING` replaces that with one globally configured daemon that must
resolve a stable, request-scoped `workspace_id` to the correct logical persistence scope, without a
per-workspace MCP process. That requires a durable, logical workspace identity independent of
filesystem path, and one exclusive per-user allowlist mapping that identity to a provider-neutral
target. See `docs/product/workspace-routing-spec.md` for the full accepted specification.

## Decision

`.throughline/config.toml` gains a required `workspace_id` field and its schema version moves to 2.
A config with `schema_version = 1` (no `workspace_id`) is a legacy workspace; `config.Load` rejects
it with `ErrLegacyWorkspace` rather than silently treating it as routable, matching the accepted
clean-cut cutover (no automatic migration). `config.Initialize` requires a caller-supplied
`workspace_id` for a fresh workspace and writes it via fsync-and-rename so a crash never leaves a
partially written config; reopening an existing workspace ignores the passed identifier.
`config.Fork` rewrites an already-initialized workspace's config with a new identity while keeping
its `database_path` and `item_key_prefix`, for the one sanctioned way an independent copy diverges
from its source.

A new `internal/registry` package implements the per-user SQLite registry described in the accepted
"one per-user SQLite registry" decision: `~/Library/Application Support/Throughline/registry.db` on
macOS, `${XDG_STATE_HOME:-~/.local/state}/throughline/registry.db` on Linux, directory mode `0700`,
file mode `0600`. It stores `WorkspaceTarget` rows (workspace identity, provider kind, an opaque
non-secret provider locator, canonical physical root, config fingerprint, lifecycle state,
generation, fork provenance) and never a database path, DSN, or credential. `BeginRegistration` is
the single atomic entry point for fresh registration, idempotent reopening, move reconciliation
(when the previously registered canonical root no longer exists), and copy rejection (when it still
does, returning `ErrWorkspaceIdentityConflict`); `Activate` completes the pending-to-active
transition once the caller has durably written the config file. `Lookup` is a fresh, read-only
resolution that fails closed with `ErrWorkspaceNotFound`, `ErrWorkspacePending`, or
`ErrWorkspaceUnavailable`. `CheckFingerprint` detects registry/config drift
(`ErrWorkspaceRegistryConflict`) without repairing it. `Fork` and `Unregister` round out the
administrative lifecycle; unregistering removes routing authority only, never provider data.
`CanonicalizeRoot` resolves symlinks so a linked path and its real path are one identity.

`throughline init` (`internal/cli`) now generates a UUIDv7 workspace_id for fresh workspaces via the
existing `app.UUIDv7Generator`, orchestrates config creation/reopen with registry registration, and
exposes `--fork`. `throughline unregister` exposes the registry's `Unregister`. The registry
location has no runtime override; tests inject a temporary path directly.

## Consequences

- Workspace identity now genuinely survives a move and rejects a silent copy, matching the accepted
  logical-identity decision; only `throughline init --fork` creates a second identity from a copy.
- `internal/registry` has no dependency on the domain/application layers or on `internal/sqlite`;
  it is a narrow, independently testable seam that `WR-03`'s `WorkspaceRouter` will consume without
  interpreting `ProviderLocator` itself.
- `ready`, `show`, and `mcp` do not yet consult the registry — they still resolve a workspace by
  local file discovery alone. Routing every domain-facing call through `WorkspaceRouter` is `WR-03`
  and `WR-04`'s scope, not this change's.
- Existing single-workspace test fixtures with `schema_version = 1` now fail closed instead of
  loading; there is no compatibility shim, per the accepted clean-cut decision.
