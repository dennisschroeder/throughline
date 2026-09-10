# Model-gap repairs, iteration 1

Frozen execution graph for `OBJ-MODEL-GAP-REPAIRS`. Accepted by Dennis on 2026-09-07 before
implementation. Throughline plan `01a07794-2d52-736e-8ccb-d3ae762f56cd`, revision 1.

```text
REP-01 PERMS -> REP-02 EFFECTS -> REP-03 ORIENT -> REP-04 CRITERIA
 -> REP-05 LOCATION -> REP-06 DIMS -> REP-07 ACTIVITY -> REP-08 QUESTIONS
 -> REP-09 REVIEW -> REP-10 REASON -> REP-11 CAPCLI -> delivery

Each implementation node I:
  I [agent] -> G [six repository commands]
  G red -> I fix -> G                         budget 3, then human
  G green -> R [fresh review] -> V [finding disposition validator]
  V unresolved -> I fix -> G -> R             budget 5, then human
  V clear -> commit -> next dependent node

Delivery:
  final gate -> PR/CI -> review comments -> safe merge -> installed-daemon smoke
  CI red -> fix -> final gate -> PR/CI         budget 3, then human
```

The nodes are sequential because their domain, service, MCP, dashboard, migration, generated-model
and test-store changes overlap. Review checks may fan out only when they touch no files. State on
every edge is the item and claim identifiers, exact commit, changed paths, command output, findings
and dispositions, and fix/review counters. A model report is hearsay until the parent reruns the
gate and reads the resulting state.

## Nodes and deterministic criteria

| Node | Scope | Gate beyond the whole repository gate |
|---|---|---|
| REP-01 PERMS | Private workspace directory, config, database and SQLite sidecars | Isolated umask-000 first-open and reopen tests observe directory 0700 and files 0600 while WAL/SHM exist. |
| REP-02 EFFECTS | Collateral mutation effects and upgrade-safe replay | Every mutation advertises actual collateral kind, id and committed version; relations gain real versions; retry writes nothing twice. |
| REP-03 ORIENT | Objective listing/key resolution and dashboard selection | An objective with no items is listable and selectable; browser refreshes it after cursor change. |
| REP-04 CRITERIA | Immutable criterion supersession and additions | Replacement, rationale and predecessor survive reopen; only active criteria block and count. |
| REP-05 LOCATION | Client-path workspace resolution and relative artifacts | Nearest canonical ancestor wins without enumeration; relative/absolute equivalents deduplicate inside the root. |
| REP-06 DIMS | Objective priority/appetite, item measure, context kinds/lifecycles | Values survive unrelated patches/reopen; every kind maps mechanically to its declared lifecycle. |
| REP-07 ACTIVITY | Objective binding and historical backfill | Objective-scoped feed includes planning records; cursor order and append-only trigger survive populated migration. |
| REP-08 QUESTIONS | Unsharp/multi-item blockers and precise attention | Every linked item blocks while unresolved; dashboard names question blockers and preserves attention state. |
| REP-09 REVIEW | Work-item validation and declared review gate | Missing, wrong or stale review evidence blocks done; matching evidence passes through app and dashboard. |
| REP-10 REASON | Objective transition rationale | Reason, actor and edge survive restart/read; failed transitions add no history and no automatic phase change is added. |
| REP-11 CAPCLI | Human-only grant CLI, schema check, final model contract | Non-human granters fail; incompatible CLI never migrates; claim error gives exact remediation; final compatibility matrix passes. |

Every node runs the six commands in `AGENTS.md`, in order:

```sh
test -z "$(gofmt -l cmd internal)"
go generate ./internal/semanticmodel && git diff --exit-code -- internal/semanticmodel/model.generated.json
go vet ./...
go test ./...
go build ./...
CGO_ENABLED=0 go build ./...
```

## Model assignment

