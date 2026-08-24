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

Semantic model generation is deterministic and part of the normal build check:

```bash
go generate ./internal/semanticmodel
git diff --exit-code -- internal/semanticmodel/model.generated.json
```

The canonical ontology is [ontology/throughline.json](../ontology/throughline.json). The binary
embeds the generated model and MCP clients can retrieve its manifest or bounded sections with
`get_semantic_model`; Graphify remains outside CI and release dependencies.

`gofmt -w` above fixes formatting in place. CI instead runs the list-only check, which fails if any
file is unformatted rather than silently rewriting it:

```bash
gofmt -l cmd internal
```

`throughline version` (or `throughline --version`) reports the build's version, commit, and date.
Those values are development defaults unless injected via linker flags at release build time:

```bash
go run -buildvcs=true ./cmd/throughline version
```

Plain `go run` (without `-buildvcs=true`) does not embed VCS metadata in the resulting binary, so
commit and date report `unknown`; `go build` embeds it by default. `-buildvcs=true` makes `go run`
report the same pseudo-version, commit, and date a local `go build` would.

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
go build -o /tmp/throughline ./cmd/throughline
```

## Initialization smoke test

Run this in an ordinary empty directory; Git is neither inspected nor required:

```bash
tmpdir="$(mktemp -d)"
go run ./cmd/throughline init "$tmpdir"
go run ./cmd/throughline init "$tmpdir"
find "$tmpdir/.throughline" -maxdepth 1 -type f -print
```

The first command reports `initialized`; the second reports `reopened`. The SQLite integration test
asserts that both runs leave five migration records, eight active built-in profiles, foreign keys on,
WAL mode on, and a 5000 ms busy timeout.

## Database and configuration

`.throughline/config.toml` currently contains a configuration schema version, the database path, and
the item-key prefix. Relative database paths resolve from `.throughline/`; absolute paths remain
absolute. Workspace discovery walks from a starting directory toward the filesystem root.

The database is authoritative. Do not edit it while Throughline is running or place the WAL database
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

`throughline ready [DIRECTORY]` lists derived ready work. `throughline show WORK_ITEM_ID [DIRECTORY]`
returns its structured objective, plan, output, criterion, dependency, validation, reuse,
coordination, and authority context. The application layer also offers actor-filtered ready work;
MCP/CLI exposure of claims and authority operations is intentionally deferred.

## Release dry run

`goreleaser release --snapshot --clean` builds every release archive locally, under the current
GoReleaser configuration, without publishing anything (no tag, remote, or GitHub token required):

```bash
goreleaser release --snapshot --clean
```

Output lands in `dist/`; remove it after inspection (`rm -rf dist/`). For the full release process,
including repository setup, tagging, checksum verification, and post-publish checks, see
[docs/release-checklist.md](release-checklist.md).
