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

### REP-05 LOCATION

- Commits: `905f246` (implementation), then `ea3b15c`, `a9e72fd` and `f352683`, each a response
  to a review pass, at the 3-pass budget (decision `01a088ef-4338-7c02-8732-e694b5c7b114`).
- Claim: `01a08cde-4db0-740b-81c5-abc27b7ea1ae`, held for the whole node without lapsing — the
  first node in this objective where that happened.
- Final gate: all six repository commands exited zero on 2026-09-10 at `f352683`, plus
  `go test ./... -race -shuffle=on`.

Two independent halves, each closing one gap where something claimed to work and did not.

A daemon resolving `workspace_id` per call (OBJ-WORKSPACE-ROUTING) has no way to learn one from a
client's own working directory, since routing runs solely through an explicit identifier the daemon
cannot infer on its own. `resolve_workspace`, a new read-only, domain-neutral tool mirroring
`get_semantic_model`'s registration, takes a client-supplied path and returns the `workspace_id` of
the nearest registered ancestor: `Router.ResolveWorkspaceIDForPath` canonicalizes the path and walks
it upward, issuing one indexed `Registry.LookupByCanonicalRoot` point query per ancestor directory,
never listing what else is registered. Two implementation decisions were recorded before writing
code — `01a08cde-ad92-75a5-b259-da615f31bead` (a dedicated tool rather than a field widening every
`workspaceInput`) and `01a08cde-f37e-734f-98c2-1a96c6e48e4b` (artifact containment and dedup stay
purely lexical) — both left open by the triage decisions this node implements.

Artifact URIs gain a `workspace:` scheme naming a path relative to canonical_root, explicitly
marked rather than inferred from a bare relative string, so a typo'd absolute URI cannot silently
become a path. Containment is enforced lexically (no `..` may climb above the root) because
`internal/domain` cannot import the registry that holds `canonical_root`. Normalizing every artifact
URI's path component at construction — the one function every URI already passes through before
`ArtifactByURI`'s dedup lookup runs — fixed dedup for absolute URIs generally, not only the new
relative form.

#### Review

Three passes, the full reduced budget. Two passes each found and fixed one real defect; the third
found none.

| Pass | Mutants | Survived | Disposition |
|---|---|---|---|
| 1 | 16 | 5 | 3 real coverage gaps on already-correct code, closed with tests; 2 accepted residual |
| 2 | 6 | 0 | 0 mutants survived; 1 new defect found by direct execution, not mutation, and fixed |
| 3 | 4 mutants + 7 probes | 1 mutant, 1 probe | 1 real defect (idempotency), fixed; 1 coverage gap, closed |

The two real defects, both found on the artifact half, are worth naming precisely because neither
was a mutation survivor in the ordinary sense — both were gaps in already-shipped logic that no
test yet existed to mutate against, found by testing the shipped code's actual behavior directly
against inputs the brief asked reviewers to try.

**Pass 2:** `normalizeArtifactURI`'s workspace-scheme branch reconstructed its result from only the
cleaned path, silently discarding any host, userinfo, query string, or fragment the caller wrote.
`workspace:docs/report.md?v=2` and `workspace:docs/report.md` normalized to the same string and
collided under the dedup lookup, though the caller meant them as distinct references — a case where
the fix that was supposed to guarantee equivalence instead created a false one. Fixed by rejecting
those four components outright, the same "fail loudly rather than silently reinterpret" rule the
scheme itself exists to enforce.

**Pass 3, immediately behind that fix:** the opaque spelling of a `workspace:` URI
(`workspace:notes%3F.md`) and its hierarchical spelling (`workspace:///notes%3F.md`) reach
`net/url`'s decoding through different rules — `Opaque` is taken verbatim, `Path` is already
percent-decoded — so writing the decoded literal straight back into the canonical form meant a
real `?` or `#` in a filename reparsed as a query or fragment the next time the function saw its
own output, tripping pass 2's brand-new rejection. The same asymmetry meant the two spellings of one
file normalized to two *different* strings, defeating dedup for exactly the filenames that need
percent-encoding at all. Fixed by decoding both spellings to the same literal path before cleaning
and re-escaping segment by segment on the way out, making normalization idempotent and
spelling-independent.

The review budget stayed at three passes and the artifact half was clean neither in review 2 nor 3;
the reviewer's own framing of pass 3's task — "if you found nothing new... that is a valid result" —
turned out to be the wrong prediction twice in a row on this node's second half, while the
`resolve_workspace` half was clean from pass 2 onward. One line of work converging while an adjacent
one keeps producing real findings, inside the same node and the same budget, is itself worth
recording: a fixed pass count per *node* averages across two pieces of work that did not need the
same number of passes.

#### Dispositions

| Finding | Disposition |
|---|---|
| The ancestor walk querying the filesystem root itself before terminating has no dedicated test | Accepted: shipped code is correct (queries `current` before checking termination), coverage gap only |
| The new `workspaces_canonical_root` index has no test forcing its existence | Accepted: performance-only, no observable behavior depends on it (SQLite falls back to a table scan) |
| A `workspace:` URI carrying userinfo without a host (`workspace://user@/path`) was untested | Closed with a test case; shipped code already rejected it correctly |

#### Evidence

- `TestResolveWorkspaceIDForPathFindsTheNearestAncestor` asserts the number of
  `LookupByCanonicalRoot` calls stays bounded to the path's own ancestor depth, not to how many
  workspaces are registered — the property distinguishing an ancestor walk from enumeration.
- `TestLookupByCanonicalRootDistinguishesPendingFromNotFound` runs against a real `*Registry`, not
  a fake reimplementing its contract, after pass 1 found every fake independently carried the
  pending-vs-not-found distinction while the real SQL path carried none.
- `TestNewArtifactNormalizesAWorkspaceReferenceIdempotently` feeds a function's own output back
  into itself; `TestNewArtifactDeduplicatesWorkspaceReferencesAcrossSpellings` and
  `TestAttachArtifactDeduplicatesEquivalentAbsoluteReferences` pin AC2 at the domain constructor and
  at the actual `attach_artifact` mutation independently.

### REP-06 DIMS

- Commits: `7e931f7` (implementation), then `2dc766e`, `91420ba` and `d39894a`, each a
  response to a review pass, at the 3-pass budget.