Bounded implementation uses `gpt-5.6-luna` with high reasoning behind the deterministic gate.
Fresh judgment/review uses `gpt-6-astra`. A different provider is unavailable; cross-provider
review is recorded as degraded rather than clean.

## Plan review history

Claude review passes 1-3 were same-family and therefore degraded; their findings were dispositioned
into the slices. Pass 4 was interrupted in Claude and completed by a fresh Codex dashboard reader;
it added criterion rendering, objective-list refresh, multi-item question blockers and stale drawer
criteria. Pass 5 was interrupted by quota after a partial read and is recorded as degraded, not
zero findings. The owner accepted the remaining decisions: capability CLI checks schema but only
the updated daemon migrates; every collateral relationship receives a real version.

The strongest cost is REP-02: literal versioned effects widen migrations and adapter contracts.
The owner accepted that cost instead of narrowing the previously accepted effects promise.

## Delivery annotation

Not delivered. Append the consumed budgets, exact edge conditions, command results, commits, review
dispositions, PR/CI state and installed-daemon smoke as execution proceeds. Do not rewrite the frozen
graph above.

### REP-01 PERMS

- Commit: `85a7ad6` (`fix: protect workspace database files`).
- Claims: initial `01a07af7-44c2-76f9-9521-cf4290e8f085`; renewed after expiry as
  `01a08070-25c7-7e34-ae90-5d6c05a32ccb`.
- Final gate: all six repository commands exited zero on 2026-09-08; generation left
  `model.generated.json` unchanged.
- Review: five fresh `gpt-6-astra` passes. Four findings were reproduced and fixed: closing an
  independent descriptor released SQLite's process locks; arbitrary database parents were chmodded;
  symlinked databases targeted the wrong sidecars; and disappearing sidecars caused a Stat/Chmod
  race. The final pass reran 2,000 concurrent close/open iterations and reported clean.
- Evidence: umask-000 first creation, reopen repair, WAL/SHM recreation, data preservation, symlink
  targets, unchanged existing parent modes, process-lock preservation and serialized file
  preparation all pass.

### REP-02 EFFECTS

- Commits: `c6e32e2` (`feat: return versioned collateral effects from every mutation`),
  `4242ea0` (`test: gate the effects contract instead of asserting it in prose`),
  `f08ba39` (`refactor: make a tool response impossible to build without validating it`),
  `89292b6` (`test: pin the collected tables and refuse a response built outside the validator`).
- Claims: `01a08522-cf3c-79cf-b22f-ecb6140ae622`, then `01a08629-db44-703e-bdb9-72a4bb3cecb2`
  after the first lapsed during an interruption. Both had to be re-established by returning the
  item to `ready` first: an expired but unreleased claim on an `in_progress` item is not directly
  reclaimable.
- Inheritance: the node was executed by `gpt-5.6-luna` into an unowned working copy outside the
  worktree, with no git metadata and nothing committed. That copy was audited file by file rather
  than applied. 31 files were taken; `rep02_refactor_calls.go`, a scratch code generator, was
  rejected; 16 collateral activity records it added were dropped as REP-07 scope. Seven defects in
  it were fixed before any review ran.
- Final gate: all six repository commands exited zero on 2026-09-09 at `f08ba39`, plus
  `go test ./... -race -shuffle=on` and three consecutive clean full-suite runs.

#### Review

Five passes, **all recorded as degraded**: cross-provider review was unavailable, so every reviewer
was Claude, the same family as the author of the fixes. Pass 1 fanned out into three independent
audits on disjoint questions (collection mechanics, contract coverage across all 39 mutating tools,
the idempotency replay contract); passes 2-5 were single readers on a narrowing scope.

Passes 1 and 2 found ten defects in behaviour. Passes 3, 4 and 5 found **none** — every later
finding was a guarantee that nothing checked. Each regression test written in response was run
against the code with the thing it gates removed, and observed to fail. The loop ended because the
budget was spent, not because a pass came back empty; passes 3 to 5 were each still productive.

