# ADR 0008: Intent and planning governance boundary

- Status: Accepted
- Date: 2026-08-21

## Decision

Milestone 1 persists typed context, questions, accepted decisions, plan-target approvals, recursive
proposed work, capability requirements, and governed OutputProfile proposals. Plan approval and the
acceptance or rejection of all included work items occur in one SQLite transaction. Entering an
objective's execution phase requires an approved plan. A newly approved plan revision supersedes
earlier approved revisions and their work items.

Context records follow kind-specific lifecycles and may supersede earlier records. Questions can be
answered or waived, and accepted decisions can be superseded, preserving their audit history.

Actor identifiers remain opaque trusted-local audit strings in this milestone. Actor registration,
actor capability matching, claims, and authenticated authority remain Milestone 3 concerns. The
approved no-plan execution exception is not yet implemented.

A planned installation is represented by a domain-neutral `tool_installation` WorkItem with an
execution policy, capability requirements, and ExpectedOutput. Exact ExternalAction revisions,
principal-bound grants, and execution evidence remain outside this milestone; Workgraph performs no
installation or other external effect.

## Consequences

A new agent can recover intent and approved commitment without transcript state, while proposed work
and proposed output vocabulary remain unusable until review. The next milestone can add the durable
execution graph without changing the adapter-independent planning contracts. Multi-actor safety and
external-action authority must not be inferred from the trusted actor strings recorded here.
