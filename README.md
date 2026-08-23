# Throughline

Throughline is a local, headless coordination state layer. `v0.1.0` lets one human and two
MCP-capable agents resume, approve, safely claim, complete, validate, reuse, and audit one
domain-neutral workflow across sessions, using one SQLite workspace. It ships as a single Go
binary (`throughline`) that owns a `.throughline/` workspace directory and speaks MCP over stdio
plus a compact CLI for local inspection. See the frozen release definition in
[Product Decision 0001](docs/product/0001-market-testable-v0.1.md).

## Scope boundary

`v0.1.0` is deliberately narrow. Explicitly out of scope: authenticated identity, a policy
language, a web UI, orchestration, multi-workspace or network transport, semantic search, broad
CLI parity, and distributed coordination. The full release boundary is in
[Product Decision 0001](docs/product/0001-market-testable-v0.1.md).

Throughline records external-action proposals, principal-bound grants, and observed execution
evidence. **It never performs an external effect.** Installing, publishing, sending, deploying, or
any other side effect stays outside Throughline; only the coordination record — proposed, granted,
started, succeeded/failed, with evidence — lives inside it.

## Security model: trusted-local

Throughline has no authentication. Actor strings (`human:reviewer`, `agent:planner`) are
self-declared and not verified — any client speaking the MCP protocol to a workspace can act as
any actor. The SQLite workspace is a plain local file. Do not place `.throughline/` on a shared or
network filesystem, and do not run `throughline mcp` as a multi-tenant service. Throughline assumes
every process reaching the workspace is already trusted.

## Supported platforms

| OS | Architecture |
|---|---|
| macOS (darwin) | amd64 |
| macOS (darwin) | arm64 |
| Linux | amd64 |
| Linux | arm64 |

## Install

macOS or Linux with [Homebrew](https://brew.sh):

```bash
brew install dennisschroeder/throughline/throughline
```

Otherwise, download the archive matching your OS/architecture and the matching checksums file from
the [GitHub Releases page](https://github.com/dennisschroeder/throughline/releases), verify with
`shasum -a 256 -c`, then extract and move the `throughline` binary onto your `PATH`. Full
step-by-step commands, the macOS Gatekeeper quarantine note, upgrade, backup/restore, and
uninstall instructions are in [docs/install.md](docs/install.md).

## Quickstart

```bash
throughline init /path/to/workspace
```

Point an MCP client at the initialized workspace. Claude Desktop configuration:

```json
{"mcpServers":{"throughline":{"command":"/absolute/path/to/throughline","args":["mcp","/absolute/path/to/workspace"]}}}
```

(Codex configuration is in [docs/install.md](docs/install.md).) Restart the client, then start with
`board_overview` for a compact orientation summary, `list_ready_items` to see executable candidate
work, and `get_item` before claiming anything.

## License and reporting a problem

Throughline is released under the [MIT License](LICENSE). Report a problem or request a feature via
[GitHub issues](https://github.com/dennisschroeder/throughline/issues).

## Building from source

If you have the Go toolchain and want to build from source instead of using a released binary, see
[docs/development.md](docs/development.md) for build, test, and verification commands.

## See also

- [Development and verification](docs/development.md)
- [Implementation handoff](docs/implementation-handoff.md)
- [Product Decision 0001: market-testable v0.1](docs/product/0001-market-testable-v0.1.md)
- [v0.1.0 release handoff](docs/v0.1.0-release-handoff.md)
- [Install guide](docs/install.md)
- [Architecture decision records](docs/adr/)
