# Reusable plans and separate Plan Runs

Frozen execution graph for `OBJ-REUSABLE-PLAN-RUNS`. Written at plan time, before implementation,
against approved Throughline plan `01a0b546-af3e-7c6f-a13d-974518a82e86`, revision 2. Do not edit
the frozen design below mid-wave; divergence is recorded in **Feedback** at delivery.

The objective introduces a definition/instance split the current model does not have. Today
`propose_plan` writes executable `WorkItem`s directly; afterwards a Plan revision carries reusable
`PlanInput`s and `PlanStep`s, and `create_plan_run` materializes one fresh `WorkItem` per Step into
a durable, separately auditable `PlanRun`.

```text
N1 RPR-FSR  first safe Run          [agent, this session]
  -> N2 RPR-RR   repeatability, concurrency, recovery
  -> N3 RPR-REA  revision evolution and Run adaptation
  -> N4 RPR-CC   contract Legacy compatibility and prove the objective
  -> delivery

Each implementation node I:
  I [agent] -> G [the exact six repository commands, in AGENTS.md order]
  G red   -> I fix -> G                                   budget 3, then stop and report verbatim
  G green -> R1 [result review, gpt-5.6-sol]  \
          -> R2 [KISS review, gemini-3.1-pro-high]  } fresh readers, no repo write access
  R1+R2 -> D [disposition: fixed now | filed | rejected with one sentence]
  D has an unresolved material finding -> I fix -> G -> R  budget 5, then stop and put to Dennis
  D clear -> commit -> close the slice in Throughline -> next node ready
```

Nodes are strictly sequential. The hard dependencies are recorded on the board
(`RPR-FSR <- RPR-RR <- RPR-REA <- RPR-CC`) and they are real: every node rewrites the same
`internal/domain/work`, `internal/app`, `internal/sqlite`, `internal/mcp` and
`internal/semanticmodel` surfaces, so nothing may fan out. That is the honest answer to graph
engineering's parallelism question here — the graph earns its place through deterministic edge
conditions and budgets, not through width.

## The deterministic edge condition

Every node's edge is the project's own gate, from `AGENTS.md`, run over the whole repository in
this order:

1. `test -z "$(gofmt -l cmd internal)"`
2. `go generate ./internal/semanticmodel && git diff --exit-code -- internal/semanticmodel/model.generated.json`
3. `go vet ./...`
4. `go test ./...`
5. `go build ./...`
6. `CGO_ENABLED=0 go build ./...`

Green means all six over the whole repository. An agent's report that the code looks right closes
nothing. Because each slice touches files in `ontology/throughline.json`'s `source_mappings`, step 2
is load-bearing on every node, not only where the domain changes.

Baseline before N1: all six green at `81604f8`.

## Nodes

| Node | Board key | Owns | Gate beyond the six commands |
|---|---|---|---|
| N1 | RPR-FSR | Objective mode + `max_concurrent_runs`; `PlanInput`, `PlanStep`, `PlanRun`, `RunInputBinding`, WorkItem Run/origin provenance, Legacy marker; `propose_plan` writing definitions instead of executable items; `create_plan_run`, `get_plan_run`, `close_plan_run`; run-active gating of claim/transition/authority/action-start | Application and domain tests cover every positive and rollback path; a failed `create_plan_run` stores neither Run nor WorkItems; reopen preserves both. |
| N2 | RPR-RR | Objective-scoped actor-independent `run_key` replay before capacity; atomic capacity under concurrency; disjoint identities across Runs; `list_plan_runs`, Run summaries in objective context, item→Run provenance, ready/list scoping | Concurrency tests prove the limit cannot be exceeded; a second Run leaves the first byte-identical after reopen; no query resolves an implicit latest Run. |
| N3 | RPR-REA | Multiple approved revisions instantiable; Run-local executable WorkItems; every Step materialized exactly once, optional-step cancellation releasing only local obligations; cross-Run bindings to one exact accepted `OutputRevision`; external reference identity; evidence-backed draft revisions | Approving a later revision changes no existing Run; cancelling an optional step releases only that item's local criteria, outputs, requirements and not-started actions. |
| N4 | RPR-CC | Legacy preservation and marking; removal of direct Plan→executable-WorkItem creation, automatic approved-plan supersession, implicit profile-based cross-Run selection and every new outside-Run execution bypass; migration of every reader, contract, read model, ADR, handoff and ontology mapping | Migration from a populated pre-Run database plus repeated reopen and daemon/MCP round trips; the twenty acceptance scenarios; the frozen graph annotated; the objective ready for evaluation. |

`internal/mcp/server.go` is 3,347 lines and `internal/app/planning.go` 1,603. Nodes grep to the
region they change rather than reading either whole; that instruction is part of the node contract
and has no home on the board.

## Expand, migrate, contract

This is the sequence the change's blast radius asks for, and the board already encodes it. N1 adds
the Run model **beside** existing behavior so nothing breaks. N2 and N3 migrate what depends on it
while the old form still stands. N4 deletes the old form last, blocked by all three. No node is
allowed to leave the repository red on the promise that a later one repairs it: each node's own gate
is the whole gate.

## Review

Two fresh readers per node, both pinned by exact model id so a nickname cannot drift, both from a
different family than the author:

| Pass | Model | Asks |
|---|---|---|
| R1 result review | `gpt-5.6-sol` (OpenAI) | Does the delivered change satisfy this slice's acceptance criteria, and is it correct — atomicity, rollback, gating, immutability, migration honesty? |
| R2 KISS review | `gemini-3.1-pro-high` (Google) | Is anything here more machinery than the criteria require — a state, table, command, flag or abstraction that could be removed without losing a recorded decision? |

Reviewers read the diff and the criteria; they do not write to the repository. Every finding gets one
of three recorded dispositions — fixed now, filed as separate work, or rejected with one sentence of
reasoning — and dispositions carry forward across passes so a settled finding does not return and eat
the budget. A skipped or degraded pass is recorded as skipped or degraded; a missing measurement is
not a clean one.

## Budgets and escapes

| Loop | Budget | Escape |
|---|---|---|
| Gate → fix | 3 | Stop. Report the failing output verbatim with what was tried. |
| Review → fix | 5 | Stop and put the open findings to Dennis. A finding recurring after a fix that claimed to resolve it is thrashing — escalate immediately, do not spend the rest of the budget. |

Beyond these, execution stops only for a necessary scope change or missing authority. Between green
slices it does not wait for confirmation.

## Non-goals

Carried from the approved plan and the accepted decisions, restated here so a node cannot quietly
widen: no scheduling or cadence, no conditional workflow evaluation, no agent selection or
orchestration, no external-effect execution. Default Plan Revision, permanent revision retirement and
additional dashboard grouping are decided but deliberately outside this objective
(`01a0920a-e457-7315-8ae6-626bf20b2148`) — and are not replaced by implicit `latest` behavior.

## Feedback

Filled in as nodes deliver. The frozen design above is left untouched; divergence is recorded here
rather than edited away.

_Nothing to report yet — N1 has not delivered._
