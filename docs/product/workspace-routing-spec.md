# Workspace routing specification (OBJ-WORKSPACE-ROUTING)

Status: accepted, plan revision 1, exported 2026-08-25.
Source: Throughline objective `01a033bd-beab-79a0-abdb-0c778c146a29`, approved plan
`01a03832-b0d0-73e0-bc37-4d0e5bf1ed73`. This document is a standalone export; it does not depend on
chat history or the pre-cutover Throughline database.

## Objective

Define and implement the single client-agnostic path by which local coding agents discover a
workspace identity and a global local Throughline daemon routes every MCP operation to its logical
persistence scope.

**Desired outcome:** every supported local coding-agent task uses one globally configured, local
Throughline daemon. The agent discovers the nearest initialized workspace's stable `workspace_id`
from `.throughline/config.toml` and supplies it with every MCP call; the daemon fails closed and
routes the request to the correct logical persistence scope without per-repository MCP
configuration, connection-bound workspace state, or alternative routing mechanisms.

## Requirements

- **Initialization creates one durable workspace identity.** `throughline init` idempotently creates
  a stable `workspace_id` in the canonical `.throughline/config.toml` and atomically registers the
  workspace in the per-user allowlisted registry. It rejects identifier collisions and silent
  rebinding. (`01a0347f-3bdf-7a64-83b5-7f77b1b9cec1`)
- **Every MCP operation is explicitly workspace-scoped.** Every Throughline MCP tool requires
  `workspace_id`. The router resolves it before accessing domain or persistence services and fails
  closed with stable machine-readable errors when the identifier is missing, unknown, conflicting, or
  unavailable. (`01a0347f-45e6-74a7-a1c2-1e2425f10427`)
- **Supported harnesses share one local daemon.** Codex, Claude Code, Hermes, and each other
  explicitly supported local coding harness connect through one canonical global MCP configuration to
  the same per-user Throughline daemon, without repository-specific MCP entries or a fallback
  transport. (`01a0347f-4dbb-7ba0-9ce2-091f784003ff`)
- **Routing remains independent of physical storage topology.** Workspace routing resolves
  `workspace_id` to a provider-neutral logical persistence target. Domain and MCP layers must not
  assume one workspace equals one database, file, schema, connection, or process.
  (`01a0347f-5495-7493-ac5e-187cbb5088e7`)
- **Parallel requests remain isolated and recoverable.** Concurrent requests for the same or
  different workspaces never share mutable workspace-selection state. Optimistic versions,
  idempotency, claims, leases, and transactions remain effective; one unavailable or corrupt
  workspace never terminates the daemon or affects another workspace.
  (`01a0347f-5c24-7d90-a005-75721365648e`)
- **Routing is traceable without leaking storage details.** Every request and resulting activity is
  attributable to `workspace_id` and `actor_id`. Diagnostics expose stable routing outcomes and error
  codes while avoiding database paths, credentials, authorization tokens, or unrelated workspace
  metadata. (`01a0347f-62e9-7109-bf2e-71f8afd59c1e`)

## Binding decisions

1. **One canonical file-based workspace routing path** (`01a0340f-58d8-7b4a-8de9-a133a61b58ad`,
   supersedes `01a033f3-6ae7-7a60-bb61-45fe52f889f2`). `throughline init` writes a stable
   `workspace_id` to `.throughline/config.toml` and registers it in the allowlisted global workspace
   registry. Before its first Throughline tool call, an agent deterministically searches upward from
   the task working directory for the nearest `.throughline/config.toml`, reads `workspace_id`, and
   includes it explicitly in every MCP operation. Every tool requires `workspace_id`; missing or
   unknown identifiers fail closed. There is no local default, server-side working-directory
   inference, environment-variable routing, connection-level `bind_workspace` state, client-specific
   adapter, or duplicated workspace identifier in `AGENTS.md`/`CLAUDE.md`-equivalent files.

2. **Cooperative local-agent threat model for the initial milestone**
   (`01a03416-5972-7d46-a146-3d9f3668dafd`). The milestone coordinates cooperative agents running
   under the same local user account. `workspace_id`, `actor_id`, claims, optimistic versions, and
   idempotency keys provide deterministic routing, attribution, and concurrency safety — they are not
   cryptographic authentication or an adversarial tenant-isolation boundary. The daemon stays
   local-only, fails closed for missing/unknown workspace identifiers, and never accepts arbitrary
   storage paths. Strict isolation between mutually untrusted agents/workspaces is out of scope and
   requires a future authorization design.