| Pass | Behavioural defects | Ungated guarantees | Rejected / deferred |
|---|---|---|---|
| 1 | 6 | 1 | 2 out of scope, 1 latent |
| 2 | 4 | 2 | 1 filed, 1 accepted as a decision |
| 3 | 0 | 3 | 1 recorded as a finding |
| 4 | 0 | 3 | — |
| 5 | 0 | 2 | 2 cosmetic, 1 latent |

The defects worth naming, because each was silent: read-only tools emitted a null `effects` member
the schema forbids; `get_item` reported `expected_output.version` 0; revoking an approval never
touched the action; the legacy replay whitelist named operations that write a second row, and one
whose version field is itself new in this change, so an upgraded replay would have reported version
0 rather than refusing; `ReplaceWorkItemCapabilities` deleted in Go map order, so the same removal
returned its effects in a different order on every call.

Two of those were self-inflicted by an earlier fix in the same node: expiring the action
unconditionally on revocation broke revocation for an action already executing, and the first
attempt at the legacy guard was too narrow. Both were caught, one by the author before the reviewer
reached it.

The ungated guarantees mattered more than the defects, and each was found only because a pass ran
after the behaviour was already correct:

- Deleting eight entries from `effectTables` left the whole suite green — the entire
  output-production chain could have stopped being reported with no signal at all.
- Removing the output-validation call left the suite green, so the effect version's advertised
  minimum was enforced by a validator nobody was obliged to call. Folding validation into the one
  function that builds a response fixed that; pass 5 then showed the guarantee still rested on the
  *callers* of that function, since reconstructing a response inline at either dispatch site
  bypassed it and the suite stayed green.
- The restart fixture stripped the stored effects before reopening, so it had never once read
  effects back out of a stored response.
- Twenty-three of the twenty-seven collected tables carry a bare `NEW.id`, and nothing pinned them:
  pointing `progress_entries` at its work item id, or reporting an external-action revision's
  revision number as its version, are both valid SQL against real columns and both silently wrong.
  `effectTables` is now a copy of a reviewed list rather than the only statement of it.

#### Dispositions carried across passes

| Finding | Disposition |
|---|---|
| `request_attention` accepts three target kinds its dispatch does not handle | Rejected: REP-08 narrows attention target kinds |
| No MCP tool assigns an actor capability, so `actor_capabilities` is unreachable | Rejected: REP-11 carries capability assignment |
| `capabilities` rows stay partially populated across two insert paths | Filed; pre-existing, made visible by this node |
| Expiry is terminal, and one revocation expires the action for every principal | Accepted, recorded as decision `01a08635-6d18-73a5-bb25-ab2149a30217` |
| FK cascade and set-null fire the collector triggers | Latent: nothing deletes plans or work items today |
| The effect `kind` vocabulary is a public contract `get_semantic_model` does not describe | Recorded as finding `01a0864a-c57d-7631-9cbe-d088f935aa1e` |
| `Effect` has no create/update/delete member, so a delete is indistinguishable from a write | Open question `01a08629-8df4-76e9-bb3f-7348ae75ec36` to the owner; this node ships the three-field shape the criteria name |

#### Evidence

- Bulk plan approval and profile supersession, deletes, all three approval kinds, restart replay of
  a multi-entity mutation, the legacy upgrade and the legacy refusal, and a database upgraded with
  data already in it, are covered in `internal/sqlite/effects_integration_test.go` and
  `internal/sqlite/effects_upgrade_test.go`.
- The MCP contract is gated at the wire in `internal/mcp/rep02_contract_test.go`, which derives the
  39 mutating and 11 read-only tools from a live server rather than from a duplicated constant.
- Two SQLite properties the design rests on were measured rather than assumed:
  `ALTER TABLE ADD COLUMN ... CHECK (version > 0)` is enforced by `modernc.org/sqlite`, and TEMP
  tables are transactional, so a rolled-back transaction leaves no stale effect rows.

