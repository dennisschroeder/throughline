# ADR 0027: Read-only visualization scope

- Status: Accepted
- Date: 2026-08-27

## Context

[Product Decision 0001](../product/0001-market-testable-v0.1.md) fixed the `v0.1.0`/V1.0 release
boundary and explicitly excluded a web UI, listing "Web UI or Kanban application" under the human
surface row and naming its own trigger for revisiting that exclusion: "Web UI when non-MCP human
reviewers cannot participate effectively" (Product Decision 0001, "Later, only if validated").

That trigger condition has now occurred through real usage. A human reviewer working alongside
MCP-capable agents has no way to get an overview of decisions, questions, tasks, and progress other
than reading raw MCP tool output — there is no structured, at-a-glance view of coordination state.
This gap was recorded and discussed as objective `OBJ-DATA-VISUALIZATION` in Throughline's own
coordination database, which reached the decision: "Scope boundary deliberately extended: a
read-only human overview UI is now in scope." This ADR documents that scope extension the same way
ADR 0009 documented `OBJ-WORKSPACE-ROUTING` extending the original boundary for workspace routing.

## Decision

A read-only, human-facing visualization layer is now in scope, on two tracks:

- **Inline elements**, rendered in an agent chat/session as part of MCP tool responses.
- **A live browser dashboard**, backed by the running daemon, for a standalone overview outside any
  agent session.

Both tracks are strictly read-only. No mutation happens through the visualization layer under this
decision; all mutations remain agent-executed via the existing MCP tools. Interactive drag-and-drop
mutation from the UI (e.g. dragging a card to change its status) is explicitly considered and
explicitly deferred — it is not in scope now and requires its own future decision.

The original release boundary's other exclusions remain intact and are unaffected by this decision:
a policy language, orchestration, semantic/vector search, broad CLI parity, and distributed
(cross-machine) coordination stay out of scope.

## Consequences

- README.md's "Scope boundary" section, which currently lists "a web UI" as unconditionally out of
  scope, needs updating to reflect that a *read-only* visualization UI is now in scope while
  mutation-capable UI remains excluded.
- Future visualization work proceeds under this decision, tracked as objective
  `OBJ-DATA-VISUALIZATION` with work items `TH-VIZ-01-SPIKE`, `TH-VIZ-02-SURVEY`, and
  `TH-VIZ-03-DASHBOARD`.
- The daemon's core security model (ADR 0018) is unaffected: a read-only dashboard is another
  authenticated consumer of daemon state, not a new mutation path or a new trust boundary.
- Any future proposal to make the visualization layer mutation-capable requires its own product
  decision, not an extension of this one.
