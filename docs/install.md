# Install guide

## Homebrew (macOS, Linux)

```bash
brew install dennisschroeder/throughline/throughline
```

This taps `dennisschroeder/homebrew-throughline` and installs the formula automatically; skip to
[step 4](#4-verify-the-install) to verify. The remaining steps in this guide install from a
released archive directly and use no package manager. None of them require the Go toolchain. If you
have Go and want to build from source instead, see [development.md](development.md).

Examples below use `v0.1.0`. GoReleaser strips the leading `v` from the git tag for the archive
filename but keeps it in the release tag and download URL path, so a release tagged `v0.1.0`
produces filenames like `throughline_0.1.0_darwin_arm64.tar.gz` under
`.../releases/download/v0.1.0/...`. For a later release, substitute that version's number in both
places the same way — check the [releases page](https://github.com/dennisschroeder/throughline/releases)
if in doubt.

## 1. Download

Pick the archive matching your OS and architecture, and download the matching checksums file.

macOS (Apple Silicon):

```bash
curl -LO https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_darwin_arm64.tar.gz
curl -LO https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_checksums.txt
```

macOS (Intel):

```bash
curl -LO https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_darwin_amd64.tar.gz
curl -LO https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_checksums.txt
```

Linux (amd64):

```bash
curl -LO https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_linux_amd64.tar.gz
curl -LO https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_checksums.txt
```

Linux (arm64):

```bash
curl -LO https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_linux_arm64.tar.gz
curl -LO https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_checksums.txt
```

`wget` works the same way, e.g. `wget https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_darwin_arm64.tar.gz`.

## 2. Verify the checksum

macOS and Linux both ship `shasum`:

```bash
shasum -a 256 -c throughline_0.1.0_checksums.txt --ignore-missing
```

On Linux distributions where `shasum` is unavailable, use the native `sha256sum` instead:

```bash
sha256sum -c throughline_0.1.0_checksums.txt --ignore-missing
```

Do not proceed if verification fails.

## 3. Install on PATH

```bash
tar -xzf throughline_0.1.0_<os>_<arch>.tar.gz
chmod +x throughline
mv throughline /usr/local/bin/throughline   # or: mv throughline ~/.local/bin/throughline
```

Use whichever directory is already on your `PATH`; `~/.local/bin` avoids needing `sudo`.

**macOS Gatekeeper note:** `v0.1.0` binaries are not notarized. On first run, macOS may refuse to
open the binary as coming from an unidentified developer. Either remove the quarantine attribute
before running it:

```bash
xattr -d com.apple.quarantine ./throughline
```

or right-click the binary in Finder and choose "Open" once, then approve the dialog.

## 4. Verify the install

```bash
throughline version
```

This should report `v0.1.0` along with the commit and build date.

## 5. Upgrade

Homebrew install:

```bash
brew upgrade dennisschroeder/throughline/throughline
```

Archive install: download the new version's archive and checksums file (step 1 with the new
`0.1.0`), verify (step 2), then repeat step 3 to replace the binary in place — same filename, same
`PATH` location. Your `.throughline/` workspace directory and its data are untouched by a binary
upgrade.

## 6. Setup: one managed daemon, once per machine

```bash
throughline setup
```

This is idempotent and safe to rerun. It:

- generates (or reuses) one per-user bearer credential;
- installs and starts the one OS-managed daemon service (a `launchd` LaunchAgent on macOS, a
  `systemd --user` unit on Linux);
- atomically reconciles a global MCP entry into every supported harness it detects installed
  (Codex, Claude Code, Hermes) — backing each up first and rolling every change back together if
  any step fails, and never overwriting a conflicting existing `throughline` entry without
  `--force`.

The daemon binds only to `127.0.0.1:43121` by default (override with `--addr host:port`, kept
consistent across `setup`, `mcp`, `doctor`, and `daemon` by passing the same flag). See
[Security model](../README.md#security-model-cooperative-local-agents-one-per-user-daemon) for the
threat model this credential and loopback bind are designed for.

## 7. Initialize a workspace

Repeatable per directory, idempotent, and preserves identity across a move (but not a plain
filesystem copy — see [docs/product/workspace-routing-spec.md](product/workspace-routing-spec.md)):

```bash
throughline init /path/to/workspace
```

This writes a stable `workspace_id` into `/path/to/workspace/.throughline/config.toml` and
registers it in the per-user registry; it does not touch daemon or global client configuration —
`throughline setup` (step 6) handles that once, for every workspace.

## 8. Command reference

| Command | Does |
|---|---|
| `throughline setup [--addr] [--force]` | One-time host setup: credential, managed daemon, global harness configs. |
| `throughline init [dir] [--database path]` | Initialize or reopen a workspace's logical identity. |
| `throughline init --fork [dir]` | Give a copied workspace directory its own independent identity. |
| `throughline unregister [dir]` | Remove a workspace's routing registration; workspace data is untouched. |
| `throughline mcp [--addr]` | Run the daemon in the foreground (what the managed service executes). |
| `throughline daemon start\|stop\|restart [--addr]` | Control the managed daemon service. |
| `throughline daemon status [--addr] [--json]` | Report daemon reachability, PID, and version. |
| `throughline daemon logs [--addr] [--lines N]` | Print the daemon's recent log lines. |
| `throughline daemon rotate-credential [--addr]` | Rotate the bearer credential and verify the restarted daemon accepts it. |
| `throughline doctor [--addr]` | Read-only: workspace, registry, and daemon health, each with a remediation pointer. |
| `throughline ready --actor <id> [dir] [--addr]` | List ready work for an actor, via the daemon. |
| `throughline show <id> [dir] [--addr]` | Print one work item's context, via the daemon. |
| `throughline uninstall [--addr]` | Stop the daemon, remove managed harness entries; workspace data and the registry are preserved. |
| `throughline version` | Print the build version, commit, and date. |

`ready` and `show` are daemon clients like any MCP client — they resolve the nearest workspace's
`workspace_id` from its `.throughline/config.toml` and call the running daemon; they never open a
workspace's database directly, and both fail with a clear connection error if the daemon is not
running (`throughline daemon start` or `throughline mcp`).

## 9. File locations

| Path | Contents |
|---|---|
| macOS: `~/Library/Application Support/Throughline/` | `registry.db` (workspace routing registry), `credentials` (bearer token), `daemon.lock`/`daemon.pid`/`daemon.log` (reference `ProcessManager` state; unused when `launchd` manages the service). |
| Linux: `${XDG_STATE_HOME:-~/.local/state}/throughline/` | Same contents as above. |
| macOS: `~/Library/LaunchAgents/com.throughline.daemon.plist` | The managed LaunchAgent unit `throughline setup` installs. |
| Linux: `${XDG_CONFIG_HOME:-~/.config}/systemd/user/throughline-daemon.service` | The managed systemd user unit `throughline setup` installs. |
| `<workspace>/.throughline/config.toml` | This workspace's stable `workspace_id` and `database_path`. |
| `<workspace>/.throughline/throughline.db` (+ `-wal`/`-shm`) | This workspace's own WAL-mode SQLite database — coordination state lives here, per workspace. |

`registry.db` and `credentials` are both mode `0600` inside a mode `0700` directory. Neither ever
contains a workspace path's contents or a workspace database; the registry stores only the
provider-neutral routing mapping described in
[docs/product/workspace-routing-spec.md](product/workspace-routing-spec.md).

## 10. Backup and restore

Back up **each workspace's own database** the same way as before — nothing changed about a single
workspace's SQLite file:

```bash
sqlite3 /path/to/workspace/.throughline/throughline.db ".backup /path/to/backup/throughline.db"
```

**Restore:** stop the daemon (`throughline daemon stop`), replace
`<workspace>/.throughline/throughline.db` with the backup, remove any stale `-wal`/`-shm`
sidecars, then start the daemon again (`throughline daemon start`).

Also back up the per-user `registry.db` (step 9) if you want to restore routing for every
workspace at once rather than re-running `throughline init` per workspace afterward; it contains
no workspace data, only the identity mapping.

## 11. Uninstall

```bash
throughline uninstall     # stops the managed daemon, removes global harness MCP entries
```

This preserves every workspace's data and the registry unconditionally — it only reverses what
`throughline setup` added. Then remove the binary:

Homebrew install:

```bash
brew uninstall dennisschroeder/throughline/throughline
```

Archive install:

```bash
rm /usr/local/bin/throughline   # or wherever you installed it
```

To also delete a workspace's coordination data (irreversible — confirm a backup first, per step
10):

```bash
rm -rf /path/to/workspace/.throughline/
```

To remove the per-user registry and credential entirely (irreversible; affects every workspace's
routing):

```bash
rm -rf ~/Library/Application\ Support/Throughline/      # macOS
rm -rf "${XDG_STATE_HOME:-~/.local/state}/throughline/"  # Linux
```

## 12. Report a problem

Run `throughline doctor` first — it points at the specific remediation for a workspace, registry,
or daemon problem without changing anything. If the problem persists, file an issue at
`github.com/dennisschroeder/throughline/issues`.
