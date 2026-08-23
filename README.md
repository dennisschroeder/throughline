# Throughline

Throughline is a lean, local-first authoritative state layer for domain-neutral agentic work. The
current milestone provides one Go binary, one SQLite database, deterministic workspace
initialization, governed planning, durable execution/output records, and safe multi-actor
coordination.

## Build and verify

Requires Go 1.26 or newer.

```bash
go build ./...
go vet ./...
go test ./...
go test ./internal/sqlite -run TestInitializationAndDomainNeutralVerticalSlice -count=1
go test ./internal/sqlite -run TestIntentAndPlanningVerticalSlice -count=1
go test ./internal/sqlite -run TestDurableExecutionGraphVerticalSlice -count=1
go test ./internal/sqlite -run TestCoordinationAndAuthorityVerticalSlice -count=1
go test ./internal/sqlite -run TestConcurrentAgentsCannotBothClaimWorkItem -count=20
```

The SQLite driver is CGo-free; `CGO_ENABLED=0 go build ./...` is supported.

## Initialize a workspace

```bash
go run ./cmd/throughline init
go run ./cmd/throughline init --database data/throughline.db /path/to/workspace
go run ./cmd/throughline ready /path/to/workspace
go run ./cmd/throughline show WORK_ITEM_ID /path/to/workspace
go run ./cmd/throughline mcp /path/to/workspace
```

The default layout is:

```text
.throughline/
  config.toml
  throughline.db
```

A relative configured database path is resolved from `.throughline/`. `init` can be rerun safely: it
reopens the configured database, applies only unapplied embedded migrations, and does not duplicate
seeded profiles.

## Current architecture

- `internal/domain`: transport- and persistence-independent work, output, and authority value types.
- `internal/app`: transaction-scoped planning, coordination, leases, readiness, output production,
  validation, authority recording, reuse, activity, and retrieval use cases.
- `internal/ports`: repository, transaction, clock, and identifier boundaries.
- `internal/sqlite`: authoritative storage, pragmas, migrations, seeds, and repository implementation.
- `internal/config`: workspace discovery, TOML configuration, and database-path resolution.
- `internal/cli` and `cmd/throughline`: thin `init`, `ready`, and `show` adapters and binary entry point.

The implemented scenario models research and agent-skill design. It requires no Git repository,
branch, commit, pull request, build pipeline, CI system, or code-specific domain field.

## Milestone boundary

The coordination kernel is feature-complete for the market-testable `v0.1.0` scope, but release
packaging and design-partner validation are not complete. The release contract is frozen in the
[v0.1 product decision](docs/product/0001-market-testable-v0.1.md). Authenticated identity, policy
language, a web UI, orchestration, multi-workspace/network transport, semantic search, broad CLI
parity, and distributed coordination are explicitly outside that boundary. Throughline records
external-action proposals, delegated authority, and observed execution evidence; it never performs
an external effect.

## MCP stdio

`throughline mcp [WORKSPACE]` starts one MCP stdio server for the selected initialized workspace.
It has no mutable current-workspace session state; tool responses identify the workspace as `local`.
Start with `board_overview`, `list_ready_items`, and `get_item`, then claim work before mutating it.

Codex configuration:

```toml
[mcp_servers.throughline]
command = "/absolute/path/to/throughline"
args = ["mcp", "/absolute/path/to/workspace"]
```

Claude Desktop configuration:

```json
{"mcpServers":{"throughline":{"command":"/absolute/path/to/throughline","args":["mcp","/absolute/path/to/workspace"]}}}
```

The MCP server records external-action authority and observed results; it does not install,
publish, send, deploy, or otherwise execute an external action.

See [development and verification](docs/development.md), the architectural
[implementation handoff](docs/implementation-handoff.md), the bounded
[v0.1 product decision](docs/product/0001-market-testable-v0.1.md), the
[v0.1.0 release handoff](docs/v0.1.0-release-handoff.md), and [ADRs](docs/adr/).
