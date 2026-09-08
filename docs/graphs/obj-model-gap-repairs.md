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

## Feedback

Pending delivery. Record where estimates, node boundaries or the designed graph diverged from the
actual work, including when there is nothing to report.
