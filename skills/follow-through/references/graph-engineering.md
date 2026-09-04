# Graph engineering

How to construct an execution graph. The rules a finished graph must satisfy are in `SKILL.md`;
this file is the construction method behind them.

Work runs on an explicit graph. **Nodes** are steps — an agent, a command, a validator, a human
gate — and **edges** decide what happens next, under which condition, how often, and in parallel
with what.

The graph exists to catch the non-deterministic intelligence of a model inside a deterministic
architecture. Retries, failure paths, budgets, validation and approval become part of the structure
instead of hopeful sentences inside a prompt.

## How to build one

List the nodes → mark each one's type → draw the real dependencies as edges → write the condition
on every branch → put a budget on every back-edge → name the state that flows.

Draw it **before** implementing, in the response, where it is cheap to correct:

- any work with more than one unit,
- anything with a real failure path,
- anything you intend to parallelise.

Skip it for single-node work. Most small fixes are one node; a graph for them is ceremony.

## Node types

| Node | Deterministic? | Examples | Rule |
|---|---|---|---|
| **Agent** | no | implement a module, explore, review a diff | Needs a written contract: what it gets, what it returns, when it is done. |
| **Command** | yes | the checks the project's `AGENTS.md` names | The source of truth. Run the command; an agent *reporting* its result is hearsay. |
| **Validator** | yes | acceptance criteria met? change inside scope? nothing left open? | Cheap check that turns a proposal into a decision. |
| **Gate** | human | risky merge, destructive migration, public visibility, spend | Blocks the edge until a person answers; their answer is the edge condition. |

The Deterministic column is what the load-bearing rule in `SKILL.md` operates on: a node in the
`no` row can only ever propose, so an edge leaving it is decided by one of the `yes` rows.

## Edge types

- **Sequential** — B cannot start until A is done.
- **Conditional** — branch on a deterministic predicate: exit code, count, boolean.
- **Loop / back-edge** — retry, fix→verify. **Always carries a budget.**
- **Fan-out** — independent nodes in parallel.
- **Fan-in** — a join, with an explicit synthesis node.

Rules that keep a graph from lying to you:

1. **Every back-edge carries a budget and an escape node.** An unbounded loop is a hang with
   extra steps.
2. **Fan out only over disjoint things.** Shared things force an edge. Shared *concepts* usually do
   not.
3. **Fan-in needs a synthesis node.** Two branches whose outputs merge implicitly produce a conflict
   nobody owns.
4. **A subagent earns its briefing at a few minutes of work or more.** Below that, briefing costs
   more than the work.

**Which model a node gets follows its gate, not its difficulty.** Assign per node, and write the
assignment down beside the graph rather than into the coordination state — a model id is not a
vendor-neutral capability and will be wrong within a year.

**Every node also carries a fixed cost before it does any work** — system prompt, tool schemas, tool
definitions — and it is charged again for each node the graph adds. Splitting one node into two
therefore buys clarity and a smaller blast radius, not cheapness. Measure that floor in your own
setup instead of guessing: in the project this document came from, with a large MCP surface, it ran
near 45k tokens per node plus roughly twice the size of the files the node had to read, which was
enough to show that a split made earlier on a ceiling three times too high had not been needed.

## State on the edges

Name what flows between nodes, explicitly and minimally: the work item, the branch, the changed
things, the last failure output, the iteration counter. Subagents start cold — anything not in the
state gets re-derived at full cost, or silently guessed.

For a narrow graph that is enough: the conversation *is* the state. Materialise it once the fan-out
gets wide.

### Tracked state for wide fan-out

**Threshold — materialise when any of these is true:**

- four or more concurrent nodes,
- more than one wave,
- nodes isolated in separate working copies,
- work that must survive a session restart.

Below that it is ceremony. Three nodes fanning straight into a join do not need a filesystem.

**Layout** — `.agents/<item-slug>/`, ignored by version control: working state, discarded once the
item ships. The permanent record is separate — see *Permanent trail* below.

```text
.agents/<item-slug>/
  contract.md        written by the plan node, frozen for the whole wave
  owners.json        node → the things it may touch
  nodes/<node>.json  one file per node, exactly one writer
  journal.md         append-only: edge taken, condition value, budget spent
```

**The five rules that make it work:**

1. **One writer per file, always.** No shared mutable file, no locks. Concurrency safety by
   construction. A node reads anything and writes only its own result file.
