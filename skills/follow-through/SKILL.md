---
name: follow-through
description: Use when a vague idea needs to become delivered work — "I have an idea", "should we build…", "ich habe da eine Idee", "sollten wir … bauen" — or when an objective needs scoping, a plan proposing or approving, a work item executing or reviewing, or something judging done. Also use whenever earlier work is picked up again — "let's continue with X", "where did we leave off", "lass uns weitermachen mit X", "wo waren wir stehengeblieben" — since where it stands is recorded outside the conversation, and whenever work must be split across subagents or drawn as an execution graph.
---

# Follow-through

Rules for carrying work to delivery without lying to yourself about where it stands.

At each point where something is called ready, done, or delivered, every rule below has either
applied or demonstrably does not bear on this work. Skimming for the applicable ones is how they
stop applying.

This is not a procedure. It names no tool and no order — phases and states live in Throughline and
are authoritative through `get_semantic_model`. Read the objective to know which phase you are in.

Nor does it name commands. What counts as a passing check, where a trail is kept, what the
non-goals are: each project's business, written in its `AGENTS.md`. Every command comes verbatim
from there. If it is not written down, ask.

Nothing here is specific to software. Where it says "thing", software means files.

## From an idea to an objective

An idea is not yet an objective. Recording one spends a person's attention and commits them to a
shape, so offer it once the idea has survived the first exchange and they mean to pursue it — not
at first mention. An objective exists because someone intends the work, not because a tool needs
somewhere to put it.

Sharpen before recording: who it is for, and what changes if it works. An objective that cannot
fail is not one.

**Find the load-bearing assumption first** — the one that, if false, changes the shape of everything
downstream rather than one branch of it. Ask it while the objective is still cheap to redraw.

Work the questions the way you would want to be interviewed: one at a time, waiting for each answer
before the next; several at once is bewildering. Anything a fact settles you go and find yourself —
filesystem, tools, the model. The decisions are the person's, and each one goes to them and waits.
Nothing gets acted on until they confirm you have understood the same thing.

Discovery ends when no open question would change the shape of the plan — not when every question
is answered. Test each by asking whether the set of work changes depending on how it comes out.
Those that would not may stay open into execution. The pull to stop asking and start building is
usually that same moment arriving on its own; it is worth trusting once you have checked it.

## The graph

> Prompt engineering shapes the instruction. Context engineering shapes the information space.
> Tool engineering shapes the permissions. **Graph engineering shapes the execution space.**

Work with more than one unit, a real failure path, or anything meant to run in parallel runs on an
explicit graph, drawn in the response before the work, where it is cheap to correct. Single-step
work skips it — a graph there is ceremony.

**The load-bearing rule: every probabilistic node is followed by a deterministic edge condition.**
An agent's output is a proposal; a command or a validator decides whether the edge is taken. An
agent judging its own work is the same node run twice and gates nothing.

**The gate is also what makes a weaker model safe.** Behind a deterministic edge condition, use the
cheapest model that can follow the specification: the gate catches what it gets wrong and cannot
tell which model wrote the work. Where no gate exists — designing, planning, judging, reviewing —
a cheaper model saves nothing and spends it at the wrong end.

Every node reports what it did, what the deterministic gate said, and which edge was taken. A graph
whose path you cannot reconstruct afterwards is prose with arrows in it.

Node types, edge rules, the state that flows between nodes, and the standard graph are in
`references/graph-engineering.md`. Read it before drawing one.

## A report about a state is not the state

Anything an agent tells you about the world is **hearsay** — including its account of a check, its
account of a write it just made, and the status field it moved. Run the check, read the state back,
look at the item. Hearsay never closes an edge.

Quote the tool's own output; a paraphrase of a result is not the result. And a status is only ever
a claim about what should happen next: an item sitting in `review` means it is waiting to be
reviewed, and someone still has to look.

**Say what came back, not what you sent.** Every write to shared state gets one line naming what it
became — the identifier and the words the store returned. A write reported from the request instead
of the response has verified nothing, and the person reading it cannot tell the two apart. This is
the only thing standing between a malformed write and its living on unnoticed.

**Read and write shared state through the interface that owns it.** A store's rules — versions,
claims, lifecycles, authorisation — live in the layer in front of it, not in the file behind it.
Reaching around that layer makes every invariant advisory, and the state you verified against is
then not the state anyone else gets. When the interface cannot answer something, that is a gap to
report, not a reason to go behind it.

**A write that cannot be undone goes to a person before it is made.** Where the state has no path
back — an approval, a one-way lifecycle, a field that is write-once — a report afterwards is a
notification, not a control. Say which kind it is and wait.

## The gate is what you actually checked

Green means the project's whole gate, run over the state you leave behind.

**Never fake green.** If a check is wrong, fix it deliberately and say so. Removing it to pass is
the one move that makes the graph useless. A flaky check gets one rerun and then gets quarantined —
re-running until green is the graph lying to you.

The fix loop is bounded: three attempts. A loop that repeats itself is thrashing, not converging —
take the escape, and report the failing output verbatim along with what you tried.

Leaving things broken is allowed only with a successor that repairs them — one that exists as a
hard dependency and carries the repair in its own criteria. Otherwise broken is just broken. A chain
that deliberately passes through an inconsistent middle needs that middle named in advance.

