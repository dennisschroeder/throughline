# ADR 0022: Codex global configuration adapter

- Status: Accepted
- Date: 2026-08-25

## Context

`docs/research/mcp-transport-compatibility.md` established that Codex, Claude Code, and
Hermes each support one user-level MCP configuration pointing at a loopback Streamable HTTP
URL with a bearer credential. `WR-09` implements the first of the three: reconciling
Throughline's entry into Codex's global `~/.codex/config.toml` without disturbing anything
else in it.

## Decision

`internal/clientconfig` defines the contract every harness adapter (Codex now; Claude Code
and Hermes next) shares: a provider-neutral `Entry{URL, BearerTokenEnvVar, Required}`, the
one server name every adapter writes under (`ServerName = "throughline"`), a `Result{Changed
bool}`, and `*ErrConflict{Path, Reason}` for an existing entry an adapter did not itself
write.

`internal/codexconfig.Reconcile(path, entry, force)` decodes `config.toml` into a generic
`map[string]any` (preserving every top-level key, table, and pre-existing
`[mcp_servers.*]` entry it does not touch), sets `[mcp_servers.throughline]` to
`{url, bearer_token_env_var, required}` — following the research doc's recommendation to use
`bearer_token_env_var` over static `http_headers`, so the adapter never writes the token's
actual value into the file, only the name of the environment variable Codex should read it
from — and writes the result back via an atomic temp-file-and-rename. If an entry already
exists under `mcp_servers.throughline` and matches byte-for-byte (compared value-by-value,
not `reflect.DeepEqual`, since a value round-tripped through `Unmarshal` and a freshly built
map can differ in wrapper type while meaning the same thing), `Reconcile` is a no-op. If it
exists and differs, `Reconcile` returns `*clientconfig.ErrConflict` and leaves the file
untouched unless `force` is `true` — diagnosing a conflict without silently overwriting it,
per the accepted decision, while still giving a later confirmed-override flow (`WR-12`) a
way to proceed.

`internal/codexconfig/codexconfig_test.go` fixtures a representative Codex 0.149.1
`config.toml` (top-level settings, a `[profiles.default]` table, and one pre-existing
unrelated `[mcp_servers.filesystem]` entry) and asserts all of it survives reconciliation
unchanged, that reconciliation is idempotent, that a conflicting existing entry is diagnosed
without being overwritten (and only overwritten when `force` is explicitly `true`), and that
no token-shaped value (`Authorization`, `Bearer `) ever appears in the written file.

## Consequences

- `internal/clientconfig`'s `Entry`/`ErrConflict`/`Result` types are reused as-is by the
  Claude Code (`WR-10`) and Hermes (`WR-11`) adapters, so all three diagnose conflicts and
  report results identically even though their underlying file formats (TOML, JSON, YAML)
  differ.
- Actually setting `THROUGHLINE_MCP_TOKEN` in Codex's process environment (so
  `bearer_token_env_var` resolves to something) is not this adapter's responsibility;
  `throughline setup` (`WR-12`) is where the token is placed into each client's supported
  secret environment, per the accepted decision.
- TOML comments and exact formatting in the user's existing `config.toml` are not preserved
  byte-for-byte — `go-toml/v2`'s generic-map round trip preserves every key and value but
  not comments, since no comment-preserving TOML editor is a project dependency. This is
  judged acceptable for a machine-managed entry in what remains a hand-editable file; every
  other key and value is verified unchanged.