### REP-03 ORIENT

- Commits: `939bf0e` (the read path and the dashboard), then `886f1bd`, `994ec41`, `8471726` and
  `958345c`, each a response to a review pass.
- Claims: `01a0878a-f7e3-7311-9874-dca398cfdd74`, then `01a0888d-3a94-7e82-8c30-e40584d40c6b` after
  the first lapsed. As in REP-02, both had to be re-established by returning the item to `ready`
  first. That is now four occurrences across two nodes.
- Final gate: all six repository commands exited zero on 2026-09-10 at `958345c`, plus
  `go test ./... -race -shuffle=on`.

The defect was one shape everywhere: every path that needed a list of objectives derived one from
the work items. `board_overview` counted work items per objective phase rather than counting
objectives, and omitted any objective with none — in the one call an agent is told to orient with.
The dashboard switcher and its auto-resolve could neither list nor select an objective without work,
which is exactly the objective someone has just created. No layer had an objective listing or a key
lookup at all. The dead helper removed by the first commit documented the defect in its own comment.

#### Review

Five passes, all recorded as **degraded** for the same reason as REP-02: cross-provider review was
unavailable, so every reviewer was Claude.

| Pass | Findings | Of which behaviour | Notes |
|---|---|---|---|
| 1 | 12 | 6 | Most were regressions the repair itself introduced |
| 2 | 13 | 3 | Ten mutants reverting pass 1's fixes survived the suite |
| 3 | 9 | 1 | The one defect was introduced by pass 2's own tidying |
| 4 | 9 | 0 | Judged functionally correct; two key decisions ungated |
| 5 | 3 | 0 | All three in tests; two passing on fixture order |

The defects worth naming, because each was silent. Key acceptance reached only two of twelve tools,
so `list_items`, `get_changes` and `list_outputs` answered a key-addressed call as though the
objective held no work — a confidently wrong answer rather than an error. `version_conflict` lost its
`current` block when the objective was addressed by key, so the caller who has only the key written
down got the one error that carries the version it needs, without the version. The dashboard
defaulted to a brand-new empty objective, because it is trivially the most recently updated. Two
resolvers existed with nothing pinning them together; a case-folding drift in either would have
answered the same field two different ways on two tools.

Three findings were defects introduced by the previous pass's repair. Pass 2's replacement of an
auto-resolve seed with a zero value made the function return an empty id with a nil error, which the
caller's 404 branch cannot see.

#### Dispositions

| Finding | Disposition |
|---|---|
| `buildObjectivesResponse` costs O(objectives) heavy reads per refresh, amplified by the new per-tick switcher refresh | Filed. `buildGates` re-runs `ListActivity` and `ListOutputProfiles` per objective though both are objective-independent; the fix is in `gates.go`, outside this node |
| `create_item` cannot be retried with the same idempotency key, even byte-identically | Recorded as finding `01a08857-7c9b-79e3-a32d-46397c888fca`. Predates REP-02 and REP-03 and breaks the handoff's sixth invariant |
| A workspace-wide proposed output profile is stamped onto every objective's gate count | Latent, pre-existing; newly visible because empty objectives now appear in the switcher |
| `internal/dashboard/static/index.html` has no test infrastructure, so four of one commit's claims are unverifiable | Accepted: `static.go` declares the frontend disposable. Those regions were read manually instead |

#### Evidence

- Every one of the twelve tools taking `objective_id` was mutated to drop its resolver, one at a
  time, and each mutation fails the suite. Before the coverage work, six survived.
- The reference rules and the objective choice are pure functions with tables of the rules they
  implement, each rule verified by inverting it.
- Both criteria are pinned against the old derivation: reverting `ListObjectives` to the
  items-derived form fails the store, contract and dashboard tests.

### REP-04 CRITERIA

