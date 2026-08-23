# Install guide

Every command in this guide uses a released archive from GitHub Releases. None of them require the
Go toolchain. If you have Go and want to build from source instead, see
[development.md](development.md).

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

Download the new version's archive and checksums file (step 1 with the new `0.1.0`), verify
(step 2), then repeat step 3 to replace the binary in place — same filename, same `PATH` location.
Your `.throughline/` workspace directory and its data are untouched by a binary upgrade.

## 6. Backup and restore

A workspace's `.throughline/` directory contains `config.toml` and a WAL-mode SQLite database,
`throughline.db`, alongside transient `throughline.db-wal` and `throughline.db-shm` sidecar files.

Stop any running `throughline mcp` client sessions against the workspace before backing up.

**Option A — offline copy (workspace stopped):**

```bash
cp .throughline/throughline.db .throughline/throughline.db-wal .throughline/throughline.db-shm /path/to/backup/
```

**Option B — live-safe copy (workspace may be running):**

```bash
sqlite3 .throughline/throughline.db ".backup /path/to/backup/throughline.db"
```

**Restore:** stop any running `throughline mcp` sessions, replace `.throughline/throughline.db`
with the backup file, and remove any stale `.throughline/throughline.db-wal` or
`.throughline/throughline.db-shm` sidecars left over from the previous database so SQLite does not
try to replay a mismatched WAL.

## 7. Uninstall

Remove the binary from `PATH`:

```bash
rm /usr/local/bin/throughline   # or wherever you installed it
```

Optionally remove a project's workspace directory:

```bash
rm -rf .throughline/
```

This permanently deletes all objectives, plans, work items, outputs, and authority records in that
workspace. Confirm you have a backup (step 6) before running it, and confirm no other client still
needs the workspace.

## 8. MCP client configuration

Point your MCP client at the built `throughline` binary and an initialized workspace directory. The
first argument is always the `mcp` subcommand; the second is the workspace directory to serve.

Codex (`~/.codex/config.toml` or project-local Codex config):

```toml
[mcp_servers.throughline]
command = "/absolute/path/to/throughline"
args = ["mcp", "/absolute/path/to/workspace"]
```

Claude Desktop (`claude_desktop_config.json`):

```json
{"mcpServers":{"throughline":{"command":"/absolute/path/to/throughline","args":["mcp","/absolute/path/to/workspace"]}}}
```

Use absolute paths for both the binary and the workspace directory; restart the client after
editing its configuration.

## 9. Report a problem

File an issue at `github.com/dennisschroeder/throughline/issues`.
