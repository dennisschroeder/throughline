# ADR 0018: Daemon authentication and routing diagnostics

- Status: Accepted
- Date: 2026-08-25

## Context

ADR 0017 made every MCP request independently routable but added no protection: any local
process could reach `/mcp` on the loopback port. The accepted decisions require one per-user
bearer credential, Host/Origin validation, no CORS, authenticated health, and stable
fail-closed diagnostics before this daemon is safe to run continuously.

## Decision

`internal/credential` generates one 256-bit random token per local user on first use
(`LoadOrCreate`), stored at the platform user-state directory alongside the registry
(`~/Library/Application Support/Throughline/credentials` on macOS,
`${XDG_STATE_HOME:-~/.local/state}/throughline/credentials` on Linux), directory mode
`0700`, file mode `0600`. `credential.Equal` compares a presented token in constant time via
`crypto/subtle`.

`internal/daemonhttp.Protect` wraps the daemon's `http.Handler` and is the transport
security boundary: it validates the Host header, then a present Origin header (an absent
Origin is allowed for native MCP clients; any present, non-matching Origin is rejected, and
no `Access-Control-*` header is ever added), then the bearer credential — all before the
request reaches MCP parsing or workspace routing — then applies a body-size limit via
`http.MaxBytesReader`. Every request gets a random correlation id (`X-Request-Id` plus
context value), and one redacted JSON access-log line is emitted per request (request id,
method, path, status, duration only — never a header, query string, body, path, or
locator). `throughline mcp` (`internal/cli`) sets `ReadHeaderTimeout`/`ReadTimeout`/
`WriteTimeout`/`IdleTimeout` on its `http.Server` and rejects any `--addr` whose host is not
a loopback form (`127.0.0.1`, `::1`, `localhost`), so the daemon cannot be pointed at a
non-loopback bind by mistake. `/health` is registered behind the same `Protect` middleware
as `/mcp`, returning only `{"status","version"}`.

`internal/mcp`'s error envelope now includes `retryable` and `request_id` on every failure:
`routingErrorPayload` classifies each routing sentinel error against a table of
`{code, retryable}` pairs (`workspace_pending`, `workspace_unavailable`, and
`provider_unavailable` are transient and marked retryable; identity/configuration errors are
not), and `errorPayload` adds the same two fields to every domain-error payload, defaulting
`retryable` to true only for `version_conflict`/`claim_conflict`. `workspace_id` is checked
before general JSON Schema validation in the central `add` wrapper, so a missing value
always reports `workspace_required` specifically rather than a generic `validation_failed`.

`throughline doctor` is the single read-only diagnostic command: nearest workspace
discovery (including a distinct `legacy_workspace_unsupported` report), registry agreement
via `Registry.Lookup` and `Registry.CheckFingerprint`, and authenticated daemon
reachability, each with one remediation pointer — it never repairs or creates a second
route. `throughline daemon status [--json]` reports the same authenticated-health
reachability as a stable `{"reachable","version","error"}` contract; both commands share one
`fetchHealth` helper that always authenticates and never falls back to an unauthenticated
request or a different address.

## Consequences

- `throughline mcp` is now safe to leave running: every request must present the correct
  bearer token and pass Host/Origin checks first, and nothing is ever exempted.
- `doctor`/`daemon status` currently report only authenticated-health reachability, not
  installed-service state (launchd/systemd) or managed-client configuration agreement;
  extending them for those is `WR-06`–`WR-12`'s scope, not this change's. The `--json`
  contract and `fetchHealth` seam are stable so those work items can extend the payload
  without changing this one.
- Redacted access logging exists only at the HTTP transport layer for now (request id,
  method, path, status, timing); attributing a specific workspace_id and result code to a
  log line, per the full accepted decision, requires wiring `request_id` through into the
  MCP tool-call layer's own logging, which is not yet done.
- The credential is generated eagerly by `throughline mcp` itself; `throughline setup`'s
  fuller provisioning flow (writing the token into managed Codex/Claude Code/Hermes
  configurations, rotation with preflight and rollback) is `WR-06`'s and later work items'
  responsibility and can reuse `credential.LoadOrCreate` as its primitive.
