# ADR 0011: Safe multi-actor coordination and delegated authority

- Status: Accepted
- Date: 2026-08-22

## Decision

Milestone 3 introduces explicitly registered trusted-local actors with a declared kind and
capabilities. Existing historical actor strings remain audit data and are not retrospectively
classified. New claims, capability assignments, approvals, grants, and execution records reference
registered actors.

Claims are exclusive leases. A lease duration and each renewal extension must be between 60 seconds
and 8 hours. Claim acquisition transactionally releases expired leases before checking availability;
release and renewal require the exact claim ID and owning actor. An expired or released claim cannot
be renewed. Every visible WorkItem mutation compares its caller-supplied version, increments the
version on success, and returns a typed conflict rather than overwriting newer state.

Each Milestone 3 mutation accepts an actor-scoped idempotency key. The operation name and a deterministic hash
of its request are stored with the original response in the same transaction as the entity changes
and activity. An identical retry replays that response. Reusing a key for a different operation or
request is rejected.

Transition and claim eligibility evaluate one explicit snapshot of facts and return stable,
ordered requirement failures. Readiness is derived from the same facts; it is never a cached flag.

Authority remains distinct from capability. An ExternalAction revision stores the canonical
AuthorizationSubject bytes and SHA-256 digest returned by the existing single canonicalization
path. Metadata edits do not create a revision or affect grants. Any subject change creates a new
revision and leaves prior grants historical but unusable for the current action.

An approval creates one grant for its exact action revision, digest, principal, constraints, and
optional expiry. V1 grant constraints are exact canonical JSON equality only; it has no policy
expression evaluator. Authorization is derived at read/start time from the current revision,
principal, capability facts, grant state, and expiry; the action's lifecycle state is not treated as
the authorization decision. Revocation is append-only state, never deletion.

Workgraph records but never performs external effects. Starting an execution rechecks authorization
in the same write transaction. A terminal result must bind that started attempt; success also
requires result evidence. A later revocation or expiry does not prevent recording the historical
terminal result of an already authorized start.

## Consequences

The database can safely coordinate multiple local actors without silently losing WorkItem changes or
allowing overlapping active leases. Retried requests are deterministic. Capability alone cannot
authorize an effect, and payload or principal drift cannot reuse a grant. These checks remain
trusted-local workflow safeguards, not authentication or isolation guarantees.

The migration generalizes the existing plan-only approval table while preserving its rows. SQLite
foreign keys, uniqueness constraints, and triggers defend immutable revisions, one-way revocation,
and execution audit integrity; application services retain responsibility for policy-free,
deterministic evaluation and transaction composition.