3. **One loopback Streamable HTTP transport** (`01a034d3-5b01-7839-b123-0a731b638ea2`). Throughline
   exposes exactly one MCP transport: Streamable HTTP on a per-user loopback endpoint. One
   independently managed Throughline daemon accepts concurrent connections from globally configured
   Codex, Claude Code, and Hermes clients. Each request is independently routable; every
   workspace-scoped tool requires `workspace_id`. Bind only to loopback, validate `Origin`, require
   bearer authentication. No STDIO or legacy SSE as alternative product paths. Workspace-discovery
   guidance is authoritative in tool schemas/descriptions because server instructions are not
   portable across all supported harnesses (see [compatibility research](../research/mcp-transport-compatibility.md)).

4. **One managed per-user daemon lifecycle** (`01a03798-79c7-7562-b423-87dea8aaddd9`). A one-time
   idempotent `throughline setup` creates protected per-user configuration and credentials, persists
   the loopback endpoint, installs and starts exactly one OS-managed user daemon, and atomically
   reconciles global MCP entries for detected supported harnesses without blindly overwriting
   conflicts. Per-workspace `throughline init` only initializes workspace identity and registration.
   `throughline daemon status|start|stop|restart|logs` always control that same OS-managed service and
   never spawn an alternative server. launchd and `systemd --user` are adapters behind one
   daemon-management seam; Throughline does not implement its own process supervisor. The service
   auto-starts and crash-restarts. Authenticated health reports version, registry state, and provider
   readiness. Updates replace the binary and restart the same service while preserving credentials,
   registry, and workspace data. Uninstall removes the service and managed global MCP entries but
   preserves workspace data unless explicit purge is requested. The legacy public `throughline mcp`
   STDIO path is removed as part of the migration path.

5. **One per-user bearer credential** (`01a037d4-9519-70a7-a4a2-adfd5befdca7`). `throughline setup`
   generates one cryptographically random 256-bit bearer token per local user and writes it as a
   static `Authorization` header into managed global Codex, Claude Code, and Hermes configurations.
   Credential and managed client configuration files are user-readable only. Environment variables,
   OAuth, TLS, OS keychains, per-agent credentials, and alternate transports are not the V1 default.
   The daemon binds only to `127.0.0.1`, validates Host and Origin, permits absent Origin for native
   MCP clients, rejects foreign browser origins, exposes no CORS, and authenticates before MCP parsing
   or workspace routing. Health is authenticated. All endpoints enforce body limits and timeouts; logs
   redact credentials and storage details. Unknown workspace IDs fail generically without revealing
   paths or providers. Tokens never enter workspace configuration, registry data, databases, logs,
   status output, or errors. Rotation preflights every managed target, creates backups, updates
   credentials and client configurations, restarts the same service, and rolls back on failure.
   Cooperative agents under one OS user share the token; actor IDs, claims, versions, and idempotency
   are coordination rather than authentication. Malicious processes running as that same OS user
   remain outside the V1 threat model.

6. **Workspace identity is logical, not path identity** (`01a03809-cdb3-7dcf-b031-daae4e27c770`).
   `workspace_id` identifies one logical workspace, not one directory path. Moving or renaming a
   workspace preserves its identity; idempotent `throughline init` updates the registry when the
   previous canonical path no longer exists. A copied directory that includes
   `.throughline/config.toml` initially represents the same logical workspace; an independent copy
   requires `throughline init --fork`, which creates a new `workspace_id` while retaining history with
   explicit fork provenance. Normal Git clones receive new identities because `.throughline` remains
   ignored. If two existing canonical paths claim the same ID, registration fails closed with
   `workspace_identity_conflict` and never silently reassigns identity. Normal re-init never changes
   the ID. Upward discovery selects the nearest config, allowing explicit nested workspaces. Registry
   paths are physically canonicalized so symlinks do not create another identity. Missing or deleted
   workspaces become unavailable; IDs are never automatically deleted, reused, or reinitialized.
   Removal and destructive reinitialization require explicit commands and cannot silently delete
   history. ID-only routing cannot reliably distinguish a move from a copy, so independent copies
   require an explicit fork rather than heuristic auto-detection.

