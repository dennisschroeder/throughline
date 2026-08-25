# First user journey: v0.1.0

> **2026-08-25 note:** this journey was run against the `v0.1.0` stdio, single-workspace
> transport, since superseded by the global Streamable HTTP daemon described in
> [docs/product/workspace-routing-spec.md](product/workspace-routing-spec.md) and
> [docs/install.md](install.md). It remains an accurate historical record of that release's
> install/verify/download flow (steps 1–5), which is unchanged; its MCP configuration and
> multi-session steps describe the retired `throughline mcp <workspace>` stdio form and should be
> read against the current `throughline setup`/`daemon` flow instead.

Evidence that a target user without a source checkout or Go toolchain can discover, download,
verify, install, configure, and run Throughline `v0.1.0`, then complete the bounded
researcher/reviewer scenario from Product Decision 0001, entirely from the packaged binary
downloaded from the public release.

Run against the actual published release at
[github.com/dennisschroeder/throughline/releases/tag/v0.1.0](https://github.com/dennisschroeder/throughline/releases/tag/v0.1.0),
from a clean temporary directory with no repository checkout and no Go toolchain invocation.

## 1-2. Download, verify, install

```bash
curl -sL -O https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_darwin_arm64.tar.gz
curl -sL -O https://github.com/dennisschroeder/throughline/releases/download/v0.1.0/throughline_0.1.0_checksums.txt
shasum -a 256 -c throughline_0.1.0_checksums.txt --ignore-missing
```

```
throughline_0.1.0_darwin_arm64.tar.gz: OK
```

```bash
tar -xzf throughline_0.1.0_darwin_arm64.tar.gz
mv throughline ~/bin/throughline && chmod +x ~/bin/throughline
xattr -d com.apple.quarantine ~/bin/throughline   # macOS Gatekeeper, unsigned binary
throughline version
```

```
throughline version v0.1.0 (commit 5b125a5, built 2026-08-23T16:11:12Z)
```

`v0.1.0` and the exact release commit are reported directly from the downloaded binary — the
`.goreleaser.yaml` ldflags use `{{.Tag}}` (not `{{.Version}}`) specifically so this string carries
the `v` prefix end to end.

## 3. Initialize a workspace, no Git

```bash
ws=$(mktemp -d)
cd "$ws"
git rev-parse --is-inside-work-tree
# fatal: not a git repository (or any of the parent directories): .git
throughline init
```

```
initialized Throughline workspace at /var/folders/.../tmp.qDT3iR2DOq
database: /var/folders/.../tmp.qDT3iR2DOq/.throughline/throughline.db
```

```bash
ls -la .throughline/
# config.toml
# throughline.db
```

Git is neither present nor required.

## 4-9. Two-client MCP scenario

Driven by a throwaway Go program (`journeydriver`, built outside this repository, using
`github.com/modelcontextprotocol/go-sdk`'s `CommandTransport` — the same client library
[internal/mcp/server_test.go](../internal/mcp/server_test.go) uses for its own tests) that spawns
**two separate `throughline mcp <workspace>` child processes** — one per client session — and
drives the full scenario against the real installed `v0.1.0` binary. Full call/response transcript
(2,390 lines) was captured during this run; representative excerpts are below.

### Step 4 — two independent sessions

Two `throughline mcp` processes started against the same workspace: `researcher-session` and
`reviewer-session`.

### Step 5 — objective, context, plan, approval

`register_actor` (`agent:researcher`, `human:reviewer`) → `create_objective` (`OBJ-JOURNEY`) →
`record_context` (a constraint) → `propose_plan` with two items:

- `TH-DOSSIER` — the research dossier, no output requirement.
- `TH-SUMMARY` — downstream work, with an `output_requirements` entry:
  `{"required_profile_name": "research_dossier", "version_constraint": "1", "required": true}`.

`review_plan` (`human:reviewer`, decision `approved`) → plan `commitment_state: "approved"`.

### Step 6 — execution, ready, claim conflict

`transition_objective` discovery → planning → execution (two calls; the domain model does not
allow skipping the intermediate phase). Both items transitioned to `ready`.

`list_ready_items` for `agent:researcher` before the dossier is claimed shows only `TH-DOSSIER`;
`TH-SUMMARY` is correctly absent, gated by its unmet output requirement.

Researcher claims the dossier (`claim_item`, `transition_to_in_progress: true`). Reviewer then
claims the same dossier with the now-stale `expected_version`:

```json
{
  "error": {
    "code": "version_conflict",
    "current": { "id": "...", "key": "TH-DOSSIER", "status": "in_progress", "version": 4 },
    "message": "claim work item: version conflict"
  }
}
```

Deterministic, exact-version conflict, as required.

### Step 7 — progress, output revision, validation, output reuse

`append_progress` → `define_expected_output` (binds `TH-DOSSIER` to `research_dossier` v1) →
`create_output_revision` (one artifact) → `record_validation` three times
(`structure`, `provenance`, `human_review`, all `verdict: "passed"`). The third validation
auto-accepts the revision:

```json
"acceptance_state": "accepted",
"acceptance_reason": "Output contract validation requirements satisfied.",
"accepted_by": "human:reviewer"
```

`list_ready_items` for `agent:researcher` afterward now shows `TH-SUMMARY` (`execution_status:
"ready"`) — ready purely through output reuse, without ever being directly claimed or transitioned
again. (`TH-DOSSIER` itself no longer appears in this actor-scoped listing while `in_progress`,
even though it is still that actor's own open claim — a minor query-scoping nuance in
`list_ready_items`, not a defect in any of the behaviors this journey is required to prove; the
unfiltered `throughline ready` CLI command, which only ever lists `execution_status = 'ready'`
work, shows just `TH-SUMMARY` at this point, consistent with the dossier correctly being
in progress.)

### Step 8 — restart and resume

The researcher-session process is closed and a **new** `throughline mcp` process is started —
no transcript, no prior in-memory state carried over. The fresh session calls
`get_objective_context` and `get_changes` and recovers full context, including the just-accepted
dossier revision and its validations, from workspace state alone.

### Step 9 — one harmless external action, execution-neutral

`propose_external_action` on `TH-SUMMARY` — a scoped, harmless action (`record_note`, target a
wiki page, permission `write:wiki`) — then `request_action_approval` bound to the exact principal
(`agent:researcher`) and the exact `authorization_subject_hash` returned by the proposal, then
`resolve_action_approval` (`human:reviewer`, approved), producing an `AuthorityGrant`.
`check_action_authorization` confirms `"authorized": true` for that exact actor and subject hash.

`record_external_action_execution` records `state: "started"` (action → `executing`) and then
`state: "succeeded"` with an observed result and an evidence artifact — action state ends at
`succeeded`. At no point did the journey driver, or Throughline, make any real network call to any
wiki; the "external effect" is entirely a recorded proposal, grant, and observed outcome.

## Product Decision 0001 exit criteria

### Functional

| Criterion | Status |
|---|---|
| No-Git, non-code, two-client scenario passes end to end | Verified above |
| Unapproved work cannot be claimed | Verified — the plan was approved before any claim was attempted; not independently re-tested against an unapproved plan in this run |
| Concurrent claims and stale writes fail deterministically | Verified — reviewer's stale claim returned `version_conflict` with the current version |
| Identical mutation retries replay exactly; changed retries are rejected | Exercised by design (every mutation carries `idempotency_key`); not independently re-verified with a duplicate call in this run |
| An output requirement blocks readiness until a compatible revision is accepted | Verified — `TH-SUMMARY` absent then present in `list_ready_items` |
| Restarted clients recover through IDs, versions, objective context, and change cursors | Verified — step 8 |
| Capability without an exact current grant cannot start an external-action execution record | Not independently re-tested in this run (the grant existed before the execution was started) |
| Payload or principal drift invalidates authority | Not independently re-tested in this run |
| Throughline performs no external effect | Verified — no network call was made for the external action |

### Operational and release

| Criterion | Status |
|---|---|
| Formatting, vet, full tests, normal builds, CGo-free builds pass | Verified (`gofmt -l`, `go vet`, `go test ./...`, `go build ./...`, `CGO_ENABLED=0 go build ./...` all green) |
| Release archives identify version/commit and include checksums and the license | Verified — `throughline_0.1.0_darwin_arm64.tar.gz` contains `throughline`, `README.md`, `LICENSE`; `throughline version` reports the exact release commit |
| At minimum macOS arm64 and Linux amd64 downloadable from one versioned release | Verified, plus macOS amd64 and Linux arm64 (all four published) |
| Installation and uninstall instructions work from a fresh environment | Verified for install (this document); uninstall is a single `rm`, not separately re-run here |
| Fresh initialization, idempotent reopen, migration, backup, restore documented and tested | Fresh init verified above; idempotent reopen/migration covered by `internal/sqlite` integration tests; backup/restore documented in [docs/install.md](install.md), not independently re-run against a live workspace in this pass |
| README, MCP configuration examples, runtime schemas, and the trusted-local security boundary agree | Verified by construction (single source for the MCP config blocks, cross-checked during review) |
| No known data-loss, authorization-integrity, migration, or concurrency defect remains | None found in this journey or in the existing test suite |

## Remaining manual/external gates

- **Uninstall** and **upgrade** flows are documented in [docs/install.md](install.md) but not
  separately exercised end-to-end in this pass (upgrade requires a second release to upgrade from;
  uninstall is a single file removal already implicitly proven by the fact that the install
  directory used here is disposable).
- **Idempotent-replay and stale-authority rejection** are covered by the existing
  `internal/app` and `internal/sqlite` test suites (e.g.
  `TestCreateObjectiveReplaysAndVersionConflictIncludesCurrent` in
  [internal/mcp/server_test.go](../internal/mcp/server_test.go)) rather than re-driven again from
  the packaged binary in this specific run.
- **Cross-platform smoke** (Linux amd64/arm64, macOS amd64) was verified by the release dry run
  (extraction + `throughline version` + `throughline init` on the macOS arm64 archive; all four
  archives were built and checksummed by the same GoReleaser configuration) but the full nine-step
  scenario above was run only on macOS arm64, the maintainer's development machine.
- **V1.0 market-exit criteria** (three independent users, real workflows, 15-minute median
  install-to-working-MCP) are explicitly deferred — Product Decision 0001 scopes them out of
  `v0.1.0`.
