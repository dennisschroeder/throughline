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

To be written at delivery, before the objective moves to evaluation. Record for each node what
it actually consumed against its budget, which edges were taken, and where the design diverged
from reality. The 150k per-node ceiling that drove the `N2` split is a setting, not a
measurement — this section is what corrects it.
