# ADR 0025: Unified setup, domain-CLI-as-daemon-client, and uninstall

- Status: Accepted
- Date: 2026-08-25

## Context

`WR-01`–`WR-11` built the daemon, its security boundary, its lifecycle seam, and three
per-harness configuration adapters as independent pieces. `WR-12` unifies them: one `setup`
command that provisions all of it atomically, `ready`/`show` converted from direct SQLite
readers into daemon clients (closing the one remaining "domain CLI opens storage" gap), and
`uninstall` that reverses `setup` without ever touching workspace data.

## Decision

**`ready`/`show` become daemon clients.** `internal/cli/cli.go`'s `findWorkspace` resolves
only `config.Find` — no database is opened. `callDaemonTool` connects as one short-lived MCP
client (`protocol.StreamableClientTransport` with `DisableStandaloneSSE: true` and a 15s
context deadline, since `http.Client.Timeout` would also cap a long-lived SSE stream had one
been kept open), authenticates via an `authRoundTripper` that adds the stored bearer token to
every request, calls the tool, and returns its `result` payload. `list_ready_items` requires
a non-empty `actor_id` at the schema level (unchanged by this work item); `ready` gained a
required `--actor` flag rather than silently degrading. `show`'s output changed from the raw
Go-struct JSON `encoder.Encode(ports.WorkItemContext)` produced to the MCP tool's own
snake_case envelope — an intentional, accepted shape change, not a regression, verified by
`TestReadyAndShowInspectExecutionGraph` seeding its execution graph entirely through the
running daemon's MCP endpoint rather than opening the database directly. `throughline init`
still opens and migrates the workspace database directly, because creating and migrating it
is unambiguously initialization, not domain-facing querying.

**`internal/setup.Run`** is the atomic preflight/reconcile/backup/rollback sequence: load or
create the credential, then for every configured `Target` (one per harness, gated on a
`DetectPath` so an uninstalled harness is left alone entirely) back up its existing content,
reconcile it, and — if backing up or reconciling with a genuine (non-conflict) error fails,
or the daemon fails to start — restore every backup made so far, including removing a freshly
created file that did not exist before. A per-target `*clientconfig.ErrConflict` does not
abort the run; it is reported in that target's `TargetResult` so the operator can rerun with
`--force` for just that harness, matching the accepted "diagnoses conflicts... without
overwrite" requirement across all three adapters at once. `internal/cli`'s `runSetup` wires
`codexconfig`/`claudecodeconfig`/`hermesconfig` as `setup.Target`s (detected via
`~/.codex`, the `~/.claude.json` file itself, and `~/.hermes` respectively) and
`newServiceManager` — the same constructor `throughline daemon`'s lifecycle subcommands now
use — for the `Manager`, so `setup`, `daemon start/stop/restart/status/logs`, and
`rotate-credential` all control one platform-selected service (`launchd.Manager` on darwin,
`systemd.Manager` on linux, `daemon.ProcessManager` elsewhere), never a second route.

**`internal/*config.Remove`** was added to all three adapters (deleting only the
`throughline` entry, preserving every other key, mirroring `Reconcile`'s parsing) so
`runUninstall` can remove what `setup` added without a fourth ad hoc parser in `cli.go`.
`throughline uninstall` stops the managed service via the same `newServiceManager`, calls
`Remove` on every present (not merely detected) client config, and touches nothing else —
no registry, no workspace database, no credential file — printing that explicitly.

**Legacy rejection** was already implemented in `internal/config.Load` (`WR-02`) and
`throughline init` (tested in `WR-02`); this work item added
`TestDomainCommandsRejectALegacyWorkspaceConfig` confirming `ready`, `show`, and `doctor` all
surface the same `legacy_workspace_unsupported` message a legacy `.throughline/config.toml`
produces, not just `init`.

## Consequences

- `throughline` now has one canonical operator flow: `setup` once, `daemon status`/`logs` to
  observe, `daemon rotate-credential` periodically, `uninstall` to reverse — all driven
  through the same `daemon.ServiceManager` instance selection.
- `throughline doctor` was not extended to check managed-client-configuration agreement in
  this pass: doing so read-only would need a new read-only "is this harness's entry present
  and matching" check in each of the three adapter packages (distinct from `Reconcile`,
  which would write a missing entry), which is a small, self-contained follow-on rather than
  part of this change.
- `ready`'s `--actor` requirement and `show`'s snake_case output are both externally visible
  CLI behavior changes; both are exercised by updated tests, and neither was a decision this
  work item was free to design around — they follow directly from `list_ready_items`'s
  required `actor_id` and the MCP envelope shape shipped in `WR-04`.