7. **Clean cut instead of product migration support**
   (`01a03820-f5b8-7b4a-af5b-47a374983a0a`). Throughline does not implement automatic workspace
   migration, lazy migration, STDIO compatibility, dual runtime paths, or rollback machinery for the
   current two disposable test workspaces. Before cutover: export this objective and its accepted
   context to an English Markdown artifact (this document); archive rather than immediately delete
   each legacy `.throughline` directory; remove legacy STDIO client configuration; install and run
   `throughline setup`; initialize each repository afresh with `throughline init`; recreate the
   routing objective in the new workspace; verify the new daemon path; only then delete the archives.
   New Throughline versions fail closed on legacy workspace configuration with
   `legacy_workspace_unsupported` and provide explicit export/archive/reset/reinitialize instructions.
   Legacy `throughline mcp` does not remain as a functioning compatibility path. This is a deliberate
   pre-product clean cut justified by having only two test workspaces.

8. **One per-user SQLite registry as the exclusive routing allowlist**
   (`01a03823-facd-76ab-93d5-1d56669177e0`). One administrative workspace registry lives in the
   platform user-state directory: `~/Library/Application Support/Throughline/registry.db` on macOS and
   `${XDG_STATE_HOME:-~/.local/state}/throughline/registry.db` on Linux. The directory is mode `0700`
   and the database is mode `0600`. Tests inject a temporary registry path; production has no
   alternate registry-location routing mechanism. The registry is not a workspace persistence
   provider. It maps `workspace_id` to a provider-neutral `WorkspaceTarget` containing provider kind,
   an opaque non-secret provider locator, canonical physical workspace root, config fingerprint,
   lifecycle state, generation, and audit timestamps. It never stores bearer credentials or provider
   secrets. Only administrative CLI flows (setup, init, fork, relocate, unregister) mutate it through
   SQLite transactions and optimistic generation checks. Fresh init uses a recoverable
   pending-to-active registration around atomic fsync-and-rename config creation; only active entries
   are routable. The daemon performs a read-only registry lookup for every workspace-scoped request,
   so changes require no reload, cache invalidation, or connection-bound state. It validates the
   active entry, config fingerprint, canonical root, and provider target availability before handing
   the opaque target to the persistence-provider layer. Unknown IDs return `workspace_not_found`;
   missing roots or targets return `workspace_unavailable`; disagreement returns
   `workspace_registry_conflict`. No request may supply or override a path or provider locator. Move
   reconciliation is allowed only when the former canonical root is unavailable; simultaneous roots
   with one ID fail closed. Unregistering removes routing authority but preserves provider data unless
   an explicit purge is separately confirmed. This registry seam replaces the current CLI coupling
   from workspace discovery directly to SQLite `Open`; all domain-facing MCP and CLI calls route
   through it.

9. **Provider-neutral runtime manager** (`01a03826-1586-7864-8f70-9fa0895bfffd`). The daemon exposes
   one deep internal `WorkspaceRouter` interface to the MCP adapter: resolve a required `workspace_id`
   to an application `Service` for the current request. The router resolves an active
   `WorkspaceTarget` through the registry, then delegates the target to a `ProviderManager`.
   `WorkspaceTarget` contains workspace identity, provider kind, opaque non-secret locator, and
   registry generation; neither the router nor the domain interprets provider locators.
   `ProviderManager` selects a `PersistenceProvider` adapter by provider kind. A provider owns its
   connection topology and returns a `ports.Store` for the target; it may use one SQLite database per
   workspace, one shared Postgres pool with tenant/schema routing, or another strategy. No layer above
   the provider assumes workspace equals database or connection. The router constructs or reuses
   `app.Service` with the returned store plus shared ID generator and clock. Runtime entries are keyed
   by `workspace_id` and target generation; concurrent cache misses are single-flight, generation
   changes replace the runtime after in-flight work drains, idle runtimes may be bounded and evicted,
   and daemon shutdown drains and closes providers. Failure to open one target is isolated and never
   poisons other workspaces. The SQLite provider owns one configured database handle per active SQLite
   target, preserving its explicit writer boundary while allowing different workspaces to execute
   independently. A fake shared provider must prove that multiple `WorkspaceTarget`s can share one
   provider instance without cross-workspace leakage. The MCP server is constructed with
   `WorkspaceRouter` rather than one `app.Service`, and one central request wrapper validates required
   `workspace_id` and resolves the service before invoking a tool handler; no handler, connection,
   context default, domain command, or client may select a workspace by path. Domain-facing CLI
   commands remain daemon clients and never instantiate providers directly. Provider secrets stay in
   provider-owned configuration and never enter `WorkspaceTarget`, the registry response, MCP output,
   or logs.

