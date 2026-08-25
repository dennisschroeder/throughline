# ADR 0023: Claude Code global configuration adapter

- Status: Accepted
- Date: 2026-08-25

## Context

`WR-10` is the second of the three harness adapters described in ADR 0022: reconciling
Throughline's entry into Claude Code's global `~/.claude.json` user-scope MCP configuration,
following the same `internal/clientconfig` contract.

## Decision

`internal/claudecodeconfig.Reconcile(path, entry, force)` decodes `~/.claude.json` into a
generic `map[string]any` (preserving every other top-level key — installation/usage state,
`projects`, and any other `mcpServers` entry it does not touch), sets top-level
`mcpServers.throughline` to
`{"type": "http", "url": entry.URL, "headers": {"Authorization": "Bearer ${<env var>}"}}` —
the documented `--transport http ... --header 'Authorization: Bearer ${VAR}'` form from
`docs/research/mcp-transport-compatibility.md` — and writes the result back via an atomic
temp-file-and-rename with two-space-indented JSON. Equality and conflict detection compare
marshaled JSON bytes rather than `reflect.DeepEqual`, since a value round-tripped through
`Unmarshal` (existing) and a freshly built map (desired) can differ in concrete
numeric/interface wrapper type while describing the same configuration; `encoding/json`
normalizes both consistently, including sorting map keys, so the comparison is stable. As
with the Codex adapter, an existing `mcpServers.throughline` entry that does not match is
diagnosed as `*clientconfig.ErrConflict` and left untouched unless `force` is `true`.

`internal/claudecodeconfig/claudecodeconfig_test.go` fixtures a representative Claude Code
2.1.231 `~/.claude.json` (top-level install/usage state, a pre-existing unrelated
`mcpServers.filesystem` stdio entry, and a `projects` section) and asserts all of it
survives reconciliation unchanged, that reconciliation is idempotent, that a conflicting
existing entry is diagnosed without being overwritten (and only overwritten with `force`),
and that the header always uses the `${VAR}` expansion form rather than a resolved secret.

## Consequences

- The same `internal/clientconfig.Entry`/`ErrConflict`/`Result` contract now backs Codex
  (TOML) and Claude Code (JSON); the Hermes adapter (`WR-11`, YAML) follows next with no
  changes to the shared contract expected.
- Per Claude Code's own documented precedence, a project- or local-scoped server with the
  same name (`throughline`) can override this user-scoped entry inside Claude Code itself;
  this adapter cannot prevent that, and diagnosing such an override is `throughline doctor`'s
  concern (`WR-05`, extended by `WR-12`), not this adapter's.
- Actually setting `THROUGHLINE_MCP_TOKEN` in Claude Code's environment is `throughline
  setup`'s responsibility (`WR-12`), matching the Codex adapter's scope boundary.
