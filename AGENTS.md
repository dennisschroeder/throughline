# Throughline

Read `docs/implementation-handoff.md` completely before making architectural or domain decisions.
Use `docs/implementation-kickoff.md` as the bounded first implementation task.

## Working agreements

- Write a short plan before implementation, then complete the requested milestone rather than only
  returning a design.
- Keep changes minimal and record consequential assumptions as ADRs.
- Use Go and a CGo-free SQLite driver unless a documented blocker requires revisiting the decision.
- SQLite is authoritative; MCP, CLI, and future UI adapters share the application layer.
- The domain layer must not import transport or persistence packages.
- Domain-neutral work must require no Git or coding-specific primitive.
- Output profiles are persisted governed data. Never branch on profile names.
- Throughline records external actions and authority but never executes external effects.
- Run formatting, static checks, builds, and all tests before finishing.
- Use Conventional Commits with an imperative subject and no agent-attribution trailers.
