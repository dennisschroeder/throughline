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

All nodes completed; every back-edge budget used 0-1 of its allowance except N14's driver-fix loop
(3 of a 3-budget, on throwaway scaffolding, not shipped code).

- **N0**: graphify over 78 files (56 code, 22 docs) — 1105-node/3526-edge/58-community graph,
  confirmed the architecture already understood from direct reads. Contract and owners.json frozen
  before any code touched.
- **N1-N4** (fan-out, parallel): all four landed clean on the first pass, zero file-ownership
  violations (`git status` checked against `owners.json` after the wave). N3 correctly flagged an
  unverifiable fact (archive version-string casing) with a placeholder rather than guessing.
- **N5 fan-in**: reconciled N3's placeholder against N2's actual GoReleaser snapshot output (leading
  `v` stripped from `{{.Version}}`, kept in `{{.Tag}}`); no other cross-node conflicts found.
- **N6 verify**: green on the first pass — gofmt/vet/build/CGo-free-build/full-test-suite/20x
  concurrency test all clean. Budget 3, spent 0 (before N9's fixes; re-run clean again after).
- **N8 release dry run**: green on the first pass — 4 archives + checksums, extracted binary ran
  and reported the injected version. Budget 3, spent 0.
- **N9 review** (`/code-review`, standards + spec in parallel; cross-provider deliberately skipped
  per instruction, recorded as skipped not clean): 5 findings, 1 fix iteration (budget 5, spent 1).
  Two were real, live-verified defects — a GoReleaser ldflag using `{{.Version}}` instead of
  `{{.Tag}}` (would have shipped `throughline version` reporting `0.1.0` instead of the
  acceptance-criteria-required `v0.1.0`), and a missing `permissions: contents: write` on the
  release workflow (would 403 against a fresh repo's default read-only `GITHUB_TOKEN`). Both fixed
  and reverified live before proceeding — the second was confirmed load-bearing when the real
  release workflow later succeeded with it in place.
- **N10-N13** (gates + push + tag): both human gates confirmed against the plan already approved by
  the user this session. CI green in ~1m on first push (budget 3, spent 0). Release workflow green
  in 1m5s on first tag push (budget 3, spent 0); published 4 archives + checksums at
  `github.com/dennisschroeder/throughline/releases/tag/v0.1.0`.
- **N14 first user journey**: real download/verify/install against the published release confirmed
  the `{{.Tag}}` fix end to end (`throughline version` → `v0.1.0` from the actual artifact). The
  two-client MCP scenario needed 3 driver-fix iterations against real schema and domain constraints
  discovered only by running it (objective phase transitions can't skip `planning`;
  `list_ready_items` requires `actor_id`; governed JSON schemas restrict several nested objects to
  fixed key sets). All fixes were to the throwaway driver, not the shipped product — zero product
  code changed during N14. Full scenario passed: deterministic claim conflict, output-reuse
  readiness, restart/resume, and a complete external-action lifecycle with zero real network calls.
- **N15**: this annotation, plus the handoff document's Outcome section and the final commit to
  `main`.

## Feedback

The plan-time graph did not anticipate that N9's review would find defects requiring a **second**
GoReleaser snapshot dry run and a second real network round-trip (repo already public, CI already
green) to reverify — the graph's `N9 -> N7 fix -> N6 -> N9` back-edge was drawn correctly, but N8
(the release dry run) was not explicitly included in that back-edge in the original design, even
though both N9 findings were release-config defects that only a dry run could have caught with
confidence. In practice N7's fixes were reverified against N6 *and* a fresh N8 run before
proceeding, which was the right call, but the frozen graph undersold how tightly N8 and N9 needed
to loop together for GoReleaser-config-shaped findings specifically.

N14 is where the design most understated real cost: it was drawn as a single node with an implicit
budget, but building a correct MCP driver against a system with optimistic-concurrency versioning,
phase-transition rules, and strict nested JSON schemas is itself a small implementation task with
its own verify-fix loop. Treating it as "N7 fix (budget 3, then gate)" undersold the number of
distinct failure classes (phase transitions, required-but-optional-looking fields, governed schema
key restrictions) a single first run would surface — three fixes across three genuinely different
causes exhausted most of a budget-3 loop on what looked like one node. A future version of this
graph should either give the journey driver its own explicit verify-fix sub-loop, separate from the
outer "did the journey pass" gate, or budget it at parity with N9 (5) rather than N6/N8 (3).

Nothing else diverged: every other node's actual budget spend was 0 against a budget of 3-5, and no
node needed to escalate to a human gate beyond the two gates the design always required.