- Claim: `01a08db2-552d-7c25-931b-0a6b2ca8c85a`.
- Final gate: all six repository commands exited zero on 2026-09-11 at `d39894a`, plus
  `go test ./... -race -shuffle=on`.

Three largely independent pieces, bundling four of the triage's decided additions in one node:
`Objective.Priority` (the same low/medium/high/urgent vocabulary `WorkItem` already uses, no ready
phase — ordering a parked idea is a priority question, not a lifecycle one); a new `Measure` type
(a value, an opaque unit never interpreted or compared across units, a basis of `estimated` or
`measured`) used as `Objective.Appetite` and as `WorkItem.Measure` beside the existing coarse
`EstimatedScope` hint; and two new `ContextKind` values, `non_goal` and `affected`, both sharing
the proposed -> accepted -> waived lifecycle `requirement`/`constraint`/`risk` already use. The
frozen graph's own gate condition for this node — "every kind maps mechanically to its declared
lifecycle" — is broader than the two stored acceptance criteria, which name only the two new
kinds; a decision recorded before writing code (`01a08db2-957a-7320-a781-6c3e06f3d6e3`) commits to
the broader reading, declaring all eight `ContextRecord` kinds' lifecycles in the ontology (a new
`kinds` field on lifecycle entries, grouped into the four shapes the code actually enforces), not
only the two this node adds.

#### Review

Three passes, the full reduced budget. Every pass found and fixed at least one real defect — the
first node in this objective where that held for the entire budget without a clean pass.

| Pass | Mutants | Survived | Real defects found and fixed |
|---|---|---|---|
| 1 | 12 | 6 | 1 (migration fails on existing data) + 4 coverage gaps |
| 2 | 10 | 4 (3 refuted) | 1 (masked test) |
| 3 | 6 | 4 | 1 (dropped appetite input) + 1 coverage gap + 1 test hardening |

**Pass 1** found the most severe defect of the node, and arguably of the objective so far: migration
0013's `context_records` rebuild — needed to widen the `kind` CHECK constraint for the two new
values — failed outright on any workspace that had ever superseded a context record before
upgrading. `supersedes_id`'s pre-existing `ON DELETE RESTRICT` is enforced the instant the original
table is dropped during a rebuild, which every prior migration touching this table had simply never
triggered (its own column was new in each earlier case). Neither `PRAGMA foreign_keys=OFF` nor
`defer_foreign_keys` alone rescues the usual drop-and-rename order once already inside the
transaction `Database.Migrate` opens — both are no-ops in that position, and RESTRICT is checked
immediately regardless of the defer pragma. The fix needed both changes together: rename the
original table out of the way before creating the replacement under the permanent name, and drop
`ON DELETE RESTRICT` to bare `REFERENCES` (inert in practice, since nothing in this codebase deletes
a `context_records` row directly). A new upgrade fixture seeds a real predecessor/replacement pair
before migrating and is verified to fail against the pre-fix SQL.

**Pass 2** found that one of pass 1's own new tests passed for the wrong reason: asserting a
version-0 insert into `context_records` failed proved nothing about `context_records`' own
`CHECK (version > 0)`, because `Database.Migrate` unconditionally installs this connection's TEMP
mutation-effects triggers, whose collector table carries an identically-worded CHECK on a
completely unrelated table — the insert failed via that trigger regardless of whether
`context_records`' own constraint existed. Fixed by asserting the CHECK's presence from the table's
own recorded schema text instead of by attempting a bad insert. Pass 2 also raised a second claim —
that pass 1's fix was more invasive than necessary, and that the pragma alone, without the reorder
and without dropping `RESTRICT`, would have been sufficient — which was checked directly against
both the real driver in isolation and this project's own fixture test, reproduced as failing both
times, and not acted on. Recorded here because a claim from a review pass is not automatically
correct merely for having been mutation-tested; this one did not survive being run.

**Pass 3** found that `create_objective`'s own `appetite` field was decoded from the wire, advertised
in the tool's schema, and silently discarded: `CreateObjectiveCommand` had no `Appetite` field at
all, so any appetite given at creation succeeded with no error and was never stored, reachable only
afterward through `patch_objective`. It also found that `patch_objective`'s priority-application
path — at both the service layer and the MCP wire mapping — had no test proving a patched priority
takes effect rather than being silently ignored (the shipped code was correct; nothing would have
caught a regression), and that the new semantic-model lifecycle test's search loop relied on a
shared helper's vacuous-truth-on-empty behavior in a way that could have picked the wrong lifecycle
entry rather than reporting no match, had a future entry ever come back kindless. All three closed.

#### Dispositions

| Finding | Disposition |
|---|---|
| `semanticmodel.Lifecycle.Kinds`'s `omitempty` json tag has no test forcing its presence | Accepted: informational, neither acceptance criterion depends on it |

#### Evidence

- `TestObjectivePriorityAndAppetiteSurvivePatchAndReopen` and `TestWorkItemMeasureSurvivesPatchAndReopen`
  each patch one field, confirm an unrelated patch does not disturb it, then close and reopen the
  database.
- `TestMigration0013AppliesToAWorkspaceWithAnExistingContextSupersession` seeds a real
  predecessor/replacement pair under the pre-migration schema and migrates over it; verified to
  fail against the pre-fix SQL.
- `TestMigration0013PreservesTheOriginalContextRecordsConstraintsAndIndex` asserts the rebuilt
  table's index and version CHECK directly against schema state, not through a path a same-connection
  side effect could mask.
- `TestCreateObjectivePersistsAppetite` and `TestPatchObjectiveAppliesPriority` each verified to fail
  against the code before its respective fix.

### REP-07 ACTIVITY

- Commits: `846caba` (implementation), then `777bbce` and `54f8d47`, each a response to a review
  pass, at the 3-pass budget.
- Claim: `01a08fa6-ea15-715f-b6e6-3df56c62ca4d`.
- Final gate: all six repository commands exited zero on 2026-09-11 at `54f8d47`, plus
  `go test ./... -race -shuffle=on`.

