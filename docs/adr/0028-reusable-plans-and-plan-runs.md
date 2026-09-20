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
- **A PlanStep may require only one exact accepted OutputRevision**, and its acceptance is checked
  when the plan is proposed, not when a run copies it. A profile-and-version constraint stays
  available on a live WorkItem through `add_output_requirement`, but expressing one in a definition
  would make run creation reach implicitly into whatever another run happened to accept — the
  `latest` behaviour the model rules out. An approved definition is immutable and copied into every
  run, so a requirement on a revision that was never accepted would block all of them forever.
- **`/` is reserved, and a PlanStep key is unique across the workspace.** A run materializes its
  work as `<step key>/<run sequence>`, the sequence is objective-scoped, and `work_items.key` is
  globally unique. Two objectives that both declared a step called `research` would otherwise
  collide on their first runs, and a hand-written work item called `research/1` would occupy a key a
  future run needs — permanently, because a failed creation consumes no sequence and every retry
  would collide again against a definition that is by then immutable. So: `create_item` refuses a
  key containing the separator, step keys are globally unique, and proposing a step whose
  materialization namespace is already occupied by older work is refused when the plan is written
  rather than when some later run fails.

## Consequences

`propose_plan` changed shape: `items` became `steps`, and a step is `required` unless the MCP caller
marks it `optional`. Plans proposed before this change keep their WorkItems and are readable as
before. `PlanContext.Items` still lists every WorkItem linked to the revision — a legacy plan's own
items, and the items each run materialized from it — because a materialized item keeps `plan_id`
pointing at the revision it came from, which is what the existing plan-approved gate reads. It no
longer says which run an item belongs to; `PlanRunContext` answers that, and so does the item's own
`plan_run_id`.

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

## Follow-on: approving a revision retires nothing

Deferred from the first slice and delivered in the third. Approving a plan revision used to supersede
every earlier approved one, which was coherent when a plan was a batch of work and is wrong when it
is a reusable definition: it made an in-flight run's work fail the plan-approved gate the moment
someone approved a successor. Approval now says only that this definition may be run. Retiring a
revision deliberately is its own audited action and remains outside this objective.

The ordering rule went with it. Refusing to approve a revision older than one already approved only
made sense as a guard for supersession; with every approved revision instantiable, approving an
older draft after a newer one is a legitimate thing to want, and `UNIQUE(objective_id, revision)`
still prevents two plans claiming the same number.

## Deliberately not in this slice

Default Plan Revision, permanent revision retirement and additional dashboard grouping are decided
but outside this objective (`01a0920a-e457-7315-8ae6-626bf20b2148`). They are not replaced by
implicit `latest` behaviour: every run names its revision explicitly.

## Follow-on: write transactions take the lock immediately

Found while proving the concurrency criterion of the second slice. Almost every mutation reads state
and then decides what to write from it — whether a run key exists, how many runs are active, what
version a row is at. Under a deferred transaction the write lock is only requested at the first
write, by which point another writer may hold it; SQLite cannot make the second writer wait, because
both already hold read locks, so it aborts one with `SQLITE_BUSY` however long the busy timeout is.

Two concurrent run creations therefore did stay within the capacity limit, but by accident: both read
a count of zero and one was killed on the lock upgrade. The caller was told "database is locked"
rather than that it was at capacity, and the limit would not have held if the abort had landed
differently. `_txlock=immediate` takes the write lock at `BEGIN`, so the loser waits, reads the
committed state, and is refused by the rule it actually broke. The driver applies it only to write
transactions; read-only ones stay deferred.

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
