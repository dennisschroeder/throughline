# 0028 — Reusable plans and separate Plan Runs

Status: accepted
Date: 2026-09-20
Objective: `OBJ-REUSABLE-PLAN-RUNS`, slice `RPR-FSR`

## Context

A Plan revision used to be a batch of executable WorkItems: `propose_plan` wrote the items, and
approving the plan accepted them. That makes a plan a one-shot. Running the same agreed workflow a
second time meant either resetting the items it produced — destroying what the first execution
recorded — or copying the plan by hand into a new revision nobody reviewed.

The objective's accepted decisions require the opposite: every approved revision instantiable any
number of times, each execution permanently separate and auditable, and no scheduling, branching,
agent selection or external effect moving into Throughline.

## Decision

Split definition from instance.

A Plan revision becomes a **definition**: named `PlanInput`s it declares, and `PlanStep`s it is made
of. Approving it commits the definition and creates no work.

A `PlanRun` is one execution of one exact approved revision. Creating it materializes exactly one
fresh WorkItem per step — with that item's own criteria, expected outputs, output requirements,
capabilities and copied ExternalActions — or stores nothing at all.

Supporting choices, each following a recorded decision on the objective:

- **A run is created active.** There is no preparatory state, no `bind_plan_run_input` and no
  `activate_plan_run`. Every required binding is supplied at creation, so there is nothing a created
  run could still be waiting for, and no half-prepared run to reason about.
- **One status field carries the terminal result** — `active`, `succeeded`, `failed`, `cancelled` —
  rather than a lifecycle plus an outcome. `finished` only ever meant succeeded or failed.
- **`origin` on WorkItem decides executability.** `plan_step` work executes while its run is active;
  `unplanned` work, recorded directly through `create_item`, never executes; `legacy` work predates
  Plan Runs and keeps exactly the behaviour it had. Cancelling is exempt from the gate, because
  withdrawing work is not executing it and a proposal must stay retractable.
- **The run key is resolved objective-wide and actor-independently, before capacity.** It names the
  business execution instance across harness retries and actor changes; a retry must find the run it
  already created rather than be refused for capacity that run itself occupies.
- **A PlanStep may require only one exact accepted OutputRevision.** A profile-and-version
  constraint stays available on a live WorkItem through `add_output_requirement`, but expressing one
  in a definition would make run creation reach implicitly into whatever another run happened to
  accept — the `latest` behaviour the model rules out.

## Consequences

`propose_plan` changed shape: `items` became `steps`, and a step is `required` unless the MCP caller
marks it `optional`. Plans proposed before this change keep their WorkItems and are readable as
before; `PlanContext.Items` is now the legacy half of that contract and is empty for every plan
proposed since.

The migration is purely additive and invents nothing. Every WorkItem that existed before it is
marked `legacy` and keeps its data, activity and behaviour, with no run of its own — including items
with no plan, which were executable under the old rules and would be falsified by re-labelling them
as non-executable proposals. No run, run key, binding, plan step or activity is synthesized to
explain historical work. Reusing an old plan means proposing a new revision from its demonstrable
contents and having that reviewed.

`ObjectiveMode` is fixed at creation, because changing it later would retroactively change what
completing the objective and every run recorded under it meant. `max_concurrent_runs` is not fixed:
it can be raised or lowered through `patch_objective`, since otherwise an objective created under the
default could never run two things at once — a limitation no decision asks for.

## Deliberately not in this slice

Approving a later revision still supersedes earlier approved revisions, which contradicts
"multiple approved revisions remain instantiable". That is `RPR-REA`'s first acceptance criterion and
a hard dependency of this slice's successor; repairing it here would pull a later slice's work
forward rather than expand beside the existing behaviour.

Default Plan Revision, permanent revision retirement and additional dashboard grouping are decided
but outside this objective (`01a0920a-e457-7315-8ae6-626bf20b2148`). They are not replaced by
implicit `latest` behaviour: every run names its revision explicitly.

## Alternatives considered

**Five tables for a step's owned child data.** A step's criteria, expected outputs, output
requirements, capabilities and action proposals are a frozen snapshot copied per run, never queried
across steps and referenced by no other row. They live as one immutable `definition_json` record.
Step parentage and step-to-step prerequisites are real columns and a real table, because those are a
graph whose integrity is checked.

**Synthesizing a historical run per existing plan.** Rejected by decision
`01a0920a-e454-7caa-a6a7-8f7c7eaad938`: even apparently unambiguous current WorkItem fields do not
prove what was originally approved, and a synthetic run would claim a precision the data does not
have.
