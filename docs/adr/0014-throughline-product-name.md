# ADR 0014: Throughline is the product name

- Status: Accepted
- Date: 2026-08-23

## Decision

Rename the pre-release product from Workgraph to Throughline. Use `Throughline` in prose and
`throughline` for the executable, Go module/repository slug, MCP server name, command directory,
workspace directory, and default database name. New workspaces use `.throughline/throughline.db`
and the default item-key prefix `TH`.

Make a clean break before `v0.1.0`; do not add legacy executable aliases or `.workgraph` discovery.

## Consequences

The repository must be published at `github.com/dennisschroeder/throughline` before a public Go
release can resolve the declared module path. Existing development-only `.workgraph` workspaces are
not discovered automatically and require explicit manual migration if any must be retained. The
local checkout directory and remote repository are operational concerns and may be renamed
separately without changing product behavior.
