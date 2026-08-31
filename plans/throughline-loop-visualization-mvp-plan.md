# Throughline Loop MVP: Kanban Board and Inline Gates

## Summary

- `throughline ui --actor <human-id> [workspace]` starts a loopback-only local Kanban board.
- An MCP App tool displays only the current blocking gate inside the agent host.
- The board projects each objective into `Backlog`, `Ready`, `In progress`, `Review`, and `Done`; SQLite and the work graph remain authoritative.
- A “Needs you” queue combines all durably actionable approvals, plan and profile reviews, open questions, and work-item attention.
- Throughline records decisions and control requests but does not execute agents or external actions.

## Interface and behavior

- When the workspace contains one objective, open its board directly. When it contains multiple objectives, show an objective picker first. `--objective` bypasses the picker.
- Cards show title, kind, priority, active claim and expiry, execution policy, attention state, blocker reasons, and criterion/output progress.
- Selecting a card opens a detail drawer containing context, dependencies, progress, artifact links, output revisions, and activity.
- The “Needs you” queue supports:
  - Plan approval or rejection through `review_plan`.
  - Output-profile activation or rejection through `review_output_profile`.
  - Approval, rejection, or revocation of generic plan, work-item, output-profile, and output-revision approvals.
  - Exact ExternalAction review showing the AuthorizationSubject, principal, constraints, and expiry, with approval, rejection, and grant revocation.
  - Answering or explicitly waiving an open question.
  - Clearing a work-item attention state.
- Rejection, waiver, revocation, and objective pause require a rationale.
- “Pause objective” changes only the authoritative objective state. It does not terminate active agent processes; connected harnesses observe the change through `get_changes`.
- The MVP has no drag and drop, claim takeover, release of another actor's claim, custom columns, or arbitrary embedded third-party HTML.
- Artifacts open as safe links. Live previews and local artifact hosting remain later work.
- Poll the activity cursor every two seconds. Reload the snapshot only when changes exist. Back off failures to at most 15 seconds and provide a manual retry.

## Application and data model

- Add a transport-neutral `GetLoopSnapshot` application query:
  - `LoopSnapshot`: objective, five status columns, `NeedsHuman`, recent activity, and change cursor.
  - `LoopCard`: work item, active claim, blockers with stable reason codes, criterion/output progress, and attention state.
  - `LoopGate`: gate ID, kind, target, requester, timestamp, expected version, allowed decisions, and typed detail payload.
- Compute blocker reasons from the same facts used by readiness evaluation, never from UI status heuristics.
- Build each snapshot from one consistent SQLite read view.
- Add no UI-specific domain state and no new domain table.
- Continue to route every mutation through existing application services and domain validation; UI handlers must never write directly to SQLite.

## CLI and local HTTP server

- Add:

  ```text
  throughline ui --actor human:reviewer [--objective ID] [--listen 127.0.0.1:0] [--open] [DIRECTORY]
  ```

- Require the actor to be registered with kind `human`.
- Permit loopback listen addresses only. Use a dynamic port by default and print the resulting URL.
- Expose:
  - `GET /api/v1/objectives`
  - `GET /api/v1/loop?objective_id=…`
  - `GET /api/v1/changes?objective_id=…&since=…`
  - Explicit POST endpoints for plan review, output-profile review, generic approval resolution, action-approval resolution, question resolution, work-item attention resolution, and objective pause.
- Derive the actor exclusively from the UI server session; never accept an actor override from a request body.
- Generate a stable idempotency key in the browser for each user intent and reuse it for retries.
- Return HTTP 409 for `version_conflict` and `approval_stale`. Refresh the selected gate/drawer and require a new decision instead of overwriting state.
- Disable CORS, require same-origin JSON mutations, validate `Origin` and `Host`, apply a restrictive CSP, and use SameSite cookies.

## MCP App