Implements triage decision `01a0720b-3d03` as decided: the binding, not a widened query. Activity
gains `objective_id`; the service sets it explicitly for objective-scoped records (objective, plan,
question, decision, context record, approval, `request_attention` on questions and decisions) and
resolves it from the work item otherwise. `NewActivity` rejects an objective-scoped kind or a
work-item event without it, so a missing binding fails at the write instead of vanishing from the
feed. Migration 0014 adds the column in place (no rebuild, so every sequence and
`sqlite_sequence` stay untouched), lifts the append-only update trigger only for a `CASE`
backfill keyed on the entity, recreates it unchanged, and indexes `(objective_id, sequence)`.
`get_changes` filters on the column. `get_objective_context`'s recent changes had the same
omission — it selected only the objective's own events and selected items' events — and was
repaired in the same node, since the decision's rationale names every read path that presents
questions, decisions and context as belonging to the objective; stated here because neither
acceptance criterion names that read path.

#### Review

Three passes, all degraded (same model family; no cross-provider reviewer available).

| Pass | Mutants | Survived | Real defects found and fixed |
|---|---|---|---|
| 1 | 7 | 4 | 0 behavioural; 4 coverage gaps + 1 vacuous check |
| 2 | 18 | 3 (+1 near-equivalent) | 0 behavioural; 3 coverage gaps + doc inaccuracies |
| 3 | 6 | 0 | none |

No pass found a behavioural defect in the shipped code. Pass 1's survivors were the recent-changes
selection (both directions), the plan-approval binding at write time, and a column-restricted
`UPDATE OF` trigger that would still reject the summary update the test tried. It also showed the
high-water-mark check was vacuous: AUTOINCREMENT always issues past the highest surviving row, so
"a new row's sequence exceeds the old maximum" cannot fail. The replacement compares
`main.sqlite_sequence` before and after — qualified, because the effects collector's TEMP table has
its own AUTOINCREMENT and an unqualified `sqlite_sequence` silently resolves to the TEMP one after
`Migrate`. Pass 2 found that the backfill's branch order was load-bearing and unpinned: execution
approvals are stored without an objective, so a pre-upgrade `approval` row with a work item must
take the work-item branch first. Pass 3 walked every one of the 57 activity literals and every
creation path and found nothing new.

#### Dispositions

| Finding | Disposition |
|---|---|
| `request_attention` on `review`/`clarification`/`intervention` writes unbound rows (free-form target ids) | Deferred to REP-08, which narrows those target kinds; documented in the `get_changes` contract |
| Output-revision approvals appear as objective-level history in recent changes | Rejected: they are bound to the objective, so the presentation is truthful |
| Recent changes share one `limit*2` window between objective-level and item history | Rejected: pre-existing cap, unchanged in shape |
| Dashboard reads the oldest 500 objective activity rows as "recent" (`gates.go`, `loop.go`) | Filed as separate work; pre-existing, reached sooner now that the objective feed is complete |
| `TrimSpace(ObjectiveID)` in `NewActivity` survives removal | Rejected: near-equivalent, every caller passes a stored id |

#### Evidence

- `TestObjectiveFeedIncludesPlanningRecords`: eight events of one objective, in cursor order, none
  of another objective's; recent changes include the decision. Fails against the pre-fix filter.
- `TestMigration0014BackfillsActivityObjectiveOnAPopulatedWorkspace`: pre-upgrade history of every
  entity kind the app writes, including an execution approval; asserts each row's binding, unchanged
  sequences and `main.sqlite_sequence`, identical trigger SQL, the index, and rejection of update,
  rebind and delete afterwards.
- `TestObjectiveContextRecentChangesKeepObjectiveLevelAndSelectedItemHistory` and
  `TestObjectiveChangesIncludeDiscoveryRecords` (MCP wire, objective addressed by key).

### REP-08 QUESTIONS

- Commits: `f130797` (implementation), then `16dea6b`, `eb50bcb` and `329092e`, each a response to
  a review pass, at the 3-pass budget.
- Claim: `01a09204-c75f-7ec1-9765-16bfc257af92`. Decision: `01a09205-07bd-7c77-b71b-1336d416a7af`.
- Final gate: all six repository commands exited zero on 2026-09-11 at `329092e`, plus
  `go test ./... -race -shuffle=on`.

Implements triage decisions `01a07241-be30` (unsharp state and per-link blocking) and
`01a07251-e48f` (attention targets narrowed, questions storing their attention state). Question
gains `unsharp` before `open`; `sharpen_question` records the graduation with the previous wording
in its event. Blocking derives from one `question_blocks` link table: `ask_question` takes
`blocks_item_ids`, `link_question_blocker` adds links to an unresolved question, links are never
removed, and an unresolved question holds every linked item for claiming, readiness and every
transition that runs the claim gate. `request_attention` accepts only `work_item` and `question`;
a question stores the exact state. `board_overview` gains `questions_needing_human_attention`
beside the unchanged `needs_human_attention`. The dashboard names question blockers on cards and
in the drawer apart from dependencies, shows the stored attention state, and offers only Waive on
an unsharp question.

One premise of the triage decision did not hold: questions were not "consulted nowhere in
coordination". An open question asked on a work item already blocked that item through
`HasOpenBlocker` and the ready query. Decision `01a09205-07bd` keeps that behaviour as a link
recorded at creation (and backfilled by migration 0015) rather than dropping it on the strength of
the premise; "never automatic" stays true for every item the question does not name. The same
reading found that `request_attention` on a question had never persisted anything, because the
update statement did not write the column; migration 0015 recovers the requested state from the
last `attention.requested` activity.

#### Review

Three passes, all degraded (same model family; no cross-provider reviewer available).

| Pass | Mutants | Survived | Real defects found and fixed |
|---|---|---|---|
| 1 | 18 | 7 | 1 (`get_objective_context` hid unsharp questions) + 1 (links to finished items) |
| 2 | 13 | 3 | 2 (replay of stored pre-REP-08 questions; links to rejected/superseded items) |
| 3 | 12 | 2 | 1 (replayed attention response contradicted itself) |

Pass 1's defect was the one this node exists to prevent: the resume surface filtered on `open`
only, so a session resuming while an unsharp question held work saw no questions at all. Pass 2
and pass 3 both found upgrade-path defects in idempotent replay — responses stored before the
change decode through the new types, and the first fix (map the old boolean) was right for
`ask_question` and wrong for `request_attention`, whose response carries the real state at its top
level. Each fix was followed by a test that decodes the old stored shape.

#### Dispositions

