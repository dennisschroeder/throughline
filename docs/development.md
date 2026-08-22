# Development and verification

## Commands

```bash
gofmt -w cmd internal
go vet ./...
go build ./...
go test ./...
go test ./internal/sqlite -run TestInitializationAndDomainNeutralVerticalSlice -count=1
go test ./internal/sqlite -run TestIntentAndPlanningVerticalSlice -count=1
go test ./internal/sqlite -run TestDurableExecutionGraphVerticalSlice -count=1
go test ./internal/sqlite -run TestCoordinationAndAuthorityVerticalSlice -count=1
go test ./internal/sqlite -run TestConcurrentAgentsCannotBothClaimWorkItem -count=20
CGO_ENABLED=0 go build ./...
```

## MCP smoke test

Build outside the repository, initialize a temporary workspace, then configure two independent
MCP clients with the same `mcp` command. Both clients should receive `workspace.id: "local"`.
Use one client to claim an item and the other with the stale version to confirm a
`version_conflict`; resume with `get_item` and `get_changes`. The package-level MCP test uses a
real MCP client/server session and verifies tool discovery, read-only annotations, and stable
`not_found` errors.

`go build ./...` validates the binary without writing it into the repository. To build an executable
explicitly, choose an output path outside the working tree:

```bash
go build -o /tmp/workgraph ./cmd/workgraph
```

## Initialization smoke test

Run this in an ordinary empty directory; Git is neither inspected nor required:

```bash
tmpdir="$(mktemp -d)"
go run ./cmd/workgraph init "$tmpdir"
go run ./cmd/workgraph init "$tmpdir"
find "$tmpdir/.workgraph" -maxdepth 1 -type f -print
```

The first command reports `initialized`; the second reports `reopened`. The SQLite integration test
asserts that both runs leave five migration records, eight active built-in profiles, foreign keys on,
WAL mode on, and a 5000 ms busy timeout.

## Database and configuration

`.workgraph/config.toml` currently contains a configuration schema version, the database path, and
the item-key prefix. Relative database paths resolve from `.workgraph/`; absolute paths remain
absolute. Workspace discovery walks from a starting directory toward the filesystem root.

The database is authoritative. Do not edit it while Workgraph is running or place the WAL database
on an ordinary network filesystem. For this milestone, each process uses one long-lived SQLite
connection, so writes are serialized and all operations share the required pragmas.

## Current data path

```text
application command
  -> domain validation
  -> application-owned transaction
  -> repository port
  -> SQLite rows

structured retrieval
  <- Objective + typed context + Questions + Decisions + Approvals
  <- reviewed Plans + recursive WorkItems + capabilities
  <- ExpectedOutputs + exact active OutputProfile versions
  <- criteria + dependencies + reusable output requirements
  <- immutable OutputRevisions + artifacts + append-only validations
  <- actor capabilities + exclusive lease claims + progress entries
  <- ExternalAction revisions + principal-bound grants + recorded execution evidence
  <- derived ready work + append-only activity
```

OutputProfile behavior is data-driven. The application checks persisted lifecycle state and exact
version; it never branches on profile names. Proposed profile versions are unavailable to plans
until an explicit review activates them.

`workgraph ready [DIRECTORY]` lists derived ready work. `workgraph show WORK_ITEM_ID [DIRECTORY]`
returns its structured objective, plan, output, criterion, dependency, validation, reuse,
coordination, and authority context. The application layer also offers actor-filtered ready work;
MCP/CLI exposure of claims and authority operations is intentionally deferred.
