# Decision provenance and objective-level measurability

Frozen execution graph for `OBJ-DECISION-PROVENANCE`. Written at plan time, before
implementation. Do not edit mid-wave; annotate at delivery.

The objective adds two relations — `Decision → Question` and `Decision → ContextRecord` —
and changes the `success_metric` lifecycle so an objective's completion becomes checkable.
Both converge on one alarm: a linked context record that stops holding raises `Attention`
on the decisions that rested on it.

```text
N0 Plan/contract [agent, this session]
  -> N1  success_metric lifecycle          [agent]
  -> N4  ADR                               [agent or main]
N1 -> N2a Provenance persistence           [agent]
N2a -> N2b Provenance surface              [agent]
N2b -> N3  Attention trigger               [agent]
N1 + N2b + N3 + N4 -> N5 Verify            [command]
N5 exit 0 for all commands -> N6 Review    [agents, parallel]
N5 non-zero -> N7 Fix [agent] -> N5        (budget 3; then stop and report output)
N6 zero findings -> N8 Metric check        [validator]
N6 findings -> N7 Fix -> N5 -> N6          (budget 5; repeated finding or spent budget -> human gate)
N8 replay test green -> done
N8 red -> N7 Fix -> N5                     (budget 2; then human gate)
```

## Why this barely fans out

`N1` and `N2a` both touch `internal/domain/work/planning.go` — `ContextKind`/`ContextStatus`
from line 10, `Decision` from line 273. A shared file forces an edge, so they cannot run in
parallel. `N2b` needs the types `N2a` writes; `N3` needs the links `N2b` exposes.

That leaves exactly one parallel branch, `N4`, and it is a document. Two nodes wide is below
every materialization threshold: no `.agents/` directory, no `contract.md`, no `owners.json`.
The conversation is the state.

The graph earns its place here through deterministic edge conditions and budgets, not through
parallelism. `go test` decides, not an agent reporting that the code looks right.

## Nodes

| Node | Owns | Token budget | Model | Gate |
|---|---|---|---|---|
| N1 success_metric lifecycle | `internal/domain/work/planning.go` (`validContextTransition`, `validContextStatus`) + test | 25–40k | Haiku 4.5 | `go test ./internal/domain/...` |
| N2a Provenance persistence | migration `0010`, `domain/work/planning.go` (`Decision`), `ports/store.go`, `app/service_test.go` (`memoryStore`), `sqlite/planning_store.go` | 100–150k | Sonnet 5 | store + migration tests |
| N2b Provenance surface | `app/planning.go`, `mcp/server.go` (recordDecision region only), `ontology/throughline.json`, digest regeneration | 100–150k | Sonnet 5 | integration + canonical model tests |
| N3 Attention trigger | app-layer context transition path | 60–100k | Sonnet 5 | integration test |
| N4 ADR | `docs/adr/0028-*.md` | 15–25k | Sonnet 5 (see note) | none — prose |

Budgets are floor-derived: measured file bytes divided by four, times three to five for
system prompt, tool schemas, repeated reads, test output and fix cycles. Calibrated against
this session's own subagents, which ran 68k, 86k, 117k and 188k.

`mcp/server.go` is 134 KB — roughly 34k tokens on its own. `N2b` must grep to the
`recordDecision` region rather than read the file, or one file consumes a quarter of the node's
budget. This instruction is part of the node contract and has no home in Throughline.

`N4` costs 15–25k to produce a 600-token artifact whose decisions already exist on the board.
That is the case graph engineering's "nodes under five minutes do not get their own subagent"
rule exists for. Recorded here as a deliberate open point rather than silently resolved.

## Work item split

`N2` was originally one node. The token estimate — 200–350k across eight layers — is what
split it. "Eight layers" sounded tractable; 300k in a single node that can fail three times
through the verify loop did not. The seam runs between persistence and surface, which keeps
file ownership disjoint even though execution stays sequential.

## Model assignment

No node runs on Opus. The Opus-shaped work — deciding the design — happened during discovery.
What remains is execution against a fixed specification, where a deterministic gate catches the
errors a cheaper model makes.

Model choice deliberately does not live in Throughline. `Capability` is defined there as "a
vendor-neutral ability required or offered by an actor", so a model id would violate the field's
own contract and be wrong within a year. The board records what the work needs; this file
records which model satisfies it.

## Budgets and escapes

| Loop | Budget | Escape |
|---|---|---|
| N5 verify → N7 fix | 3 | Stop. Report the failing output verbatim. |
| N6 review → N7 fix | 5 | Stop and escalate with open findings. A finding recurring after a fix that claimed to resolve it is a wrong fix, not slow progress — escalate immediately. |
| N8 metric → N7 fix | 2 | Human gate. A replay test that will not go green may mean the mechanism is wrong, not the code. |

## Review

`N6` runs `/code-review` and one independent cross-provider read in parallel, findings merged
into one pool the fix loop must clear. If the cross-provider call fails twice, record that the
pass was skipped — a missing data point is not the same as no issues.

## Success metric

`N8` is the objective's own success metric, run as a test: reconstruct the historical case where
decision `01a05d51-78fb` rested on a falsified assumption about the graphify skill, transition
that assumption to `invalidated`, and assert an `Attention` record exists with
`target_kind="decision"` and that decision's id. Today the same sequence produces nothing.

