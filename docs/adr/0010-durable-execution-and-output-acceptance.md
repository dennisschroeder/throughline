# ADR 0010: Durable execution graph and output acceptance

- Status: Accepted
- Date: 2026-08-21

## Decision

Milestone 2 adds structured acceptance criteria, typed dependencies, append-only activity,
immutable artifact-backed OutputRevisions, append-only ValidationRecords, and reusable
OutputRequirements. Readiness remains a derived query over authoritative rows; it is never cached.
Hard dependency cycles are rejected with a recursive CTE inside the linking transaction.

Output acceptance evaluates the persisted OutputProfile validation definition. Each required entry
uses its explicit `criterion_ref`, then rubric, then validator kind as its stable reference. The
latest append-only record for each reference determines its verdict; `passed` and explicitly
recorded `waived` satisfy the requirement. Validation is isolated by exact revision. Profiles with
no required validations accept a produced revision immediately in the creation transaction.
Non-empty ExpectedOutput instance contracts must declare their own `validation.required` entries;
profile and instance requirements are evaluated conjunctively and duplicate criterion references
are rejected.
Acceptance closes the revision state but not its evidence stream: later passed `successor_use`
records may be added without reevaluating or mutating the accepted verdict. Contract validation and
failed/waived evidence cannot be appended after acceptance because latest-verdict semantics would
otherwise contradict the durable accepted state.

OutputRequirement supports an exact OutputRevision or a profile name with a deliberately minimal
exact-version constraint (`N` or `=N`). Range solving and registry behavior remain later work.
Required references block readiness until a matching revision is accepted. SQLite triggers protect
revision content, artifact bindings, artifacts, validations, and activity from updates or deletion.
Artifact bindings are finalized in the creation transaction and reject later insertion. Accepted
output discovery is bounded and filters by exact profile version, objective, producer, or recency.
Later revisions may reuse an existing immutable Artifact reference for the same work-item URI while
recording a new revision digest and a newly finalized binding set.

Work-item execution transitions implement only the lifecycle and completion gates needed by this
milestone. Claims, actor capability matching, idempotency, the complete transition policy, manual
blockers, external actions, and delegated authority remain later milestones. Actor identifiers are
still trusted local audit strings; Workgraph records actions and never executes external effects.

## Consequences

A non-code dossier can be produced as an immutable revision, validated against persisted profile
data, accepted, discovered, and reused exactly by another objective. Dependent ready work becomes
visible only after hard prerequisites are done and required reusable outputs are accepted. The CLI
provides `ready` and structured `show` inspection without bypassing the application layer.

This migration does not synthesize historical activity for changes made before the activity table
existed. Every application mutation after migration emits activity transactionally; failure to
record that event rolls back the mutation. Cancellation reasons are retained in immutable activity.
