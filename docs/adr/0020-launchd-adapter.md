# ADR 0020: launchd user-service adapter

- Status: Accepted
- Date: 2026-08-25

## Context

ADR 0019 defined `daemon.ServiceManager` and a bare-process reference adapter. `WR-07`
provides the real macOS adapter: one `launchd` LaunchAgent standing in for `ProcessManager`
wherever `throughline setup` (later work) installs it, giving the daemon auto-start on login
and crash-restart, neither of which `ProcessManager` offers.

## Decision

`internal/launchd.Manager` implements `daemon.ServiceManager` by mapping every lifecycle
operation onto one LaunchAgent labeled `com.throughline.daemon`
(`~/Library/LaunchAgents/com.throughline.daemon.plist`): `Start` (re)writes the plist and
runs `launchctl bootstrap gui/<uid> <plist>`; `Stop` runs `launchctl bootout
gui/<uid>/<label>`; `Restart` runs `launchctl kickstart -k gui/<uid>/<label>`; `Status` runs
`launchctl print gui/<uid>/<label>` and parses its `state = running` / `pid = N` lines,
treating a non-zero `launchctl` exit as "not loaded" rather than an error (that is what it
almost always means) and optionally probing an injected `CheckHealth` for the version;
`Logs` reads the file `StandardOutPath`/`StandardErrorPath` both point at, via the same
`daemon.ReadLogTail` `ProcessManager` uses (extracted from `WR-06`'s implementation so both
adapters format logs identically).

The generated plist contains exactly four pieces of information: the label, `ProgramArguments`
(`<executable> mcp --addr <addr>`), and the log path for both stdout/stderr redirection —
`RunAtLoad` and `KeepAlive.SuccessfulExit = false` so launchd restarts it on a crash but not
after a clean `bootout`. `Manager` never receives a bearer token, workspace path, registry
path, or provider locator, so there is nothing for the plist to leak; it is written mode
`0600` regardless. Every `launchctl` invocation goes through an injectable `CommandRunner`
interface (production shells out via `os/exec`); `internal/launchd/manager_test.go` uses a
fake recording exact commands and returning canned `launchctl print` output, so every test
asserts the correct command was constructed without ever touching the real launchd — the
accepted "fixture tests that never mutate the real service" requirement.

## Consequences

- `daemon.ServiceManager` now has two implementations (`daemon.ProcessManager`,
  `launchd.Manager`); which one `throughline daemon`/`throughline setup` selects on macOS is
  later work (`WR-12`), not this adapter's own concern.
- This adapter is untested against a real `launchd` instance in this session (deliberately,
  per the fixture-only requirement); an end-to-end manual verification against a live
  LaunchAgent is worth doing once `WR-12` wires real installation, but is not a substitute
  for the fixture coverage here.
- Uninstall (removing the plist and unregistering the service entirely, as opposed to
  `Stop`) is not part of `daemon.ServiceManager` and is not implemented here; it belongs to
  the setup/uninstall flow in `WR-12`.
