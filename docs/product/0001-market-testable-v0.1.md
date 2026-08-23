# Product Decision 0001: Market-testable v0.1 and V1.0 exit contract

- Status: Accepted
- Date: 2026-08-23
- Decision owner: project maintainer

## Context

The implementation handoff describes the architectural destination, while the implemented kernel
already covers the first coherent market-facing journey. Treating every destination capability as
a release requirement would make “V1 ready” expand indefinitely.

## Decision

`v0.1.0` is the first market-testable release:

> A local, headless coordination state layer that lets one human and two MCP-capable agents resume,
> approve, safely claim, complete, validate, reuse, and audit one domain-neutral workflow across
> sessions using one SQLite workspace.

No new domain feature is required before `v0.1.0`. Finish only release packaging, operational
documentation, and the bounded acceptance scenario. V1.0 is reached when the same product boundary
also satisfies the design-partner outcome criteria below.

## Target adopter and job to be done

Target an AI-native technical operator, solo builder, or small local team using multiple
MCP-capable agent sessions for research, design, operational, or knowledge workflows.

> When work spans agents, sessions, and human approval, let me resume the approved next step without
> restating context, prevent conflicting work, and know exactly what was produced, accepted, reused,
> and authorized.

## Primary journey

A user initializes a workspace for a vendor-research objective and connects researcher and reviewer
clients. The researcher records constraints and proposes a plan containing a research dossier and
downstream work that requires the accepted dossier. The reviewer approves the plan. The researcher
claims the dossier; the reviewer cannot claim it concurrently. The researcher records an immutable
output revision and the reviewer validates it, making the downstream work ready. After a restart,
another session resumes from objective context and deltas, reuses the dossier, obtains an exact
grant for one scoped external action, performs that action outside Throughline, and records its
result and evidence.

## Release boundary

| Area | v0.1/V1 in | Explicitly out |
|---|---|---|
| Deployment | One binary, workspace, local host, and SQLite file | Cloud service, sync, network filesystem |
| Interfaces | Single-workspace MCP stdio; `init`, `ready`, `show`, version reporting | Network transport, multi-workspace server, broad CLI parity |
| Intent | Objective, typed context, questions, decisions, approved plan | Generic project hierarchy, chat archive |
| Execution | Ready queries, dependencies, blockers, attention, leases, concurrency, idempotency | Agent selection, launching, scheduling, orchestration |
| Outputs | Exact profiles, expected outputs, immutable revisions, validation, acceptance, reuse | Profile registry, compatibility solver, blob storage |
| External effects | Exact proposal, principal-bound grant, authorization check, result/evidence | Effect execution, credential management, policy-generated grants |
| Identity | Trusted-local actor strings and declared capabilities | Authentication, IAM, isolation, remote identity |
| Human surface | MCP-capable client and compact CLI inspection | Web UI or Kanban application |
| Retrieval | Structured overview, context, item retrieval, cursor deltas | Semantic/vector search or generated ranking |
| Coordination | Multiple local processes | Distributed or cross-host coordination |

Existing capabilities outside the primary journey may remain available, but they do not enlarge the
release contract or justify adjacent feature work.

## v0.1.0 exit criteria

### Functional

- The no-Git, non-code, two-client scenario passes end to end.
- Unapproved work cannot be claimed.
- Concurrent claims and stale writes fail deterministically.
- Identical mutation retries replay exactly; changed retries are rejected.
- An output requirement blocks readiness until a compatible revision is accepted.
- Restarted clients recover through IDs, versions, objective context, and change cursors.
- Capability without an exact current grant cannot start an external-action execution record.
- Payload or principal drift invalidates authority.
- Throughline performs no external effect.

### Operational and release

- Formatting, vet, full tests, normal builds, and CGo-free builds pass.
- Supported release archives identify version/commit and include checksums and the license.
- At minimum, macOS arm64 and Linux amd64 artifacts are downloadable from one versioned release.
- Installation and uninstall instructions work from a fresh environment.
- Fresh initialization, idempotent reopen, migration, backup, and restore are documented and tested.
- README, MCP configuration examples, runtime schemas, and the trusted-local security boundary agree.
- No known data-loss, authorization-integrity, migration, or concurrency defect remains.

## V1.0 market exit criteria

- Three independent target users install and initialize without maintainer database intervention.
- At least two use Throughline for a real workflow rather than the supplied scenario.
- Those two resume after a restart or separate-day session without copying the prior transcript.
- At least two complete an approved output and reuse it or name a concrete second workflow.
- Median first-install-to-working-MCP time is at most 15 minutes.
- Users can explain what is ready, blocked, claimed, accepted, and authorized from Throughline state.

These criteria establish credible early demand, not broad product-market fit.

## Later, only if validated

- Authenticated/networked identity when users require a real remote security boundary.
- Policy language when repeated manual approval is the dominant friction.
- Web UI when non-MCP human reviewers cannot participate effectively.
- Orchestration only as a separate harness/integration layer; the core remains execution-neutral.
- Multi-workspace/network transport when users routinely operate many workspaces or remote teams.
- Semantic search when structured context and deltas fail at observed graph sizes.
- Broad CLI parity when users need to operate without an MCP client.
- Distributed coordination when cross-host demand justifies a server architecture.

## Validation questions

1. Show the last workflow where context had to be re-explained to another agent or session. What was
   lost, and what did recovery cost?
2. Which facts must be authoritative before another agent may continue: plan, owner, output
   acceptance, action permission, or something else?
3. During the primary journey, where does structure reduce uncertainty and where does it feel like
   bookkeeping?
4. What exact event makes trusted-local identity or an MCP-only human surface insufficient?
5. What would the user run through Throughline next week, and which workaround would it replace?

## Consequences

The implementation handoff is an architectural north star, not the current release contract. A web
UI, authentication, orchestration, richer search, additional transports, and broad CLI work cannot
block `v0.1.0` or V1.0 without a new product decision supported by observed demand.
