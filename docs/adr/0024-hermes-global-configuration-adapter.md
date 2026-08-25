# ADR 0024: Hermes global configuration adapter

- Status: Accepted
- Date: 2026-08-25

## Context

`WR-11` is the third harness adapter: reconciling Throughline's entry into Hermes Agent's
global `~/.hermes/config.yaml`. `docs/research/mcp-transport-compatibility.md` found no
evidence the inspected Hermes revision reads MCP server `instructions` into the agent
context, so this adapter's entry must be sufficient entirely on its own.

## Decision

`internal/hermesconfig.Reconcile(path, entry, force)` follows the same shape as the Codex
and Claude Code adapters: decode into `map[string]any`, preserve every other top-level key
(`active_profile`, `log_level`, `profiles`, and any other `mcp_servers` entry), set
top-level `mcp_servers.throughline` to `{"url": entry.URL, "headers": {"Authorization":
"Bearer ${env:<var>}"}}` — Hermes's documented `${env:VAR}` expansion form — and diagnose a
differing existing entry as `*clientconfig.ErrConflict` rather than overwrite it, exactly as
the other two adapters do.

The one real difference is YAML's lack of the other formats' built-in canonical ordering:
`gopkg.in/yaml.v3` (added as a new dependency; no YAML library previously existed in this
module) does not sort `map[string]any` keys when marshaling, and Go's own map iteration
order is randomized per-process. Without correcting for that, two reconciliations of the
same logical document could byte-differ on every run — which would also make the conflict
and idempotency checks unreliable, since they compare marshaled bytes. `orderedNode`
recursively converts a decoded document into a `yaml.Node` tree with every map's keys
sorted before marshaling, making output deterministic; `valuesEqual` uses the same
conversion so entry comparison isn't affected by the underlying map's random iteration
order either. `internal/hermesconfig/hermesconfig_test.go`'s
`TestReconcileIsIdempotentAndByteStable` reconciles the same entry five times in a row and
asserts byte-identical output each time, specifically to catch a regression here.

`internal/hermesconfig/hermesconfig_test.go` fixtures a representative Hermes Agent 0.19.0
`config.yaml` (`active_profile`, `log_level`, a `profiles.default` section, and a
pre-existing unrelated `mcp_servers.filesystem` entry) and asserts all of it survives
reconciliation unchanged, alongside the same conflict/force/missing-file coverage the Codex
and Claude Code adapters have.

## Consequences

- All three harness adapters (`WR-09`–`WR-11`) now share one contract
  (`internal/clientconfig`) and one test shape (representative version-pinned fixture,
  preserve-unrelated-config, idempotent, conflict-without-overwrite, force-override,
  no-raw-token, missing-file-creates). `WR-12`'s unified `throughline setup` can drive all
  three uniformly.
- `gopkg.in/yaml.v3` is now a direct module dependency, used only by this adapter.
- This adapter's entry alone is sufficient for Hermes to route MCP calls correctly without
  relying on server instructions, matching the accepted decision; actually setting
  `THROUGHLINE_MCP_TOKEN` in Hermes's resolvable secret scope or process environment remains
  `throughline setup`'s responsibility (`WR-12`), as with the other two adapters.