- Commits: `f9c546a` (`feat: supersede an acceptance criterion instead of rewriting or excusing
  it`), then `fa15c8d`, `84d30ba` and `1d5da5e`, each a response to a review pass. Budget reduced
  to 3 passes for this node and every one after it (decision `01a088ef-4338-7c02-8732-e694b5c7b114`).
- Claims: `01a088ef-997f-72f5-81ce-3b65992ff3ee`.
- Final gate: all six repository commands exited zero on 2026-09-10 at `1d5da5e`, plus
  `go test ./... -race -shuffle=on`.

A wrong acceptance criterion could previously only be waived, which records that the condition was
excused rather than that it was mistaken, or resolved anyway, which falsifies what the gate actually
checked. `AcceptanceCriterion` gains a `superseded` status plus `supersedes_id` and
`supersession_reason`; the predecessor's text, ordinal, required flag and resolution evidence are
never rewritten, only its status and version change. Migration `0012` rebuilds
`acceptance_criteria` to add the columns, a partial unique index enforcing at most one active
criterion per ordinal, and a partial unique index enforcing at most one replacement per predecessor.
`patch_item` carries `acceptance_criteria_to_add[].supersedes_id`.

#### Review

Three passes, the reduced budget, all recorded as **degraded** for the same reason as REP-02 and
REP-03: cross-provider review was unavailable, so every reviewer was Claude.

| Pass | Mutants | Survived | Findings fixed |
|---|---|---|---|
| 1 | 40 | 25 | 10 |
| 2 | 27 | 5 | 5 |
| 3 | 4 | 3 | 3 (closed with pinning tests; none were live defects) |

The defects worth naming, because each was silent. The board card counted superseded criteria in
its progress fraction, so correcting a condition moved the card *away* from done and a criterion
satisfied before being superseded stopped counting as satisfied. The table rebuild in migration
`0012` dropped `acceptance_criteria_by_item`, the index both hot queries need, turning
`ListAcceptanceCriteria` and the completion gate into full table scans. An ordinal collision leaked
the driver's raw `UNIQUE constraint failed ... (2067)` rather than a domain message — first for a
plain addition (pass 1), then for a replacement pointed at a *different* active criterion's ordinal,
which the first fix's guard did not cover because it was skipped for every superseding addition
regardless of which ordinal it named (pass 2). Superseded criteria rendered identically to pending
ones on the dashboard, with neither the replacement link nor the reason reaching the drawer. A
required criterion added to a `done` item left an unmet gate unnoticed.

Pass 3 found no further defects: its three survivors were all cases where the shipped code was
already correct and simply unpinned — the `patch_item` supersession-field mapping, the dashboard
drawer's mapping (the same class of gap pass 2 found at the MCP layer, this time at the rendering
layer), a superseding replacement reopening a `done` item's gate, and two plain additions in one
batch claiming the same ordinal. Each was closed with a test verified to kill the mutant that found
it, not filed as residual risk, because the fix was cheap and directly bears on AC1's "visible" or
the driver-error class the rest of the node treats as a defect wherever it can reach a caller.

Two items were considered and deliberately left as-is: the app-layer cross-work-item guard in
`service.go`, which duplicates a domain-layer guard one line later and only changes the error
message if removed; and the `acceptance_criteria_one_replacement` partial unique index, which no
current code path can reach past the domain's already-superseded guard and the store's version CAS
— kept as schema-level defense in depth rather than as a tested guarantee.

#### Dispositions

| Finding | Disposition |
|---|---|
| The ontology has no `acceptance_criterion` lifecycle | Filed (F10, pass 1); same class as REP-02's effect-kind vocabulary gap |
| `acceptance_criteria_one_replacement` index has no code path that reaches it | Accepted as schema-level defense in depth, not a tested guarantee |

#### Evidence

- `TestSupersedingACriterionKeepsItsPredecessorIntact` supersedes a criterion, reopens the database,
  and confirms the predecessor's text/ordinal/required and the replacement's link/reason both
  survive.
