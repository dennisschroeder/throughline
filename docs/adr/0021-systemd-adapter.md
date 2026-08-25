# ADR 0021: systemd --user service adapter

- Status: Accepted
- Date: 2026-08-25

## Context

`WR-08` provides the Linux counterpart to `WR-07`'s `launchd` adapter: one `systemd --user`
unit standing in for `daemon.ProcessManager` on Linux, behind the same `daemon.ServiceManager`
seam ADR 0019 defined.

## Decision

`internal/systemd.Manager` implements `daemon.ServiceManager` by mapping every lifecycle
operation onto one user unit, `throughline-daemon.service`
(`${XDG_CONFIG_HOME:-~/.config}/systemd/user/throughline-daemon.service`): `Start`
(re)writes the unit, runs `systemctl --user daemon-reload`, then `systemctl --user start`;
`Stop` and `Restart` map directly to `systemctl --user stop`/`restart`; `Status` uses
`systemctl --user show <unit> --property=ActiveState,MainPID` — machine-readable
`key=value` output rather than the human-oriented `status` subcommand — treating
`ActiveState != active` or a non-zero `systemctl` exit (typically "unit could not be found")
as not running rather than an error, and optionally probing an injected `CheckHealth` for
the version; `Logs` reads the file `StandardOutput`/`StandardError` (`append:<path>`) both
point at, via the same `daemon.ReadLogTail` the `launchd` and `ProcessManager` adapters use.

The generated unit contains exactly the daemon's invocation (`ExecStart=<executable> mcp
--addr <addr>`, `Type=simple`, `Restart=on-failure`) and the log path — never a bearer
token, workspace path, registry path, or provider locator, none of which `Manager` even
receives. It is written mode `0600`. Every `systemctl` invocation goes through an injectable
`CommandRunner`, mirroring `internal/launchd`; `internal/systemd/manager_test.go` uses a
fake recording exact commands and returning canned `systemctl show` output, so every test
asserts the correct command was constructed without ever touching a real systemd instance.

## Consequences

- `daemon.ServiceManager` now has three implementations
  (`daemon.ProcessManager`, `launchd.Manager`, `systemd.Manager`); which one
  `throughline daemon`/`throughline setup` selects per platform is `WR-12`'s concern, not
  this adapter's.
- This adapter is untested against a real systemd instance in this session (this repository
  and its CI run on macOS, where `systemd --user` does not exist) — the fixture coverage
  here is thorough for command construction and unit-file content, but a manual end-to-end
  verification on a real Linux host with systemd is worth doing once `WR-12` wires real
  installation.
- Uninstall (removing the unit, `daemon-reload`, and `systemctl --user disable`) is not part
  of `daemon.ServiceManager` and is not implemented here, matching the `launchd` adapter; it
  belongs to the setup/uninstall flow in `WR-12`.