## Feedback

Filled in as nodes deliver. The frozen design above is left untouched; divergence is recorded
here rather than edited away.

### N1 — delivered

Consumed **54,162 tokens against a 25–40k budget**. The file floor was about 4.7k, so the real
multiplier was roughly **11.5×**, not the 3–5× assumed when the plan was written. The gate passed
on the first attempt, so no fix-loop budget was spent; the overrun is baseline cost, not rework.

One measurement, and the smallest node in the graph — so 11.5× is probably an upper bound rather
than the factor. Fixed overhead (system prompt, tool schemas, MCP definitions) is roughly constant
per node and dominates a small one; a larger node amortizes it. The honest reading is that the
original numbers were too low and the true factor sits somewhere between, not that 11.5× is now
the rule.

Reprojected with both bounds:

| Node | Planned | at 6× | at 11.5× |
|---|---|---|---|
| N2a persistence | 100–150k | 138k | 265k |
| N2b surface | 100–150k | 156k | 300k |
| N3 attention | 60–100k | 90k | 172k |

The 150k ceiling that justified splitting N2 sits inside that uncertainty. Three nodes breach it
at the upper bound and none clearly does at the lower. That makes the ceiling the questionable
part, not the nodes: a threshold whose verdict flips across the plausible range is not deciding
anything. Do not re-split on these numbers alone — measure N2a first, since it is the next code
node and will narrow the range far more than reasoning will.

### N1 — what the design missed

**A lifecycle change is a data migration.** Moving `success_metric` off the agreement lifecycle
orphaned every record already written under it, including this objective's own success metric,
which still sits at `proposed`. `TestIntentAndPlanningVerticalSlice` in `internal/sqlite` went red.
No node in the frozen graph covered this. A node was inserted between N1 and N2a
(`TH-PROV-06-STATUS-MIGRATION`) and N2a now depends on it.

**The acceptance gate was scoped to the node, not to the repository.** Criterion 4 asked for
`go test ./internal/domain/...`, which passed while the repository was red. The gate measured the
node's blast radius instead of the state it left behind. Binding correction for the remaining
nodes: green means `go test ./...` passes. It could not be applied by editing the criterion —
acceptance criteria are write-once — so it lives as a requirement record on the objective and in
the new node's own criteria.

**Two model gates fired that the graph did not anticipate**, both at the start of execution:
work items refuse to leave `backlog` unless the objective is in `execution`, and `claim_item`
enforces `required_capabilities` with no MCP path to grant one. The first is useful — it is the
only phase boundary the model enforces. The second forced clearing the requirement on N1 to
proceed at all.

### TH-PROV-06 — delivered

The inserted migration node. **69,875 tokens against a 150k estimate**, on a floor near 13k, so
roughly 5.4× — against N1's 11.5× on a 4.7k floor. Two points give a better shape than a
multiplier: about **45k fixed overhead per node plus twice the file floor**. Reprojected on that,
the remaining nodes land near 89k, 94k and 74k — comfortably under the ceiling that justified
splitting N2. **The split was probably unnecessary**, and the figure that justified it was too
high by a factor of three.

### TH-PROV-07 — delivered, and not the node that was scoped

Exposing `transition_context` was added mid-flight, before merging, because the lifecycle change
would otherwise have shipped as something nobody could drive: a metric could be created and never
moved, so the completion gate it enables stayed unusable.

**136,938 tokens against roughly 70k.** Not an estimation error. The node found that
`TransitionContext` was the only mutating command without idempotency, and that the MCP wrapper
reads the idempotency key off the raw request regardless — so wiring the tool as briefed would
have committed the write and returned an error to the client. Fixing that was a prerequisite
nobody had planned. A budget predicts the planned work, not what the work turns out to require;
breaching one means the node found something or is lost, and only looking tells you which.

Also: registration takes three sites, not the two the brief named. The third, a `resultSchema`
switch, panics at server startup on an unrecognised tool — a missed case fails loudly.

### The review node had no budget, and cost more than implementing

| Node | Implementation | Review |
|---|---|---|
| N1 | 54k | — (skipped) |
| TH-PROV-06 | 70k | 94k |
| TH-PROV-07 | 137k | 111k |

Reviewing consistently cost at least as much as implementing. The frozen graph gives `N6` a
loop budget but no token budget, because budgets were only assigned to implementation nodes.
That was an oversight, not a judgement.

Two passes ran degraded and are recorded as such rather than as clean: the cross-provider read
timed out and was skipped, and the Claude-side review ran on Sonnet rather than Opus after an
Opus rate limit. A missing data point is not the same as no issues.

**N1 was accepted without any review at all.** The work-item status `review` was mistaken for
the review node having run. The `gofmt` defect that later broke CI was in an N1 file and was
caught only when a subsequent node's diff was reviewed. A status is not proof that the action it
names took place.

### The graph ended at Done, and the work did not

Nothing in the frozen graph commits, opens a pull request, releases, or deploys. The migration
therefore reached the live database only because someone did those steps by hand afterwards:
merge, tag `v0.5.0`, `brew upgrade`, restart the daemon. Two things worth carrying forward from
that stretch — upgrading the binary does not update a running daemon, and migrations apply on
first workspace access rather than at daemon start, so `doctor` reporting a healthy new version
is not evidence that a migration ran.
