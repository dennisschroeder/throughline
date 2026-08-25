# ADR 0019: Managed daemon core — single-owner lock, ServiceManager seam, credential rotation

- Status: Accepted
- Date: 2026-08-25

## Context

ADR 0018 secured the endpoint but relied entirely on `runMCP` running once, in the
foreground, forever. The accepted decisions require exactly one endpoint owner with a
deterministic duplicate-start failure, a lifecycle a real OS service manager can drive
(start/stop/restart/status/logs), and safe credential rotation (preflight, backup, commit,
restart, rollback) — none of which existed yet. `launchd` and `systemd --user` (`WR-07`,
`WR-08`) are adapters behind one daemon-management seam; this work item defines that seam
and its own OS-agnostic reference implementation.

## Decision

`internal/daemon.Acquire(path)` is the single-endpoint-owner lock: it opens (or creates) a
PID file and takes an exclusive, non-blocking `flock(2)` on it. A second `Acquire` on an
already-held lock returns `ErrAlreadyRunning{PID}` immediately — a deterministic, specific
failure, not an ambiguous "address already in use". Because ownership is enforced by the
kernel-held flock rather than the file's mere existence, a process that crashes without
releasing it leaves no stale lock a later `Acquire` must clean up; `HeldBy` reads the
recorded PID for read-only diagnostics without taking the lock. `runMCP`
(`internal/cli`) now calls `Acquire` itself, right after validating `--addr`, before
generating or loading the credential — a second `throughline mcp` invocation, however it was
started, fails the same way.

`internal/daemon.ServiceManager` is the seam: `Start`, `Stop`, `Restart`, `Status`, `Logs`,
each `context`-scoped. `ProcessManager` is the OS-agnostic reference adapter this work
item's own tests exercise: `Start` preflights the lock (fail fast, deterministically, before
spawning anything), then spawns `<executable> mcp --addr <addr>` detached
(`SysProcAttr.Setsid`) with output appended to a log file and its PID recorded; `Stop` sends
`SIGTERM`, waits up to 5s, then escalates to `SIGKILL`; `Status` checks PID liveness and
optionally probes an injected `CheckHealth` callback for the version; `Logs` returns the
tail of the log file. `launchd`/`systemd --user` adapters (`WR-07`, `WR-08`) implement the
same `ServiceManager` interface using their own unit/service concepts instead of a bare
detached process; `ProcessManager` is not a substitute for either (no auto-start on login, no
crash-restart) — CLI selection of which adapter `throughline daemon` uses is later work.

`throughline daemon <start|stop|restart|status|logs|rotate-credential>` (`internal/cli`)
drives a `ProcessManager` built from the same state directory as the registry and
credential (`daemonStateDir`, derived from the registry path so tests inherit
`registryPathForTesting`'s hermeticity automatically) — `daemon.lock`, `daemon.pid`,
`daemon.log` live alongside `registry.db` and `credentials`. `daemon status --json` now
reports `{reachable, running, pid, version, error}`, extending `WR-05`'s `{reachable,
version, error}` contract additively.

`daemon.RotateCredential(ctx, path, manager, verify)` implements the accepted rotation
sequence: preflight (the current credential must be readable — there is nothing to rotate
otherwise), backup (`path + ".backup"`), commit (`credential.Regenerate`, an unconditional
overwrite sibling to `LoadOrCreate`, added to `internal/credential`), `manager.Restart`, then
an optional caller-supplied `verify` (the CLI wires this to an authenticated `/health`
request with the new token). A restart or verification failure restores the backup and
restarts again so the daemon is left serving a token known to work, joining the rotation
failure and any rollback failure into one error rather than swallowing either.

## Consequences

- `throughline mcp` is now safe to invoke twice by accident (a second terminal, a restart
  race, a supervisor misconfiguration): the second invocation fails immediately with a clear
  message instead of silently losing the port bind or, worse, half-starting.
- Updating managed client configurations (Codex, Claude Code, Hermes) with a rotated token is
  explicitly out of scope here — `RotateCredential` only rotates and verifies the local
  credential file and restarts the daemon; `WR-09`–`WR-12` extend rotation to also rewrite
  those configs once their adapters exist.
- CLI-level `daemon start/stop/restart/rotate-credential` wiring (`internal/cli`) is verified
  by code review, `go build`/`go vet`, and the underlying `internal/daemon` package's own
  tests (which spawn a real detached process via the standard `TestMain` re-exec idiom), not
  by a dedicated CLI integration test that spawns the real `throughline` binary: doing so
  would need either a production env-var override for the registry/credential paths (which
  the accepted decision explicitly forbids — "production has no alternate registry-location
  routing mechanism") or would touch the real per-user registry and credential files.
- `ProcessManager`'s own duplicate-owner preflight (`Acquire` then immediately `Release`
  before forking) is a fast-fail convenience only; the authoritative enforcement is the
  spawned process's own `Acquire` call once it runs `runMCP`, exactly as it would if started
  outside `ProcessManager` entirely.