| Finding | Disposition |
|---|---|
| A retry of a command first sent to the old binary is refused, because the request hash covers the whole command struct and the struct changed (`ask_question` here; `create_objective` in REP-06 alike) | Filed as separate work: a general hash-compatibility rule, not an `ask_question` patch |
| The snake-case wire converter renders `EvidenceIDs` as `evidence_i_ds` | Noted, pre-existing; this node renamed its own field to `BlocksWorkItems` to avoid the same shape |
| `status` and `attention_state` carry no enum in the `ask_question` input schema | Accepted: the server validates both |
| A question asked on an item shows that card as "gated" rather than `blocked_question` | Accepted: gate precedence is deliberate; other linked items show `blocked_question` |
| Pre-REP-08 `request_attention` replays of a `decision` target lose their decision pointer | Accepted: the target kind no longer exists and the result had no state to carry |

#### Evidence

- `TestUnresolvedQuestionsBlockEveryLinkedItemUntilResolved`: unsharp and open questions hold items
  linked at creation, by being asked on the item, and later; claims are refused with only the
  no-blockers requirement; answering and waiving each release every held item; links to other
  objectives' and to done or rejected items are refused.
- `TestMigration0015CarriesQuestionsAndTheirExistingBlocksForward`: pre-upgrade questions keep
  status, text and version, gain their self-links, and recover attention from the latest request.
- `TestDashboardShowsAnUnsharpQuestionHoldingAnItem` (loop and drawer handlers),
  `TestCardNamesAQuestionBlockerApartFromDependencies`,
  `TestQuestionGatePreservesTheStoredAttentionState`,
  `TestQuestionBlockingAndAttentionOverTheWire` (MCP), and the two replay-decoding tests.

### REP-09 REVIEW

- Commits: `e491ecb` (implementation), then `eaf0173`, `34f8ffa` and `e29a914`, each a response to a
  review pass, at the 3-pass budget.
- Claim: `01a0a5f3-7ac6-73ff-9a75-5f4b39bd827c`. Decision: `01a0a5f4-81dc-7635-865d-36d5d90df3fa`.
- Final gate: all six repository commands exited zero on 2026-09-15 at `e29a914`, plus
  `go test ./... -race -shuffle=on`.

Implements triage decision `01a07226-8f46`: Throughline records that a review happened, not how.
`ReviewRequirementsSatisfied` was a literal `true`; it is now derived. A work item declares
`review_requirements` (criterion reference and validator kind) on `create_item`, `propose_plan`
and `patch_item`; `record_validation` accepts `work_item_id` beside `output_revision_id`, and any
record may be `degraded`. For each requirement the gate reads the latest matching work-item
validation: passed or waived satisfies it unless work was recorded on the item afterwards. Migration
0016 rebuilds `output_validations` with exactly one subject. `get_item` and the dashboard drawer
report each requirement as satisfied, missing, failed or stale, with the deciding record and its
degraded flag, and a card in review names the review done waits on.

The plan and the triage decision left "stale" undefined, which decides how strict the gate is in
daily use; it went to Dennis as the one product question of this node. He chose staleness on later
recorded work (progress, artifacts, output revisions, criterion and output-contract changes, a
return to in_progress) over staleness only on rework or on any item mutation. Claims, the move to
review or done and criterion resolutions deliberately do not stale a review, so the ordinary flow
of review, resolve criteria, done keeps working.

The node also repaired a REP-06 leftover the column refactor exposed: the ready-work join listed its
own columns and had never read objective priority and appetite or item measure, so
`list_ready_items` reported them unset. It now shares the column lists with the plain selects.

#### Review

Three passes, all degraded (same model family; no cross-provider reviewer available).

| Pass | Mutants | Survived | Real defects found and fixed |
|---|---|---|---|
| 1 | 14 | 6 | 1 (`patch_item` expected outputs did not stale a review) |
| 2 | 13 | 5 | 2 (dropping an unsatisfied requirement, or declaring one on a done item, raised no attention) |
| 3 | 13 | 6 | 0 |

Pass 1's defect was a second path to the same state change writing a different record:
`define_expected_output` wrote `expected_output.defined`, `patch_item` creating the same kind of
row did not, and staleness keyed on the record. Pass 2's two defects were the review-requirement
analogue of a rule acceptance criteria already had (waiving or adding a required criterion flags
the item); the rule existed, the new field had not been held against it. Pass 3 found only test
gaps, the most important being that nothing asserted the ordinary flow, a review recorded in
progress followed by the move to review, still reaches done.

#### Dispositions

| Finding | Disposition |
|---|---|
| Adding `Degraded` and `ReviewRequirements` changes command hashes, so a retry spanning the upgrade is refused | Covered by the separately filed hash-compatibility work from REP-08 |
| Any verifier string, including an unregistered actor or a non-human `waived`, satisfies a `human_review` requirement | Rejected for this node: output validations share the gap, and which reader counts is workflow policy by the triage decision |
| Removing `acceptance_criterion.superseded` from the staling events survives | Rejected: equivalent, every supersession also records `acceptance_criterion.added` |
| Declaring a requirement on a done item flags it even when a matching pass already exists | Accepted as specified: the declaration changes what that completion claimed |

#### Evidence

- `TestDeclaredReviewRequirementsGateDone`: missing, wrong criterion, wrong kind, failed, a later
  failure after a pass, degraded pass, stale after progress and after a return to in_progress and
  after an added criterion, and resolution and claim renewal not staling; done only at the end.
- `TestEveryKindOfRecordedWorkStalesAReview` (every staling step, plus a review recorded straight
  after one not being stale), `TestClaimingIntoInProgressStalesAnEarlierReview`,
  `TestWorkOnAnotherItemDoesNotStaleAReview`, `TestReviewBeforeMovingToReviewStillAllowsDone`.
- `TestMigration0016KeepsOutputValidationsAndTheirProtection`, `TestReadyWorkCarriesEveryObjectiveAndItemField`,
  `TestReviewRequirementsOverTheWire` (MCP), `TestDashboardExposesReviewEvidence` and
  `TestCardInReviewNamesTheUnsatisfiedReview`.

### REP-10 REASON

- Commits: `22bd77b` (implementation), then `fdc9336` and `3fd2a79`, each a response to a review
  pass, at the 3-pass budget.
- Claim: `01a0a646-22b0-7910-8635-8d5b81a555e1`. Decision: `01a0a646-5e4e-7b62-9242-c6b7ddfb2d9b`.
- Final gate: all six repository commands exited zero on 2026-09-15 at `3fd2a79`, plus
  `go test ./... -race -shuffle=on`.