2. **The contract is frozen during a wave.** It holds what the branches must agree on: signatures,
   types, ownership, shared vocabulary. A branch that needs to change it stops and escalates to the
   fan-in node. This prevents the classic fan-out failure
   where two branches each invent a different version of the same interface.
3. **`owners.json` is a deterministic gate.** After the wave, diff what actually changed against the
   ownership map. Something touched by a node that does not own it is a failed edge — reject and
   re-run that node, do not turn it into a merge exercise.
4. **Every node result carries the same shape**: `status`, `changed`, `public_surface` (what it
   added or changed that others can call), `assumptions`, `follow_ups`. `assumptions` is the field
   that earns the whole directory — it is where divergence becomes visible before the join.
5. **The journal is append-only.** It answers "which edge was taken and why" after the fact, and it
   makes resume possible: on restart, read journal plus node files and re-enter at the first
   incomplete node.

**Fan-in reads all node files, reconciles, and writes the next contract.**

When the user opts into the Workflow tool, this comes for free: state is script variables, node
results are schema-validated returns, the run journal plus resume-from-run-id give journal and
resume. The directory above is the by-hand equivalent for when you are the scheduler.

## Permanent trail

`.agents/<item-slug>/` is scratch, discarded once the item ships. The two-pass trail `SKILL.md`
requires lives where the project's `AGENTS.md` says — commonly `docs/graphs/<item-slug>.md` —
tracked in version control and reviewed alongside the change. The delivery annotation is assembled
from `journal.md` (edge conditions and budgets spent) and `nodes/<node>.json` (final statuses).
Whatever tracker holds the work keeps the short pointer it already had, not a copy of the trail.

## The standard graph

The default execution graph. Everything else is a variation of it.

```text
                Work item (ready)
                      │
                      ▼
                  Plan / split ─────────── fan-out over disjoint things
                      │
       ┌──────────────┼──────────────┐
       ▼              ▼              ▼
   Agent: A       Agent: B       Agent: C      (parallel, independent)
       └──────────────┼──────────────┘
                      ▼
                 Fan-in: wire up                (single node, one owner)
                      │
                      ▼
          Verify: the project's whole gate      (commands, deterministic)
                      │
            ┌─────────┴─────────┐
          red                 green
            ↓                   │
       Agent: fix               │
            │                   │
            └─→ back to Verify  │   budget: 3, then stop and report
                                ▼
       Review ── own-family pass ‖ cross-provider pass ── always, both
                 │        + fan-out for high risk: correctness │ security │ perf
                 ▼
            findings? ── yes ──→ Agent: fix ──→ back to Review  (budget 5, then escalate)
                 │
                 no
                 ▼
            Gate: risky? ── yes ──→ ask a person
                 │
                 no
                 ▼
              Ship → CI ── red ──→ fix (budget 3)
                 │
               green
                 ▼
               Done
```

The graph ends at Done, and the work often does not. Whatever carries the change into the world —
commit, pull request, release, deployment, migration — is either a node or it is being done by hand
without anyone having decided to.

## Cross-provider review

The independent read from a different provider runs in parallel with the same-family pass, findings
merged into the same pool the review→fix loop has to clear.

**Pin the model id, do not resolve a nickname.** Nickname tables default to newest-in-family and
drift silently when a new alias appears. This is the one review component allowed to hardcode an
id, exactly so it does not drift.

The call can fail outright or hang. One retry. If it is still down, record that the cross-provider pass
was skipped and why.

Feed it the change plus the acceptance criteria as a self-contained message — it has no access to
the conversation. Its findings go through the same three dispositions as any other. A finding it
raises does not get waved off just because the same-family pass missed it too.

## Execution

- **By hand (default).** You are the scheduler: independent nodes go out as parallel agent calls in
  one message, commands run in the shell, gates come back to the user. Report which edge was taken
  and why.
- **Workflow tool.** The script *is* the graph — `parallel()` is a fan-in barrier, `pipeline()` is a
  per-item chain without one. Only on the user's explicit request; it spawns many agents and costs
  accordingly.

Either way, the main agent draws the graph and prints it in the chat before any node runs, while
it is still cheap to correct.

## Knowledge graphs

Graph engineering here means execution structure — `Agent → Command → Validator → Gate`. A knowledge
graph describes entities and their relations. Same word, different subject; work that needs one is
doing domain modeling.