10. **Stable fail-closed routing diagnostics** (`01a03827-e153-7c54-8e5a-0f189526ee2c`).
    Authentication and transport rejection happen before MCP parsing: missing/invalid bearer
    credentials return HTTP 401, rejected Host/Origin returns HTTP 403, and neither response reveals
    workspace state. Workspace-scoped tool failures use one structured error envelope with stable
    code, safe message, retryable boolean, `request_id`, and optional non-secret remediation code.
    Initial routing codes: `workspace_required`, `workspace_invalid`, `workspace_not_found`,
    `workspace_pending`, `workspace_unavailable`, `workspace_registry_conflict`,
    `provider_unsupported`, `provider_unavailable`, `workspace_busy`. Errors never include filesystem
    paths, provider locators, credentials, DSNs, SQL, or cross-workspace details. `throughline doctor`
    is the single read-only diagnostic command: it checks service installation/version, authenticated
    health, managed harness configuration, nearest workspace config, registry agreement, and provider
    readiness, then points only to the authoritative setup/init remediation path — it never repairs or
    creates a second route. `throughline daemon status --json` exposes stable machine-readable
    service/health state. Authenticated health remains minimal. Logs use `request_id` and opaque
    `workspace_id` with result code and timing while redacting headers, paths, locators, payload
    secrets, and storage errors. No routing error silently falls back to another workspace, provider,
    transport, CWD, or connection state.

11. **Distinct logical actor identity for every parallel graph node**
    (`01a03827-f8a9-77eb-87c7-24f197fc8ae8`). Throughline remains coordination state rather than an
    execution harness and does not add an agent-graph domain model. At graph-run bootstrap, the
    orchestrator generates a UUIDv7 `run_id`. Every logical node uses a distinct stable `actor_id`
    formatted `agent:<harness>:<run_id>:<node_key>`, registers it as kind `agent`, and reuses it only
    when resuming that same logical node. Harness task IDs may be recorded as metadata but are not
    required because no portable provider-wide task identifier exists. Parallel nodes share
    `workspace_id` and objective/plan identifiers but never share `actor_id`, claim ownership, or
    idempotency keys. Each logical mutation uses an idempotency key derived from `run_id`, `node_key`,
    operation, logical target, and attempt; an exact transport retry reuses the identical key and
    payload, while a new semantic attempt uses a new attempt suffix. Request correlation IDs are
    separate from idempotency. Generic actor IDs such as `agent:codex` are not valid for parallel
    execution. Claims, leases, optimistic versions, dependencies, and idempotency remain the conflict
    controls: a version conflict requires rereading authoritative state rather than blindly changing
    `expected_version`, and a crashed node resumes with its actor identity or lets the claim expire.
    Actor identity is not authentication, does not imply an authority principal, and is never derived
    from the shared bearer credential or MCP connection.

## Risks

- **Incorrect scoping could leak or mutate another workspace** (`01a03481-3476-79d7-9f7a-0842c91e5cd2`):
  any omitted workspace predicate, incorrectly keyed cache, mutable current-workspace state, or
  provider context leak could route reads or writes to the wrong logical workspace.
- **Registry and workspace configuration can diverge** (`01a03481-3df8-70d2-9f67-23e48bf8eb5d`):
  interrupted initialization, concurrent registration, moved/copied workspaces, stale daemon state,
  and identifier reuse can create conflicting mappings unless registry updates and lifecycle
  operations are explicit and atomic.
- **A global daemon can amplify failures** (`01a03481-4534-73c8-ac49-a70aa8dc05b3`): a crash, leaked
  handle, migration failure, or unbounded resource use in one workspace could affect all connected
  tasks unless provider handles, errors, and lifecycle operations are isolated per workspace.