- Add a read-only `show_loop_gate` tool with `actor_id`, optional `objective_id`, and optional `gate_id`.
- Return the typed gate snapshot as structured content so unsupported clients retain a useful text/JSON fallback.
- Reference `ui://throughline/loop-gate` in the tool metadata and serve the resource as `text/html;profile=mcp-app`.
- The Gate Card may invoke only existing MCP mutation tools.
- Use exact expected versions, action versions, principals, and AuthorizationSubject hashes for decisions.
- Capability negotiation must preserve normal structured tool behavior when the host does not support MCP Apps.

## Frontend

- Use vanilla TypeScript with two Web Components:
  - `throughline-loop-board` for the complete browser board.
  - `throughline-gate-card` for the compact inline decision surface.
- Share one view model with separate browser-fetch and MCP-App postMessage transport adapters.
- Use esbuild and the MCP Apps SDK for transport only; do not add a UI framework.
- Embed compiled assets into the Go binary.
- Keep generated assets committed so `go build ./...` continues to work without Node. CI must rebuild them and fail when the generated diff is non-empty.
- Below 900 px, replace the five simultaneous columns with a selectable status view instead of compressing cards into unreadable columns.
- Preserve keyboard operation, visible focus, semantic dialogs, and accessible status announcements.

## Product documentation

- Add Product Decision 0002 documenting:
  - Kanban as a derived human-control projection, not authoritative workflow state.
  - The local board as the human-on-the-loop overview.
  - The inline Gate Card as the human-in-the-loop control point.
  - The continued execution-neutral boundary of the Throughline core.
- Update usage, build, install, and release documentation for the UI command, frontend build, trusted-local browser boundary, and MCP App fallback.

## Test plan

### Application and SQLite

- Each work item appears in the correct execution-status column.
- Claims, expiry, attention, criteria, outputs, and concrete blocker reasons are correct.
- Every supported gate type appears exactly once with the correct allowed decisions.
- Snapshot data and the activity cursor remain consistent under concurrent changes.

### HTTP

- Verify actor injection, loopback/origin/host protection, and JSON-only mutations.
- Verify every decision reaches the existing application service and records activity.
- Identical retries replay exactly.
- Stale versions and stale authorization subjects return 409 without mutation.

### MCP

- Verify tool and `ui://` resource registration.
- Verify the structured fallback without MCP App capabilities.
- Verify approval, rejection, and revocation use the exact version, principal, and subject hash.

### Frontend

- Test board columns, “Needs you”, drawer, Gate Card, conflict refresh, and cursor polling.
- Test keyboard navigation, focus, dialogs, accessible announcements, and responsive status selection.

### End to end

- In a temporary non-Git workspace: approve a plan, claim work, answer a question, resolve an output approval, authorize one exact ExternalAction, pause the objective, restart, and recover all state.
- Confirm that Throughline performs no external action.
- Run the frontend build/diff check, `gofmt -l`, `go vet ./...`, all tests, normal and CGo-free builds, and a GoReleaser snapshot.

## Acceptance criteria

- A human can understand ready, active, blocked, review, and completed work from one objective board.
- Every pending durable human decision is visible in “Needs you” and opens the exact governed context.
- Decisions remain idempotent, version-safe, auditable, and subject to current domain rules.
- An agent host with MCP App support can render and resolve a gate without opening the complete board.
- A host without MCP App support receives equivalent structured gate data.
- Pausing records durable control intent without claiming that Throughline stopped an external runtime.
- The released artifact remains one CGo-free Go binary with embedded UI assets.

## Assumptions and exclusions

- The MVP uses only attention states that are already durably resolvable: work-item attention, open questions, and all approval types. Activity-only `review`, `clarification`, and `intervention` targets do not gain a new `AttentionRequest` entity in this milestone.
- The trusted-local actor and security model remains unchanged.
- Plan/profile rejection with rationale represents “request changes”; do not add a `revision_requested` state.
- Output-revision approval remains distinct from validation-based output acceptance and must be labeled accordingly.
- No orchestration, authentication, remote access, arbitrary website embedding, live artifact server, drag and drop, or claim takeover is included.
