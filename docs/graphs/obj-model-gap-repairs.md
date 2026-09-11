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