- `TestOnlyActiveCriteriaBlockCompletion` and `TestCardProgressCountsOnlyActiveCriteria` pin AC2 at
  the completion gate and at the board card independently.
- Six mutants surviving pass 1 and five surviving pass 2 were re-run after their fixes and confirmed
  to fail the suite; the three surviving pass 3 were each verified the same way before being closed.
- `TestEffectsWorkOnADatabaseUpgradedWithDataPresent` extended to assert migration `0012`'s table
  rebuild preserves a pre-existing row's `required`, `text`, `ordinal` and `status`, not merely its
  count; verified to fail against a hardcoded-`required`-to-`0` rebuild.

## Feedback

- REP-01 was estimated small but consumed the full five-pass review budget because file permissions
  intersect SQLite's process-scoped POSIX lock behavior and sidecar lifecycle. The deterministic
  gates stayed green throughout; only adversarial review exposed the four defects.
- The implementation agent could write only to `/tmp`, and a later replacement agent could not
  start a shell because the configured workspace root was absent. The parent applied reviewed
  patches in the actual worktree and independently reran every gate.

### REP-02

- **The graph budgeted one implementation node and got a handover instead.** REP-02 was executed by
  another model into a directory with no git metadata that was never committed, and the parent
  inherited it as an artefact to audit rather than a result to accept. That is the second node in a
  row where the implementation agent could not write where the work belonged. The graph's node type
  assumes the implementer and the gate see the same tree; twice now they have not. Either the graph
  should name the worktree as part of a node's contract, or it should carry an explicit
  inherit-and-audit node so the audit is budgeted rather than absorbed silently.
- **The five-pass review budget was spent, and the last three passes justified themselves for a
  reason the graph does not model.** Passes 3, 4 and 5 found no wrong behaviour. Under a rule that
  stops at zero findings they would not have run, and the three largest holes in the work would
  have shipped: `effectTables` could lose eight entries silently, output validation could be deleted
  silently, and the restart fixture never tested the branch it was written for. The distinction that
  earned those passes is between *the code is wrong* and *nothing would notice if it were*. The
  graph's edge condition is "gate green", which cannot see the second kind. A node whose deliverable
  is a contract should carry an explicit mutation check — remove the mechanism, expect red — as part
  of its gate rather than as a reviewer's initiative.
- **Two defects were introduced by fixes to earlier findings in the same node.** Expiring the
  external action unconditionally on revocation, and the first version of the legacy replay guard.
  Both were caught, one before a reviewer reached it, but the budget counted them as new findings
  rather than as rework. A fix is a change and deserves the same suspicion as the code it replaces;
  the loop budget should probably distinguish findings against original work from findings against
  repairs, because the second kind is a signal that the node is churning.
- **The estimate held.** REP-02 was estimated large and was large. Unlike REP-01, the review budget
  was spent on real defects rather than on one intricate interaction: seven behavioural defects
  across two passes, spread over the adapter, the application layer, the store and the domain, which
  is what a change touching every mutation looks like.
- **Delegating the mechanical audit paid, and delegating judgment to the same model family did
  not fully.** The exhaustive tool-to-table trace across all 39 mutating tools was cheap, complete,
  and found nothing wrong — exactly the shape of work worth handing to a cheaper reader behind a
  gate. Every review pass was same-family and is recorded as degraded; the parent independently
  re-measured the two SQLite properties the design rests on rather than accept them as reported,
  and that habit is what the degraded label should mean in practice.
- **Three interface gaps surfaced while operating the tracker, not while building.** An expired but
  unreleased claim on an `in_progress` item cannot be reclaimed without first transitioning back to
  `ready`; `ask_question` bound to a work item blocks it, with no way to record a non-blocking
  question against an item; and `claim_item` does not transition to `in_progress` by default though
  the handoff says it does. All three were hit by the agent doing the work, which is the only way
  they surface. Worth a standing habit: record what the tracker made awkward, not only what the
  code needed.

### REP-03