Implements triage decision `01a0732f`. `transition_objective` required a reason and then discarded
it. The objective now carries `last_phase_transition` (from, to, reason, actor, time of its most
recent transition), stored on the row by migration 0017 and returned by every read that returns
the objective; each `objective.phase_changed` activity carries the from/to/reason payload
work-item status changes already write, so the full history reads through the objective-filtered
change feed REP-07 built. No history table was added. Objectives transitioned earlier keep a null
transition rather than an edge recovered without its reason. The node looked at work-item
transitions as the decision required and found no widening needed: `transition_item` has always
kept its reason in the payload, and REP-09 gave `claim_item` the same. Criterion 2 (no automatic
advance, no objective lease) is pinned by tests that recording work leaves an objective's phase
alone and that no tool claims or advances one.

#### Review

Three passes, all degraded (same model family; no cross-provider reviewer available).

| Pass | Mutants | Survived | Real defects found and fixed |
|---|---|---|---|
| 1 | 7 | 3 | 0; two doc inaccuracies |
| 2 | 4 | 3 | 1 (the phase obligation missing from the server instructions) |
| 3 | 10 | 1 (equivalent) | 0 |

Pass 2's finding was not in the code this node wrote but in what decision `01a0732e` promised in
exchange for dropping the automatic advance: the obligation to move the phase would be stated in
the server instructions. It never was, so with the advance gone nothing told a session to move an
objective out of idea. The instructions now say so and a test requires the sentence.

#### Dispositions

| Finding | Disposition |
|---|---|
| The dashboard shows the phase without its reason | Accepted: not among the decision's read paths; a UI follow-up if wanted |
| An objective created directly in a later phase has a null transition | Accepted: `create_objective` takes no reason, and `objective.created` records the creation |
| Gating the scan on `phase_transition_from` instead of `_at` survives | Rejected: equivalent, the five columns are always written together |

#### Evidence

- `TestObjectivePhaseTransitionReasonSurvivesRestart`: two transitions, a refused one and an
  unrelated patch under an advancing clock, then a reopen; the latest transition keeps its edge,
  trimmed reason, actor and time, and the feed holds exactly the two accepted transitions.
- `TestRecordingWorkNeverAdvancesAnObjective`, `TestMigration0017LeavesEarlierObjectivesWithoutATransition`,
  `TestObjectiveTransitionReasonOverTheWire` (transition response, `list_objectives`,
  `get_objective_context`, UTC timestamp, the pinned objective tool list),
  `TestReadyWorkCarriesEveryObjectiveAndItemField` (`get_item`, `list_ready_items`), and the
  instructions assertion in `TestSemanticModelInitializationAndReadContract`.

### REP-11 CAPCLI

- Commits: `f58477b` (implementation), then `8426569`, `516f6e3` and `11b66f7`, each a response to a
  review pass, at the 3-pass budget.
- Claim: `01a0a67e-490c-7914-94e9-d2858264840e`. Decision: `01a0a685-531b-78e5-831d-869ff8f60fbb`.
- Final gate: all six repository commands exited zero on 2026-09-15 at `11b66f7`, plus
  `go test ./... -race -shuffle=on`.

Implements triage decision `01a07208-694c` and the plan owner's refinement that the CLI checks the
schema but only the updated daemon migrates. `throughline capability grant` assigns a capability
with a registered human as granter; the service refuses agent and service granters, so no adapter
can let an agent grant itself what a claim requires, and no MCP tool exists for it. A claim refused
for a missing capability lists each one with a shell-quoted grant command. The command opens the
workspace database directly (ADR 0025 amended), never migrates it, refuses a missing file, retries
while the SQLite result code says another writer holds the lock, and reports schema mismatches
with a remediation that is actually true. `throughline doctor` shows the schema state read-only.
The semantic model moves to 1.2.0 with 30 source mappings (migrations 0010-0017, review evidence,
mutation effects, the grant command in its own file); the migration upgrade matrix asserts every
earlier prefix is refused by the schema check and passes once migrated; install.md describes the
upgrade and restart path.

#### Review

Three passes, all degraded (same model family; no cross-provider reviewer available).

| Pass | Mutants | Survived | Real defects found and fixed |
|---|---|---|---|
| 1 | 11 | 5 | 3 (remediation that did not work, empty database created, immediate SQLITE_BUSY) + docs |
| 2 | 7 | 6 | 2 (busy matched on error text; doctor could not surface schema drift) + AGENTS.md |
| 3 | 9 | 4 | 2 (cancellation inside an attempt misreported and its test flaky; doctor reset file permissions) |

Pass 1's most important finding contradicted the plan's own wording: "restart the daemon" does not
migrate a workspace, the first request after the restart does, so the remediation the criterion
asked for would have sent people into a loop. Pass 3 found that the cancellation test written in
pass 2 failed roughly six runs in ten on unchanged code, which a single green suite run had hidden.

#### Dispositions

| Finding | Disposition |
|---|---|
| `--as` is not authenticated; `register_actor` over MCP can register a `human` actor | Accepted and documented (install.md, ADR 0025): the boundary is the local user who can run commands |
| `IsBusy` extended codes (`BUSY_SNAPSHOT`) untested | Accepted: modernc exposes no constructor for the error, and the code check covers the family |
| One extra retry beyond the budget survives mutation | Rejected as immaterial |

#### Evidence

- `TestCapabilityGrantRequiresARegisteredHuman` (agent, service, unregistered, human),
  `TestCapabilityRejectionNamesTheGrantCommand` (only missing capabilities, quoting).
- `TestCapabilityGrantNeverMigratesAMismatchedSchema` (older, newer, renamed history; schema left
  untouched), `TestCapabilityGrantRefusesAMissingDatabaseWithoutCreatingOne`,
  `TestCapabilityGrantWaitsOutAWriterHoldingTheLock`, `TestCapabilityGrantRetryOnlyForTheLockAndWithinItsBudget`,
  `TestDoctorReportsASchemaBehindTheBinary`, `TestDoctorSchemaLineOnAMissingOrUnmigratedDatabase`.
- `TestMigrateUpgradesEverySupportedPrefix` with the schema check before and after migrating from
  every prefix; generator tests validating all 30 source mappings; model version pinned at 1.2.0.

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

### REP-05

