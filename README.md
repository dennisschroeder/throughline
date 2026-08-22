# Workgraph

Workgraph is a lean, local-first authoritative state layer for domain-neutral agentic work. The
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
go run ./cmd/workgraph init
go run ./cmd/workgraph init --database data/workgraph.db /path/to/workspace
go run ./cmd/workgraph ready /path/to/workspace
go run ./cmd/workgraph show WORK_ITEM_ID /path/to/workspace
go run ./cmd/workgraph mcp /path/to/workspace
```

The default layout is:

```text
.workgraph/
  config.toml
  workgraph.db
```

A relative configured database path is resolved from `.workgraph/`. `init` can be rerun safely: it
reopens the configured database, applies only unapplied embedded migrations, and does not duplicate
seeded profiles.

## Current architecture

- `internal/domain`: transport- and persistence-independent work, output, and authority value types.
- `internal/app`: transaction-scoped planning, coordination, leases, readiness, output production,
  validation, authority recording, reuse, activity, and retrieval use cases.
- `internal/ports`: repository, transaction, clock, and identifier boundaries.
- `internal/sqlite`: authoritative storage, pragmas, migrations, seeds, and repository implementation.
- `internal/config`: workspace discovery, TOML configuration, and database-path resolution.
- `internal/cli` and `cmd/workgraph`: thin `init`, `ready`, and `show` adapters and binary entry point.

The implemented scenario models research and agent-skill design. It requires no Git repository,
branch, commit, pull request, build pipeline, CI system, or code-specific domain field.

## Milestone boundary

This is not the full V1. It intentionally excludes authenticated identity, a policy language, the
complete transition/review policy, manual blockers, a web UI, and orchestration. Workgraph
records external-action proposals, delegated authority, and observed execution evidence; it never
performs an external effect.

## MCP stdio

`workgraph mcp [WORKSPACE]` starts one MCP stdio server for the selected initialized workspace.
It has no mutable current-workspace session state; tool responses identify the workspace as `local`.
Start with `board_overview`, `list_ready_items`, and `get_item`, then claim work before mutating it.

Codex configuration:

```toml
[mcp_servers.workgraph]
command = "/absolute/path/to/workgraph"
args = ["mcp", "/absolute/path/to/workspace"]
```

Claude Desktop configuration:

```json
{"mcpServers":{"workgraph":{"command":"/absolute/path/to/workgraph","args":["mcp","/absolute/path/to/workspace"]}}}
```

The MCP server records external-action authority and observed results; it does not install,
publish, send, deploy, or otherwise execute an external action.

See [development and verification](docs/development.md), the canonical
[implementation handoff](docs/implementation-handoff.md), the bounded
[implementation kickoff](docs/implementation-kickoff.md), and [ADRs](docs/adr/).
