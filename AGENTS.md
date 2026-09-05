# Throughline

Read `docs/implementation-handoff.md` completely before making architectural or domain decisions.
Use `docs/implementation-kickoff.md` as the bounded first implementation task.

## Working agreements

- Write a short plan before implementation, then complete the requested milestone rather than only
  returning a design.
- Keep changes minimal and record consequential assumptions as ADRs.
- Use Go and a CGo-free SQLite driver unless a documented blocker requires revisiting the decision.
- SQLite is authoritative; MCP, CLI, and future UI adapters share the application layer.
- Never open `.throughline/*.db` directly, not even read-only. The MCP server and the CLI are the
  only supported paths to workspace state; going around them bypasses versioning, claims, lifecycle
  validation and authority, and reads a state no other client sees. If the interface cannot answer
  a question, that is a defect to file, not a reason to reach for the file.
- The domain layer must not import transport or persistence packages.
- Domain-neutral work must require no Git or coding-specific primitive.
- Output profiles are persisted governed data. Never branch on profile names.
- Throughline records external actions and authority but never executes external effects.
- The gate before finishing is exactly what CI runs, all six, in this order:
  - `test -z "$(gofmt -l cmd internal)"`
  - `go generate ./internal/semanticmodel && git diff --exit-code -- internal/semanticmodel/model.generated.json`
  - `go vet ./...`
  - `go test ./...`
  - `go build ./...`
  - `CGO_ENABLED=0 go build ./...`
  Green means all six over the whole repository, not the package you touched. The generated model's
  digest is the byte content of every file listed in `ontology/throughline.json`'s `source_mappings`
  (19 today: the `internal/domain` packages, `internal/mcp/server.go`, `docs/implementation-handoff.md`
  and migrations 0001-0009). Editing any of them moves the digest, including the handoff document and
  the MCP adapter; editing anything else does not, including a new migration, which is unmapped until
  someone adds it. Regenerate and commit the digest in the same change.
- Use Conventional Commits with an imperative subject and no agent-attribution trailers.