**A change whose blast radius fans out does not need that middle: expand, migrate, contract.** Add
the new form beside the old so nothing breaks; migrate what depends on it in batches sized by blast
radius, each batch its own unit that stays green because the old form is still there; delete the old
form last, blocked by every batch. Where even the batches cannot stand alone, they share one
integration and green is promised only at the join. Reach for the repairing successor when this
sequence genuinely does not fit, not before.

## Review

**Always in a fresh reader, including when you are confident.** The value comes from someone who
did not write the thing. Budget it: reviewing costs at least as much as doing.

One pass from a different model family, where that is available. A reviewer from the same family
shares the author's blind spots, and a review that agrees too easily adds no information. Pin the
model rather than resolving a nickname, so it cannot drift.

Review before acceptance. Afterwards is an audit: it finds the same defects a day later and a merge
deeper.

Record a skipped or degraded pass as skipped or degraded — a weaker model than intended, a failed
independent pass. A missing measurement is not a clean one.

Every finding gets one of three dispositions, all recorded: fixed now, filed as separate work, or
rejected with one sentence of reasoning. Carry the dispositions into the next pass so something
already filed or rejected is not re-flagged and does not eat the budget.

Budget five, with two escapes, both the same move — stop and put the open findings to a person: the
budget is spent, or the same finding recurs after a fix that claimed to resolve it. The second is
thrashing, and the way out is a person, never a silent ship.

**The plan is reviewed the same way, before a person ever sees it.** Nothing automatic judges a
plan, approval is usually a door that does not open backwards, and criteria written into it may be
unamendable once accepted. Past the threshold that earns a graph, the plan goes to a fresh reader,
findings are worked in, the revised plan goes back, and that repeats under the same budget of five
and the same two escapes. What is put to the person is a plan that has already survived this, or a
plan plus the findings that outlasted the budget — never one that was never read.

**Zero findings is an outcome, not a target reached by attrition.** A reader asked for problems will
produce them, and by the fourth pass it is inventing. Carry each pass's dispositions forward so
nothing already fixed or rejected returns, and treat a pass that only restates settled ground as the
loop having ended rather than as a reason to run another.

**Show the review history with the plan, not a laundered plan.** How many passes it took, what was
found, and what was rejected rather than fixed is the best signal available about how sound the
thing is. A plan clean on the first pass and one that took four are not the same plan, and hiding
the difference buys a tidier surface with the reader's ability to judge.

## Planning

**The model tells you what exists, not what you can reach.** Before planning around a field or
mechanism, confirm it can be operated from where you stand. Something correctly modelled, dutifully
enforced and quietly unreachable blocks you only once the plan is approved.

Anything meant to run in parallel must not touch the same thing. Checkable at plan time, and usually
what decides whether you have a plan or an ordered list.

**Cut the work into slices, not layers.** A slice is narrow but complete: it goes all the way
through, and it can be verified on its own. A layer cannot — it is only ever half of something, so
"done" for a layer means the whole is still broken, and any gate you write for it measures the piece
instead of the state you leave behind. When a split feels like persistence, then interface, then
wiring, it is layers.

**Criteria come from what the objective requires, and each one has to be able to fail.** What makes
a slice verifiable is having something written down that an outcome could contradict. Coordination
state usually asks whether anything is outstanding, and the empty set satisfies that — a unit with
no criteria passes every such check and has been recorded as done without ever being checked. State
what would have to be observed for the objective to be closer, not what the work will consist of.

A change to a shared definition is also a change to everything already recorded under it, and to
whatever already runs on it. Migration and deployment are part of the work; a plan that ends at
"built" has not finished.

An estimate is a signal, not a ceiling. Work that breaches the size or effort you predicted has
either found something or lost its way, and only looking tells you which. Loop budgets are the
opposite: when one is spent, take the escape.

## Putting a decision to a human

Show what gets bound, what it costs, what it rests on, what stays open, how success will be judged —
and the strongest reasons against. Without the last one it is a form, not a decision.

**Only offer a counterargument whose stake the reader can weigh.** One needing knowledge they do not
have makes the surface look rigorous and leaves it unusable. If the objection is real but they cannot
judge it, say who can and what finding out would cost.

Name the work by its title, not its identifier. A wall of keys and numbers is unreadable to the
person who has to decide; the identifier rides inside the name for anyone who needs to look it up.
This holds for everything a person reads, not just the decision surface.

## Briefing someone who was not here

**Mark your claims as claims.** A brief stating as fact something you have not verified will be
believed, and the mistake returns wearing the authority of your instructions. Say which parts you
checked and which you assumed, and ask to be corrected on the difference.

## The trail

Past the same threshold that earns a graph, the graph is written down — twice, not once.

**At plan time**, once the drawn graph is accepted and before work starts: nodes with type, edges
with condition and budget. This is the frozen design; what changed goes into the delivery
annotation, not into it.

**At delivery**, before the work is called done: annotate the frozen graph with what actually
happened — what each node consumed against its budget, which edges were taken, what each gate said.
Then a **Feedback** section: where design and reality diverged. A budget that was far off, a node
that should have existed and did not, a step that should have fanned out or should not have, a
contract renegotiated mid-flight. Write it even when nothing surfaced; "nothing to report" is a
claim and silence is not.

The Feedback sections across many runs are the raw material for revising this document. Without
them it cannot correct itself, and a rule that cannot be corrected becomes folklore.
