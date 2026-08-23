# v0.1.0 release engineering

Frozen execution graph for `docs/v0.1.0-release-handoff.md`: make Throughline publicly
installable and prove it with the bounded first user journey. No new domain feature; only
release packaging, operational documentation, and the acceptance scenario (Product Decision
0001).

```text
N0 Plan / contract [agent, fan-in owner]
   writes .agents/v0-1-0-release-engineering/{contract.md,owners.json,journal.md}
   writes this trail doc (frozen design)
   |
   +-- fan-out over disjoint files ----------------------------------+
   |                |                 |                  |           |
   v                v                 v                  v
 N1 Version      N2 CI + release   N3 Front door      N4 Ops docs
 [agent]         config [agent]    docs [agent]       [agent]
   |                |                 |                  |
   +----------------+--------+--------+------------------+
                             v
                    N5 Fan-in: reconcile [agent]
                    validator: version string, artifact names, install paths
                    and MCP config agree across N1-N4
                             |
                             v
                    N6 Verify [command]
                    gofmt -l / go vet / go test ./... / go build ./...
                    / CGO_ENABLED=0 go build ./... / targeted slice tests
                    |                          |
                 non-zero                   all exit 0
                    v                          |
                 N7 Fix [agent] --> N6         |   budget 3, then stop and report verbatim
                                               v
                    N8 Release dry run [command]
                    goreleaser release --snapshot --clean
                    validator: 4 archives + checksums file, correct names,
                    extracted binary runs and reports a snapshot version
                    |                          |
                 fails                       passes
                    v                          |
                 N7 Fix --> N6 --> N8          |   budget 3
                                               v
                    N9 Review [subagent] /code-review standards + spec, parallel
                    cross-provider pass deliberately skipped (recorded, not treated as clean)
                    |                          |
                 findings                   zero findings
                    v                          |
                 N7 Fix --> N6 --> N9          |   budget 5; repeated finding or spent budget
                                               |   -> human gate
                                               v
                    N10 Gate: publish public repository [human]
                    confirm slug, description, visibility at creation time
                                               |
                                               v
                    N11 Push main -> CI [command]
                    |                          |
                  red                        green
                    v                          |
                 N7 Fix --> N6 --> N11         |   budget 3, then stop and report the log
                                               v
                    N12 Gate: tag and publish v0.1.0 [human]
                                               |
                                               v
                    N13 Tag + release workflow [command]
                    validator: release page carries 4 archives + checksums.txt
                                               |
                                               v
                    N14 First user journey [agent + commands]
                    real download, checksum verify, install on PATH,
                    init, two independent MCP stdio client sessions,
                    9-step journey -> docs/first-user-journey.md
                    |                          |
                  fails                     passes
                    v                          |
                 N7 Fix (budget 3, then gate)  v
                    N15 Close out [agent]
                    annotate this doc with actual edge outcomes + Feedback,
                    add Outcome section to docs/v0.1.0-release-handoff.md,
                    commit journey + trail docs to main
```

## Node ownership (fan-out N1-N4)

| Node | Files it may touch |
|---|---|
| N1 | `internal/cli/version.go`, `internal/cli/cli.go`, `internal/cli/cli_test.go` |
| N2 | `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.goreleaser.yaml`, `.gitignore` |
| N3 | `README.md`, `docs/install.md` |
| N4 | `docs/release-checklist.md`, `docs/development.md` |

Full contract (shared facts, symbol names, name templates) is in
`.agents/v0-1-0-release-engineering/contract.md`, frozen for the wave.

## Budgets and escapes

Same as the standard card graph: verify->fix 3, review->fix 5 (repeated finding or spent
budget escalates to a human gate), CI red->fix 3. No cross-provider review pass — the
`gpt-5.6-sol` route is explicitly out of scope for this run per user instruction.

## Delivery annotation

_Filled in at N15, from `journal.md` and `nodes/*.json`._

## Feedback

_Filled in at N15, even if the answer is "nothing to report"._