- **Parallel agent graphs can exhaust SQLite write capacity** (`01a03481-4c0f-7259-a656-3ee3feb85c15`):
  WAL and busy timeouts preserve correctness but do not remove the single-writer boundary; bursty
  parallel graph nodes may cause latency or timeout failures and require bounded retry, short
  transactions, and load tests.
- **Harness transport differences can break the single-path goal**
  (`01a03481-5314-73c0-bb77-9b55e0b78e34`): if any target harness cannot connect to the selected
  daemon transport or does not surface MCP instructions consistently, adding a client-specific
  fallback would violate the accepted architecture.

## Success metrics

- **Target harnesses use the same daemon contract** (`01a03481-5ced-7dde-b928-70bfb10c370c`): current
  supported versions of Codex, Claude Code, and Hermes each connect through their one global
  configuration to the same local daemon and successfully call Throughline for a workspace without
  repository-specific MCP configuration.
- **Workspace bootstrap is deterministic and fail-closed** (`01a03481-6454-7347-86de-749d0de85b3e`):
  from the workspace root and nested directories, an agent selects the nearest initialized
  `.throughline/config.toml` and sends its stable `workspace_id`. Missing, unknown, conflicting, and
  unavailable identifiers produce the specified machine-readable errors without fallback.
- **Concurrent workspaces remain isolated** (`01a03481-6ba4-7e05-9149-a8cd7f1c2cb8`): parallel reads
  and mutations against at least two registered workspaces through one daemon never return, modify,
  cache, or log data under the wrong workspace identity.
- **Workspace registration is durable and atomic** (`01a03481-73af-7311-951a-6c327a8f9ffd`):
  initialization is idempotent, concurrent registration cannot corrupt the registry, identifier
  collisions and silent rebinding are rejected, and newly registered workspaces become routable
  through the documented single lifecycle without partial state.

## Assumptions (untested)

- Each supported local coding harness gives its agent a deterministic task working directory and
  permission to read the nearest `.throughline/config.toml` before the first Throughline MCP call
  (`01a0347f-69be-7e82-9ece-863be6dded8e`, confidence: medium).
- Codex, Claude Code, and Hermes can each globally configure the same local long-lived MCP transport
  and endpoint without requiring a repository-specific launcher
  (`01a0347f-70d3-7d65-93a6-07e4ef1a1ec0`, confidence: medium).

## Multi-harness transport compatibility

See [`docs/research/mcp-transport-compatibility.md`](../research/mcp-transport-compatibility.md) for
the full compatibility matrix and evidence. Summary relevant to this spec:

- Codex, Claude Code, and Hermes each support a user-level MCP configuration pointing at one loopback
  Streamable HTTP URL with a static `Authorization: Bearer <token>` header.
- None of the three clients guarantees a single shared MCP connection/session across tasks or
  processes — "one daemon" must not mean "one connection."
- MCP server `instructions` are not a portable bootstrap surface (unread by the inspected Hermes
  revision, size-capped elsewhere), so the workspace-discovery procedure must live in tool
  schemas/descriptions, not only in server instructions.
- Do not ship stdio or legacy SSE as alternative transports.

## Clean-cut cutover procedure (executed under WR-15, human-approved)

1. Export this specification and the objective's accepted context (this document) — done under WR-01.
2. Archive (do not delete) each legacy `.throughline` directory for the two existing disposable test
   workspaces.
3. Remove legacy STDIO client configuration for Codex/Claude Code/Hermes.
4. Install and run `throughline setup` to provision the managed daemon, credentials, and registry.
5. Reinitialize each repository afresh with `throughline init`.
6. Recreate the routing objective/state as needed in the new workspace.
7. Verify the new daemon path end-to-end (health, routing, isolation).
8. Only after explicit human confirmation, delete the archives.

There is no automatic migration, lazy migration, STDIO compatibility shim, dual runtime path, or
rollback machinery for the legacy workspaces — this is a deliberate pre-product clean cut.

## Non-goals for this milestone

- Cryptographic per-agent authentication or adversarial multi-tenant isolation between workspaces or
  agents (deferred; see decision 2 above).
- A second persistence provider implementation (the routing contract must preserve the seam, but a
  concrete second provider ships later).
- An agent-graph domain model inside Throughline; actor identity remains attribution, not
  orchestration.
