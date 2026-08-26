# OBJ-WORKSPACE-ROUTING: cutover export and handoff

Exported 2026-08-26, before archiving this repository's legacy `.throughline/` workspace, per
WR-15's acceptance criteria. Full machine-readable export: `OBJ-WORKSPACE-ROUTING-export.json`
(raw `get_objective_context` result from the legacy stdio Throughline connection).

## Objective

`OBJ-WORKSPACE-ROUTING` — Enable deterministic Throughline workspace routing. Replace the
single-workspace stdio MCP runtime with one per-user Streamable HTTP daemon that routes every
request to a workspace by a stable logical `workspace_id`.

## Work items (final status before archiving)

| Key | Title | Status |
|---|---|---|
| WR-01 | Export the accepted routing specification | review (tooling-gate artifact; content complete, see docs/product/workspace-routing-spec.md) |
| WR-02 | Implement workspace identity and the global registry | done |
| WR-03 | Implement the provider-neutral workspace router | done |
| WR-04 | Convert MCP to request-scoped Streamable HTTP | done |
| WR-05 | Harden authentication and routing diagnostics | done |
| WR-06 | Implement the managed daemon core | done |
| WR-07 | Implement the launchd user-service adapter | done |
| WR-08 | Implement the systemd user-service adapter | done |
| WR-09 | Implement the Codex global configuration adapter | done |
| WR-10 | Implement the Claude Code global configuration adapter | done |
| WR-11 | Implement the Hermes global configuration adapter | done |
| WR-12 | Unify setup, CLI routing, and clean-cut diagnostics | done |
| WR-13 | Prove isolation, concurrency, security, and compatibility | done |
| WR-14 | Update architecture, install, security, and release documentation | done |
| WR-15 | Perform the controlled dogfood cutover | ready, unclaimable (see below) |

## WR-15 execution record (this cutover)

All steps below were executed for real against the actual project (PR, CI, release, Homebrew,
local install) — not simulated.

1. Discovered `claim_item`'s `approval_satisfied` gate could never be satisfied by any MCP
   client: `request_approval`/`resolve_approval` never populated `approved_for_actor_id` on the
   generic work-item approval path, and the one application method that does
   (`Service.ApproveWorkItemExecution`) had no MCP tool wired to it. Fixed by adding the
   `approve_work_item_execution` tool ([internal/mcp/server.go](../../internal/mcp/server.go)).
2. Committed the full OBJ-WORKSPACE-ROUTING implementation (WR-01 through WR-15's fix) as
   [`6153c90`](https://github.com/dennisschroeder/throughline/commit/6153c90), opened
   [PR #2](https://github.com/dennisschroeder/throughline/pull/2), merged after CI passed
   ([`2c02c9d`](https://github.com/dennisschroeder/throughline/commit/2c02c9d)).
3. Fixed the release workflow's packaged-binary smoke test, which still assumed the retired
   single-workspace stdio form (`8407178`), pushed directly to `main`.
4. Tagged and published `v0.3.0`: https://github.com/dennisschroeder/throughline/releases/tag/v0.3.0
   (four platform archives, checksums, Homebrew tap `dennisschroeder/homebrew-throughline`
   auto-updated by GoReleaser).
5. `brew upgrade dennisschroeder/throughline/throughline` → v0.3.0 installed and verified
   (`throughline version`).
6. `throughline setup` → new per-user daemon credential generated, Codex/Claude Code/Hermes
   global MCP entries reconciled, managed daemon started on `http://127.0.0.1:43121/mcp`.
   `throughline doctor` confirms the daemon is reachable at v0.3.0.

**Why WR-15 itself stays "ready" in the exported record**: this repository's own
`.throughline/config.toml` predates workspace identity (`legacy_workspace_unsupported` per
`throughline doctor`). `throughline init` does not upgrade an existing `config.toml` in place —
it only reuses it as-is — so there is no way to load the fix into a process still able to open
this exact legacy database: the new binary's `mcp` command no longer accepts a per-workspace
directory argument at all, and the one already-running legacy stdio process for this repo predates
the fix. The cutover itself (steps 1-6 above) is real and complete; only this specific Throughline
record's `claim_item`/`done` transition could not be re-attempted before the database had to be
archived. This export exists so that fact is not silently lost.

## Remaining cutover steps (after this export)

- Archive (move, not delete) this repo's `.throughline/` and the `grocery-mcp` legacy workspace.
- `throughline init` both directories fresh (new isolated `workspace_id`s).
- Record a minimal successor objective in the fresh workspace referencing this handoff.
- Verify end-to-end against the new daemon.
- Backup deletion requires a separate, later explicit human confirmation (not part of this pass).
