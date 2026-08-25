# ADR 0026: Workspace routing validation summary

- Status: Accepted
- Date: 2026-08-25

## Context

`WR-13` validates the complete daemon contract built across `WR-01`–`WR-12`: client
compatibility, workspace isolation under concurrency, parallel-agent-graph identity and
coordination semantics, the full set of accepted failure modes, provider-neutral isolation,
and the format/lint/type/unit/integration/race bar every prior work item already held itself
to. Rather than a new subsystem, this work item's job is to close genuine coverage gaps and
record where each acceptance criterion's proof actually lives, since the tests were written
incrementally across a dozen packages as each piece shipped.

## Decision

Two new tests closed the gaps that remained after `WR-01`–`WR-12`:

- `internal/mcp/server_test.go`'s `TestParallelGraphNodesClaimConflictRereadAndReplayIndependently`
  exercises the accepted `agent:<harness>:<run_id>:<node_key>` parallel-node-identity
  contract through the full HTTP+router+MCP stack: two distinct node identities from the
  same run race a claim, the loser's `claim_conflict` envelope carries enough `current`
  state to reread, the winner's identical retry replays via (actor_id, idempotency_key)
  scoping rather than re-executing, a differently-scoped actor reusing the same key text is
  never mistaken for the same replay, and a same-actor changed retry is rejected as
  `idempotency_key_reused_with_different_request`. Claim lease expiry itself remains proven
  at the domain layer (`internal/domain/work/coordination_test.go`, which predates this
  objective); this test's job is confirming routing does not disturb what that layer already
  guarantees.
- `TestDaemonRemainsHealthyAfterAnAbruptClientDisconnect` proves one client's abandoned,
  already-cancelled request (no clean MCP session shutdown, no standalone SSE stream left
  open) never wedges the daemon: a fresh session opened immediately afterward against the
  same `Router`/HTTP server completes a call within a bounded 5s deadline.

Every other acceptance criterion's proof already existed by the time this work item started;
this ADR is the map from criterion to evidence:

1. **All three harness versions share the daemon contract.** `internal/codexconfig`,
   `internal/claudecodeconfig`, and `internal/hermesconfig` each fixture a specific pinned
   version (Codex 0.149.1, Claude Code 2.1.231, Hermes 0.19.0) and prove their adapter
   produces a configuration matching `docs/research/mcp-transport-compatibility.md`'s
   documented contract for that client. `internal/mcp/server_test.go`'s
   `TestHTTPTwoClientNonCodeWorkflowSmoke` proves two independent client sessions —
   standing in for any two of the three harnesses — drive the full coordination workflow
   against one shared daemon correctly. Running the actual third-party binaries is out of
   reach in this environment; the fixture-plus-shared-session combination is the closest
   verifiable proxy.
2. **Concurrent two-workspace traffic never cross-routes.** `internal/mcp/server_test.go`'s
   `TestConcurrentRequestsRouteDistinctWorkspacesIndependently` (16 concurrent
   `create_objective` calls across two workspaces through one `Router`/HTTP endpoint) and
   `internal/router/router_test.go`'s `TestFakeSharedProviderIsolatesWorkspacesUnderOneProviderInstance`.
3. **Parallel node identity, claims, idempotency, version, reread, expiry.** This work
   item's new `TestParallelGraphNodesClaimConflictRereadAndReplayIndependently`, plus
   pre-existing domain-layer expiry coverage as noted above.
4. **Deterministic registry/restart/provider/SQLite/rotation/Origin/unknown-workspace/disconnect
   failures.** `internal/registry/registry_test.go` (not-found, pending, unavailable,
   conflict); `internal/daemon/service_test.go`
   (`TestProcessManagerRestartReplacesTheRunningProcess`) and `rotate_test.go`
   (commit+restart, rollback on restart failure, rollback on verify failure); `internal/router/router_test.go`
   (`ErrProviderUnsupported`/`ErrProviderUnavailable`, the real `SQLiteProvider`);
   `internal/daemon/rotate_test.go` (rotation); `internal/daemonhttp/middleware_test.go`
   (`TestProtectRejectsAForeignOriginEvenWithACorrectCredential`); `internal/mcp/server_test.go`'s
   `TestWorkspaceScopedToolsFailClosedOnMissingAndUnknownWorkspaceID`; this work item's new
   disconnect test.
5. **Fake shared provider isolation.** `internal/router/router_test.go`'s
   `TestFakeSharedProviderIsolatesWorkspacesUnderOneProviderInstance` (`WR-03`).
6. **Format, lint, type, unit, integration, race.** `gofmt -l .`, `go vet ./...`, and
   `go test ./... -race` all pass clean across every package in the module as of this work
   item's completion — verified as the last step before marking it done, not merely at each
   prior work item's own boundary.

## Consequences

- The full test suite (`go test ./... -race`) is the durable, re-runnable proof this ADR
  describes; this document is a map into it, not a substitute for reading the tests
  themselves.
- Genuinely running the real Codex, Claude Code, and Hermes binaries against a live
  Throughline daemon remains unverified in this environment; `WR-15`'s human-approved cutover
  is the first point at which that becomes possible, using the real dogfood workspace.