- **A fixed number of review passes per node averages across pieces of work that did not need the
  same number.** REP-05 was two independent halves — a routing tool and an artifact-URI format —
  sharing one budget of three. The routing half was clean from pass 2 on; the artifact half
  produced a real, previously-undetected defect in pass 2 *and* in pass 3, the second one hiding
  directly behind the first one's own fix. A node-level pass count cannot see that one half
  converged early while the other kept producing findings on the pass explicitly framed as "report
  nothing new if there's nothing new" — it was wrong to expect nothing, twice.
- **A fix that closes one gap can open an adjacent one in the same function, and only a further
  pass finds it.** Pass 2's fix — reject a host, query, or fragment on a `workspace:` URI rather
  than silently dropping it — was itself correct and is not being second-guessed. What it exposed
  was that the function's *acceptance* path and its *rejection* path disagreed about which
  characters were already decoded: the rejection path (pass 2's fix) read `RawQuery`/`Fragment`
  straight from `net/url`'s parse, while the acceptance path (the pre-existing code) wrote a
  percent-decoded literal back out unescaped. Tightening validation surfaced an inconsistency in
  normalization that had been there since the implementation commit, unreachable until the
  validation around it changed. Worth a standing question for review generally: when a pass fixes
  a validation gap, does anything downstream of the newly-validated field make an assumption the
  validation itself doesn't guarantee?
- **Direct execution against the shipped function found what mutation testing structurally
  cannot.** Both of this node's real defects were gaps in logic that *already existed* and had no
  test to mutate — mutating code that is already silently wrong just changes which wrong thing
  happens. The brief's own instruction to try concrete adversarial inputs (a query string, a
  percent-encoded filename, round-tripping a function's own output back through itself) is what
  found both, and neither would have surfaced from "delete a line and see what turns red." The
  standing habit from earlier nodes — mutate rather than read — needs a second half for exactly
  this case: for new, previously-unexercised code, also try the inputs an adversarial user would
  actually type, not only mutations of the code that would validate them.
- **The claim held for the whole node.** A second node in a row (after REP-04) where the claim
  covered every commit and every review pass without lapsing, against four lapses across REP-02
  and REP-03 combined. Not yet enough to call it fixed rather than favorable timing, but two
  clean nodes after two rough ones is worth carrying forward as a data point rather than losing.

### REP-06

- **Every one of three passes found and fixed a real defect — the first node in this objective
  where the reduced budget never had a clean pass to spare.** REP-05 alternated (defect, defect,
  none); REP-04 converged to nothing by pass 3. REP-06 went defect, defect, defect. Three passes
  was still enough — nothing found in pass 3 looked structural, all three were the kind of gap a
  fresh angle on already-reviewed code turns up — but this is the first data point suggesting the
  budget is sized close to where a genuinely three-piece node (priority, measure, two context
  kinds bundled in one item) actually needs it, not comfortably above it.
- **A review pass's own claim is data, not a verdict, even when it comes with a mutant table.**
  Pass 2 reported, with a specific mutant table, that pass 1's migration fix was more invasive
  than necessary. Reproducing the simpler alternative directly — against the real driver in
  isolation and against this project's own fixture — showed it fails both times; the claim did not
  survive being checked. Nothing here impugns mutation testing as a technique: the tool caught a
  real defect (the masked version-CHECK test) in the very same pass. It argues for one further
  step on a *surprising* result specifically — reproduce it a second, independent way before
  accepting it — which pass 3's own brief adopted explicitly, and which produced a clean signal
  (three findings, all confirmed by direct reproduction, one instance where a mutant's mechanism
  was itself worth explaining rather than taken at face value).
- **A field can be in a schema, decoded, and validated, and still never reach storage.**
  `create_objective`'s `appetite` was declared in the tool's input schema and successfully parsed
  into a Go struct on every call; the command type it was copied into simply had no field for it.
  Every check that would normally catch this — schema validation, decode, the domain layer's own
  `Validate()` — passed, because none of them see the *command construction* step in between,
  where the value was dropped without an error on either side of it. The class of bug is not new
  to this objective (REP-02's effect-kind vocabulary gap and REP-03's key-resolution gap were both
  "the value exists and something downstream silently doesn't use it"), but this is the sharpest
  instance yet: a field that round-trips cleanly through everything except the one line that was
  supposed to forward it.
- **The claim held for a third node in a row.**

### REP-07

- **The first node in this objective with no behavioural defect in any pass.** Every finding was a
  test that could not fail or a line no test reached. Two things plausibly explain it: the decision
  being implemented was diagnosed from a reproduction before any code was written, and the node's
  one real hazard (the append-only trigger colliding with a backfill) was visible from the schema
  and designed around before the first commit. Not evidence that three passes were too many — pass
  2 still found the load-bearing branch order — but the first node where pass 3 reported nothing.
- **A check can be green for a structural reason rather than because it tested anything.** The
  high-water-mark assertion could not fail under any migration, because AUTOINCREMENT guarantees
  the property it checked. Same class as REP-06's masked version-CHECK test, from the opposite
  direction: there an unrelated constraint made the test pass; here the database's own invariant
  did. Worth a standing question in review: what would have to change for this assertion to fail?
- **The TEMP effects collector shadows `main` schema objects by name a second time.** REP-06 hit its
  identically-worded CHECK constraint; REP-07 hit its `sqlite_sequence`. Any test or query that
  inspects schema state after `Migrate` should name `main.` explicitly.
- **The claim held for a fourth node in a row.**

### REP-08

- **A triage decision's premise can be wrong about the code, and the fix is to record the
  divergence, not to follow the premise.** The decision said questions were advisory; the code had
  made item-scoped questions blocking all along. Implementing the decision literally would have
  silently unblocked work. The divergence went into its own decision before any code was written,
  which is where a reviewer or the owner can overturn it.
- **Upgrade paths through idempotent replay are a review blind spot the mutation technique does not
  reach.** Two of the three passes found defects in how responses stored by the previous binary
  decode through the new types. No mutant of the new code exposes that; only asking "what does a
  record written before this change look like, and what reads it?" does. The same question
  produced the filed hash-compatibility item, which applies to every node that reshaped a command.
- **The node was large and the budget still held.** Four capabilities (a lifecycle state, a link
  table with blocking semantics, attention narrowing and storage, dashboard surfaces) shared one
  item. Every pass found at least one real defect, but each was smaller and further from the core
  than the one before: resume surface, then replay of stored questions, then replay of stored
  attention responses.
- **The worktree's HEAD was detached between REP-07 and REP-08 by something outside this session.**
  The first REP-08 commit landed on a detached HEAD whose parent was the branch tip, so a
  fast-forward repaired it without loss; `git status -sb` after every commit is what caught it.
- **The claim held for a fifth node in a row.**

### REP-09

- **The one real product question of the node was spotted before any code and asked once.** The
  plan's gate said "stale" without defining it, and each plausible definition makes the gate
  behave differently every day. Asking cost one exchange; guessing would have been baked into a
  migration and every workflow that records reviews.
- **A rule keyed on a record type is only as good as the discipline of every writer of that
  record.** Pass 1's defect was not in the staleness query but in a second code path creating the
  same kind of row without writing the same activity. The same shape appeared in REP-07 (activity
  without a binding). Worth a standing review question: when a derived rule reads a record, list
  every writer of the underlying state, not only the tool named after it.
- **New fields should be held against the rules their siblings already obey.** Both pass-2 defects
  were the acceptance-criterion attention rule not extended to review requirements. The rule was
  one screen away in the same function.
- **A refactor for this node repaired a defect from an earlier one.** Unifying the ready-work
  column lists exposed that REP-06's fields had never reached `list_ready_items`, which none of
  REP-06's three review passes found because the defect was a missing read in a query nobody
  changed.
- **The claim held for a sixth node in a row.**

### REP-10

- **A decision that removes a mechanism and moves its duty elsewhere needs the new home checked,
  not assumed.** `01a0732e` dropped the automatic advance on the explicit condition that the
  obligation be stated in the server instructions. Nothing in this plan carried that condition as
  a criterion, so it would have shipped unmet; pass 2 found it only because its brief quoted the
  decision. Worth a standing plan-review question: for every "instead" in a decision, where is the
  replacement, and which item's criteria check it?
- **Small, well-bounded nodes converge fast.** No behavioural defect in the code written for the
  node in any pass, and pass 3 found nothing. The design reused two things earlier nodes had built
  (the objective-filtered feed from REP-07, the shared column lists from REP-09) instead of adding a
  store.
- **The claim held for a seventh node in a row.**

### REP-11

- **A remediation is a claim about behaviour and needs testing like one.** The plan's criterion said
  the error should name "daemon update/restart"; implementing exactly that would have shipped advice
  that does not work, because the daemon migrates lazily. The fix came from a reviewer reading the
  router, not from the criterion.
- **One green run is not evidence against flakiness.** Pass 2's cancellation test passed the gate
  and failed about six runs in ten. Timing-sensitive tests written in a review pass should be run
  repeatedly (`-count`) before being called done; this node now does so for the retry tests.
- **The last node of an iteration inherits documentation debt from all the earlier ones.** AGENTS.md
  still described 19 mapped sources from before REP-02; nobody updated it because no earlier node
  touched the mappings. Finalizing the contract was the first moment the drift became a gate issue.
- **The claim held for an eighth node in a row, across a usage-limit interruption.**

## Delivery

Final gate on `d52b633` (2026-09-16/17), all seven commands over the whole repository, exact output
quoted rather than summarized:

```
test -z "$(gofmt -l cmd internal)"                                    → clean, no diff
go generate ./internal/semanticmodel && git diff --exit-code ...      → clean, model.generated.json unchanged
go vet ./...                                                          → clean
go test ./...                                                         → every package ok
go build ./...                                                        → clean
CGO_ENABLED=0 go build ./...                                          → clean
go test ./... -race -shuffle=on                                       → every package ok, exit 0
```

PR [dennisschroeder/throughline#7](https://github.com/dennisschroeder/throughline/pull/7),
`claude/remite-c146a7` → `main`, opened 2026-09-16. CI (`.github/workflows/ci.yml`, the same six
gates) passed on the first push. `mergeStateStatus: CLEAN` throughout.

**Independent cross-review, requested explicitly because nine of eleven nodes' own reviews were
same-family and recorded degraded.** A handoff prompt sent the branch, the frozen graph, and the
dispositions above to Codex. It reproduced three real behavioral defects, none previously flagged or
dispositioned:

1. `patch_item` refused a legitimate ordinal reuse when one addition in a batch superseded a
   criterion off an ordinal and a later addition in the *same* batch claimed it: `activeOrdinals` was
   built once from the pre-patch snapshot and never updated as earlier additions in the batch
   superseded their way off an ordinal. Fixed in `685b357` by freeing the predecessor's ordinal the
   moment a supersession is decided, not only for that addition's own collision check.
2. `workspace:` URI normalization was not idempotent for a percent-encoded leading slash: the opaque
   spelling (`workspace:%2Fdocs%2Freport.md`) decodes to the same leading slash the hierarchical
   form's already-decoded `Path` carries, but only the hierarchical branch trimmed it, so
   re-normalizing the function's own first output silently dropped the slash a second time. Fixed in
   `9db965a` by trimming the decoded opaque path's leading slash the same way.
3. `throughline doctor` was not read-only as documented: it resolved its workspace through
   `config.Find`, which loads `config.toml` through the same permission-repairing path `throughline
   init` uses, so every doctor run chmodded the workspace directory to 0700 and `config.toml` to
   0600 regardless of what they were set to before. Fixed in `1d93cf9` by adding `FindReadOnly` /
   `LoadReadOnly`, which never chmod, and pointing doctor at them.

Each fix carries a regression test verified to fail against the pre-fix code (confirmed by stashing
each fix, rerunning the specific test red, then restoring and rerunning green) before being folded
into the final gate. Full gate rerun after all three fixes, same seven commands, same result: clean.
Pushed as `685b357`, `9db965a`, `1d93cf9`; CI green again; merged by Dennis's explicit approval
(`gh pr merge 7 --merge`) as `6822f36` on 2026-09-17T14:48:38Z. No unresolved review comments at
merge time.

**Release and installed-daemon smoke test**, run against Dennis's real workspaces (`~/.throughline`
does not exist as a single directory; each of the eight registered workspaces carries its own
per-root `.throughline/`). A full backup of all eight `.throughline/` directories plus the registry
was taken first (`~/throughline-backup-20260917-165047`, 23 MB), per Dennis's confirmation.

No release existed yet for the merge commit (installed binary was `v0.5.0` at `ca89e75`, well behind
`6822f36`). Dennis chose to cut one rather than build from source locally, so `v0.6.0` was tagged at
`6822f36` and pushed, triggering `.github/workflows/release.yml`: snapshot build, packaged-daemon
smoke test, then the real GoReleaser run — green in 1m50s, publishing four platform archives and
updating the Homebrew tap.

- `brew upgrade dennisschroeder/throughline/throughline` → `0.5.0 -> 0.6.0`; `throughline version`
  confirmed `v0.6.0 (commit 6822f36, ...)` — the exact merge commit.
- `throughline daemon restart` → daemon reachable at `v0.6.0`. `throughline doctor` on this
  session's own workspace immediately after restart: `schema: workspace database schema does not
  match this throughline binary: database is at migration 10, this binary carries 17` — confirmed
  the daemon does not migrate at restart. One `throughline ready --actor human:dennis` call (opens
  the workspace) later, `throughline doctor` read `schema: current`. Confirms the lazy-migration
  claim in `docs/install.md` exactly.
- `board_overview` with `include_attention: true` returned `questions_needing_human_attention`
  populated with two real open questions (from OBJ-MODEL-GAP-TRIAGE and this objective itself),
  full text and state — after one transient client-side schema-validation error on the first call
  that cleared on retry (this session's MCP connection predated the daemon restart).
- `get_changes` for this objective returned planning records (`decision.recorded`,
  `context_record.recorded`, `objective.phase_changed`) in the objective-filtered feed, confirming
  REP-07.
- `record_validation` with `work_item_id` **initially could not be verified in this session**: the
  connected MCP client's cached input schema for this one tool lacked `work_item_id` even after an
  explicit reload, while `get_item` and `board_overview` returned current data over the same
  connection — a stale client-side schema for this one tool, not a server defect. Confirmed the
  following day (2026-09-18), same session, after the MCP connection had cycled on its own (new
  tools absent the day before, e.g. `resolve_workspace`, `list_objectives`, appeared in the deferred
  list): `record_validation` with `work_item_id: 01a07794-2d52-73b9-a5bc-d8d3044d36a4` and
  `validator_kind: human_review` created `ValidationRecord 01a0b50d-272d-79ee-9983-4862e3c16d36`
  against the real REP-09 work item. (First retry used `validator_kind: manual`, correctly refused
  server-side with `unsupported validator kind "manual"` — confirming the request now reaches the
  server's own validation rather than failing client-side — before finding `human_review` in
  `internal/domain/output/revisions.go`'s `ValidatorKind.supported()` list.)
- `throughline capability grant`: refusal for a non-human granter confirmed
  (`agent:claude-code` → `capability smoke_test can only be granted by a registered human actor;
  agent:claude-code has kind agent`); success with `--as human:dennis` confirmed (`granted
  capability smoke_test to agent:claude-code as human:dennis`). `throughline doctor` after both
  calls still read `schema: current`, confirming the grant command never migrates.

Objective transitioned `execution` → `evaluation` → `completed` (the model has no direct
`execution → completed` edge; confirmed against the live `v1.2.0` semantic model's
`objective_phase` lifecycle before transitioning). Both transitions carry Dennis as actor and a
reason naming what was checked.

### Delivery

- **An independent, cross-family review pass at the delivery gate found real defects nine same-family
  review loops had not.** All three of Codex's findings — the ordinal-supersession batch bug, the
  URI leading-slash idempotency gap, and doctor's non-read-only config path — survived eleven work
  items' worth of same-family review because every one of those passes shared the author's blind
  spot on exactly the kind of input a fresh model tries first (a batch mixing supersession and
  reuse, a round-tripped opaque URI, a permission check under a read-only contract). The frozen
  graph named cross-provider review as unavailable throughout implementation and recorded every pass
  degraded for it; it should have named an independent delivery-gate pass as the node that recovers
  that cost, rather than leaving it to be requested ad hoc after the PR was already open.
- **"The gate is green" and "the branch is correct" are different claims, and the graph's delivery
  edge only checked the first.** The frozen delivery edge is `final gate → PR/CI → review comments →
  safe merge`; nothing in it names a review pass distinct from whatever comments happen to arrive on
  the PR. All three Codex findings would have merged silently without one, since none was caught by
  CI (which runs the same six commands the implementation nodes already ran) or would plausibly have
  surfaced as an unprompted PR comment.
- **The lazy-migration claim needed operating, not just reading, to trust.** `docs/install.md` and
  REP-11's remediation text both say a restart alone does not migrate; the pre-migration `doctor`
  output confirming that (`database is at migration 10, this binary carries 17`) after a real
  restart against a real seven-week-old workspace was materially more convincing than the same claim
  read off a doc, and cost one extra CLI call to obtain.
- **A stale MCP client schema is indistinguishable from a server defect until you check which side
  changed.** `board_overview`'s output-schema failure resolved itself on retry; `record_validation`'s
  input-schema gap (missing `work_item_id`) did not, even after an explicit reload. Chasing the first
  one costs nothing; assuming the second one is a shipped bug without first confirming `get_item`
  and `board_overview` return current data over the same connection would have filed a false defect
  against code that had already been reviewed eleven times over. The distinction is worth a standing
  habit at any delivery step touching a live MCP connection: before reporting a schema mismatch as a
  server defect, retry once and cross-check a tool whose contract did not change.
- **A local git checkout can drift far enough behind its own remote to make a real, merged field look
  missing.** Grepping the main checkout for `QuestionsNeedingHumanAttention` came back empty and
  briefly looked like a serious gap between REP-08's delivery annotation and the shipped code, before
  `git status` showed the checkout was 62 commits behind `origin/main` — it had never been synced
  after work moved to a dedicated worktree. Worth checking `git rev-parse HEAD` against
  `origin/<branch>` before treating an absence in a local checkout as an absence in the codebase.
- **Fast-forwarding a long-stale branch onto a shared remote can collide with untracked local work
  that isn't yours to discard.** Bringing the six-week-stale main checkout current hit seven
  untracked files (an unrelated dashboard-visualization objective's wireframes and plan) that would
  have been overwritten by the merge. Diffing each byte-for-byte against the incoming committed
  version before removing any of them confirmed no content would be lost; the same collision with an
  actual divergence would have needed a different resolution entirely, and checking first is what
  made that distinction available.

