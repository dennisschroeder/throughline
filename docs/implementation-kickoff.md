# First implementation task

Implement Workgraph's executable foundation milestone and one thin domain-neutral vertical slice.

## Source of truth

Read `docs/implementation-handoff.md` completely before making architectural decisions. Treat it as
canonical. Make the smallest reversible assumption when implementation details remain ambiguous and
record consequential decisions as ADRs.

Do not attempt the entire V1 in this task.

## Scope

1. Inspect the repository and write a short implementation plan.
2. Scaffold the Go module and the smallest useful form of the recommended repository structure.
3. Produce one `workgraph` binary.
4. Implement:
   - `workgraph init`;
   - workspace configuration and database-path resolution;
   - SQLite connection setup and required pragmas;
   - embedded, versioned, transactional migrations;
   - schema initialization and idempotent reopening;
   - seeded generic OutputProfiles as ordinary database data.
5. Implement one transport-independent vertical slice:
   - create an Objective;
   - create a Plan;
   - create a WorkItem;
   - assign an ExpectedOutput using an exact OutputProfile version;
   - retrieve the resulting WorkItem with its structured context.
6. Add unit and SQLite integration tests.
7. Add initial ADRs for:
   - Go and the single-binary architecture;
   - SQLite driver and authoritative persistence;
   - migration strategy;
   - identifiers and timestamps;
   - transaction and concurrency boundaries;
   - immutable output contracts;
   - canonical ExternalAction authorization subjects and delegated authority.
8. Add build, test, initialization, and current-scope documentation.

## Architectural constraints

- Go is the V1 language. Prefer a CGo-free SQLite implementation unless a documented blocker is
  discovered.
- SQLite is authoritative.
- Core domain logic cannot depend on MCP, CLI, UI, or SQLite packages.
- MCP, CLI, and the later UI are adapters over the same application/domain layer.
- The vertical slice must require no Git repository, branch, commit, pull request, CI system, or test
  suite.
- Do not hardcode behavior based on OutputProfile names. Profiles are governed, versioned database
  entities.
- Preserve capability versus authority as separate concepts.
- Workgraph does not orchestrate agents or execute external actions.
- Keep external-action persistence and execution outside this milestone, except for the canonical
  authorization-subject specification and fixtures required by the handoff.
- Avoid speculative abstractions and unused directories.

## Verification

- Format all Go code.
- Run static checks, build all packages, and run the complete test suite.
- Verify `workgraph init` creates a usable database and is safe to run again.
- Verify migrations and seeded profiles are deterministic and idempotent.
- Verify no domain behavior branches on profile names.
- Verify the vertical slice with a non-code scenario such as producing a research dossier or
  designing an agent skill.

## Completion report

Report implemented functionality, repository structure, verification results, ADRs, assumptions,
unresolved risks, and the recommended next bounded milestone. Implement the code, tests, and
documentation; do not stop after producing only a plan.