- **The estimate was medium and the implementation was medium; the review was not.** The change
  itself took one pass to write and four to make safe. What consumed the budget was not difficulty
  but blast radius: a field that twelve tools accept cannot be half-changed, and the first version
  changed it on two of them. A node whose deliverable is "X is now addressable" should carry, in its
  own criteria, the requirement that every existing surface taking X agrees — otherwise the repair
  ships a new inconsistency in place of the old uniform defect.
- **Three of five passes found defects introduced by the previous pass's repair.** This is the second
  node in a row where that happened, and it is the clearest signal available that the loop budget
  measures the wrong thing. Counting findings, the passes went 12, 13, 9, 9, 3 and never looked like
  converging. Counting *behavioural* findings they went 6, 3, 1, 0, 0, which is exactly the shape a
  budget should stop on. The graph should distinguish findings against original work from findings
  against repairs, and should read convergence from severity rather than count.
- **Reviewers were most valuable where they mutated rather than read.** Every pass that ran mutations
  found something a reading pass had missed: ten surviving mutants in pass 2, six in pass 3, two
  fixture-order tests in pass 5 that had been written *by* the previous fix and looked correct. A
  review instruction that says "mutate the code to contradict each claim and report what survives"
  produced more than any amount of "look for problems".
- **The tests written to gate a fix are the least reviewed code in the node.** Twice a test was
  written, run against the pre-fix code, seen to fail, and was still wrong — it failed for a reason
  adjacent to the one it claimed. Watching a test go red proves it is sensitive to *something*, not
  that it is sensitive to the rule in its name.
- **Interface friction, recorded because it is only visible from inside the work.** An expired but
  unreleased claim on an `in_progress` item cannot be reclaimed without transitioning the item back
  to `ready`: four occurrences across two nodes now. `ask_question` bound to a work item blocks it,
  with no way to record a non-blocking question against one. `claim_item` does not transition to
  `in_progress` by default though the handoff says it does.

### REP-04

- **Counting mutants converged where counting findings had not.** 40, 27, 4 across the three
  passes, and every pass's survivors fell within what the next pass's fixes explained: 25 to 5 to 3.
  This is the first node in the objective where the raw mutant count itself reads as a convergence
  signal, not just the behavioural-finding count REP-02 and REP-03 needed to see it. Reducing the
  budget to 3 passes after REP-03 did not cost real findings here — pass 3 found nothing that was
  actually wrong, only three more things nobody had pinned yet.
- **The same class of gap recurred at three different layers of the same feature, each invisible to
  the others.** The `patch_item` request mapping (pass 2), the dashboard drawer's rendering mapping
  (pass 3), and the ordinal-collision guard's blind spot for an unrelated criterion's ordinal
  (pass 2) versus its blind spot for two *new* criteria sharing an ordinal within one batch
  (pass 3) are the same underlying shape twice each: one guard written for the case the author had
  in mind, silent on the adjacent case a caller could still reach. A criterion worth adding to this
  kind of node: state the surface *and* the input shape a guard is meant to cover, not only the
  surface.
- **A finding that isn't a defect is still worth a test, when it's cheap and sits on a stated
  criterion.** All three of pass 3's survivors were code already behaving correctly; none were
  filed as residual risk. AC1 says "visible" — the two rendering mappings (MCP and dashboard) are
  exactly what makes that true or false for a person, so leaving either unpinned would have left
  the criterion resting on manual reading rather than on the gate. The two structural gaps (index
  and app-layer redundant guard) that *were* left as accepted residual risk were both cases where no
  reachable code path exercises them at all, which is a different judgment from "correct but
  untested."
- **The claim held for the whole node this time.** Unlike REP-02 and REP-03, REP-04's single claim
  (`01a088ef-997f-72f5-81ce-3b65992ff3ee`) covered all three implementation commits and all three
  review passes without lapsing. Worth noting precisely because the four prior lapses across two
  nodes made it look like a certainty rather than a function of how long a node runs unattended.
