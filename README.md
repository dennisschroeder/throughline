# Workgraph

Workgraph is a lean, local-first authoritative state layer for domain-neutral agentic work. Its
target deployment is one Go binary and one SQLite database, with MCP as the primary agent interface
and optional human projections.

## Repository status

The repository is prepared for the first dedicated implementation task. No product code has been
implemented yet.

- [`docs/implementation-handoff.md`](docs/implementation-handoff.md) is the canonical product and
  architecture specification.
- [`docs/implementation-kickoff.md`](docs/implementation-kickoff.md) is the bounded first coding
  task and its exit criteria.
- `AGENTS.md` contains the repository-wide working agreements.

The first Codex task should implement executable foundations and one domain-neutral vertical slice,
verify the result, and stop before expanding into the full V1 tool surface.
