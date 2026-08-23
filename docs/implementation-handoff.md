# Throughline — implementation handoff

**Status:** architectural north star and technical brief; the bounded `v0.1.0` release contract is [Product Decision 0001](product/0001-market-testable-v0.1.md)

**Audience:** a fresh coding/design session  

**Product name:** `Throughline`; executable/module slug: `throughline`

**One-line thesis:** A lean, local-first, open-source authoritative state layer for domain-neutral agentic work, backed by SQLite and exposed primarily through MCP—deterministic from intent to accepted output and authorized external effect.

### Revision decision: non-code work is first-class

Throughline is domain-neutral. Its reference scenarios must include research, writing, operational and creative knowledge work, skill and agent design, tool/MCP/CLI installation, workflow design, structured deliverable production, evaluation, and human approvals—not only software implementation.

This scope matches the broader agentic-work pattern rather than a coding board: official OpenAI examples span automation, integrations, knowledge work, research, reports, dashboards, workflow audits, reusable skills, and CLI/tool creation ([ChatGPT/Codex use cases](https://learn.chatgpt.com/use-cases)). Throughline supplies durable state for such work; it does not replicate the runtime that performs it.

The full-history and follow-up domain audits changed six earlier assumptions:

1. The original `backlog → ready → in_progress → review → done` state machine is retained only as the **execution lifecycle**. It is insufficient as the lifecycle of an objective or plan.
2. Planning context requires first-class requirements, constraints, assumptions, findings/evidence, questions, decisions, approvals, plans, expected outputs, and objective phases.
3. The original MCP surface is the execution kernel, but it needs a small objective/context/plan surface in V1; otherwise the named domain concepts are read-only or forced into comments.
4. Designing a skill, subagent, MCP integration, CLI toolchain, research method, or output package is valid work. Throughline stores the authoritative plan, context, policies, checkpoints, and artifacts. The external agent runtime still performs the work and launches agents/tools.
5. “A task is done and a file is attached” is too weak. Throughline needs governed, immutable `OutputProfile` versions, immutable produced `OutputRevision`s, append-only `ValidationRecord`s, and reusable `OutputRequirement`s.
6. Capability is not authority. Any concrete effect outside Throughline is an `ExternalAction`; authorization binds an exact canonical action payload to a specific principal, scope, constraints, and lifetime. Throughline records the authority chain and result evidence but does not execute the action.

## 1. Read this first

Build a **coordination substrate**, not another project-management application.

The authoritative state is a shared work/context/authority graph: objectives, plans, work items, requirements, constraints, assumptions, findings, decisions, questions, approvals, dependencies, claims, progress, output profiles/contracts/revisions/validation/reuse, artifacts, external actions, grants, execution evidence, and activity. Humans and agents read and mutate the same state. MCP is the primary agent interface; a CLI and optional Kanban-style web UI are human-friendly control surfaces and projections over the same SQLite database.

The core promise is deliberately narrow but has two independent dimensions:

> A local, concurrency-safe control plane for domain-neutral agentic work, with a human-readable Kanban projection.

Its architectural USP is one binary, one SQLite file, open/local/headless operation, and protocol-level portability. Its semantic USP is a deterministic path through intent, commitment, work, outputs, validation, delegated authority, external effects, and reusable capabilities. “Non-code” is a mandatory proof condition, not the public category name.

The project must remain:

- **Local-first:** one portable SQLite database in the project, with no account, login, cloud workspace, or database server required.
- **Headless:** the domain and storage do not depend on a UI. MCP is a first-class interface, not an API bolted onto a board.
- **Vendor-neutral:** usable by Codex, Claude, ChatGPT, custom agents, and humans; no model or orchestration-framework lock-in.
- **Open source:** inspectable, self-hostable, embeddable, and extensible. Choose a permissive license unless a deliberate copyleft strategy is later approved.
- **Deterministic:** transactions, constraints, valid state transitions, optimistic concurrency, idempotency, and explicit leases protect shared state.
- **LLM-efficient:** query small summaries and deltas first; retrieve deep context only when needed.

The product is not “an AI app.” An LLM must not be required to use the state through the CLI or UI. Conversely, “not an orchestrator” does not mean “coding only”: Throughline can represent and coordinate the creation of workflows, skills, agents, research systems, tools, structured output packages, and explicit external actions while leaving their actual execution to the connected human/agent runtime.

## 2. Product rationale and positioning

### The problem

Agents and people repeatedly lose work context between sessions and tools. Chat transcripts are not a reliable shared source of truth, and a visual board alone is too weak for agents: it does not make executable work, dependencies, acceptance conditions, ownership, changes, or decisions queryable and enforceable.

Throughline makes the shared board/context the authoritative coordination state:

```text
                     authoritative shared state
  human ── CLI/UI ─────────────────────────────────┐
                                                     │
  agent ── MCP ── domain services ── SQLite database │
                                                     │
  another agent ── MCP ─────────────────────────────┘
```

A Kanban board is therefore **a projection of a work graph**, not the core data model. An agent sees executable work and compact state; a person sees overview, columns, activity, and attention queues. Both observe the same facts.

### Differentiation

The broad “shared workspace for humans and agents” market is crowded. This project competes by being lower-level, simpler, local, and composable:

- single-file SQLite persistence;
- no Docker, Postgres, Redis, account, SaaS workspace, API key, or required web service;
- first-class database-backed coordination semantics rather than opaque agent behavior;
- MCP/CLI first, UI optional;
- narrow, stable primitives rather than a full work-management suite;
- structured, token-efficient work/context retrieval.

The semantic differentiation must be equally concrete:

- a complete domain-neutral lifecycle that never requires Git, code, branches, pull requests, test suites, or CI;
- governed `OutputProfile` versions that define what finished work means without hardcoded work-type logic;
- immutable produced output revisions plus explicit machine, provenance, policy, or human validation records;
- accepted outputs that can become versioned requirements or capability evidence for later work;
- explicit `ExternalAction` proposals at the boundary to the outside world;
- authorization grants bound to one principal and the exact authorization-relevant action payload;
- an auditable chain from objective and approved plan to work, authority, execution result, and evidence.

The useful shorthand is: **less Jira/Plane for agents; more SQLite/Postgres-like coordination state for agentic work.**

An additional shorthand for the semantic direction is: **Throughline models work and delegated intent, not software-development tickets.**

### Competitive research snapshot

This is positioning context, not an implementation dependency. Revalidate licenses, capabilities, and current product claims before publishing marketing material.

| Project | Relevant overlap | Positioning implication |
|---|---|---|
| Plane | Broad canonical work graph for humans and agents; open-source community edition with commercial layers | Do not chase its full workspace/project-management scope. |
| Asana / Asana MCP | Existing commercial Work Graph exposed to agents through MCP | Throughline is local, open, and not tied to a SaaS subscription. |
| Muster | OSS human-agent collaboration platform with board/specs/agent visibility | Avoid building a broad collaboration suite. |
| Agent Kanban / kanban-mcp | Agents pull/claim work over MCP; Kanban persistence/UI | Differentiate through SQLite simplicity, strong deterministic semantics, and headless work graph. |
| Handoff | Proprietary hosted shared context graph/workspace | Local-first and open-source are material differences. |
| Handover | Hybrid hosted/open tooling around durable handoffs | Keep the core as an embeddable local state layer. |
| Concord | Shared workspace around heterogeneous agents, task memory, ownership, review | Focus on storage/query/coordination primitives, not an agent workplace. |
| Work Graph | Local/open work graph with memory, completion gates, and Kanban projection | Closest conceptual overlap; establish a concrete semantic and deployment advantage before broadening scope. |
| SCND.AI | Work graph/context-package direction, likely commercial product layer | Context projection is promising but not unique; keep it protocol-oriented and small. |

The clear proprietary examples from the prior research are **Asana** and **Handoff**. Plane is open-core-ish (community code is AGPL); Muster and Agent Kanban were reported as MIT; Handover is a mixed hosted/open-tooling case. Treat these classifications as research notes to verify, not legal advice.

Do not claim “non-code work” alone as unique. Asana and Plane already own broad cross-functional work; Handoff, Handover, and SCND.AI own substantial domain-general context/continuity territory; Muster, Agent Kanban, Concord, and Work Graph are closer on local coordination, claims, contracts, or evidence but remain code/repository-centered in their public positioning. The weakly occupied compound position is **domain-neutral outputs + deterministic acceptance + exact delegated authority + local/open/headless deployment + execution neutrality**.

## 3. Scope boundaries

### V1 must do

1. Persist shared work state in SQLite.
2. Preserve the full lifecycle from a vague objective through discovery, planning, approval, execution, review, and learning.
3. Let MCP clients discover, inspect, claim, update, and transition executable work safely.
4. Model requirements, constraints, assumptions, findings, questions, decisions, approvals, dependencies, blockers, acceptance criteria, expected outputs, artifacts, concise progress, and activity.
5. Distinguish an agent proposal from an approved shared commitment and prevent premature execution when the objective or plan has not been approved.
6. Represent capability requirements and execution authority without binding work to a particular vendor or model.
7. Offer compact overview, ready-work, planning-context, and incremental change queries.
8. Provide a minimal local CLI for initialization and operation.
9. Make phase-aware human projections possible, but do not make a UI a prerequisite for the useful server.
10. Define, govern, discover, produce, validate, and reuse versioned output contracts without adding code for each work domain.
11. Represent external side effects precisely enough that policy or an authorized human can grant least authority to a principal for an exact action payload.

### Explicit non-goals

- Not another Plane, Jira, Trello, Linear, or generic SaaS workspace.
- Not an agent runtime/orchestrator: it neither launches agents nor selects their planning methodology. It may model workflow steps, roles, capabilities, approvals, handoffs, and run state for external runtimes to consume.
- Not a replacement for Codex/Claude/ChatGPT/Cowork, GitHub, issue trackers, or a document store.
- Not a chat/transcript archive and never a storage location for hidden chain-of-thought.
- Not an IAM system or general policy engine in V1. Trusted-local identity makes grants deterministic workflow safeguards, not a hard security boundary; networked enforcement requires authentication and authorization later.
- No Docker, Postgres, Redis, or cloud account requirement.
- No mandatory board columns, card coordinates, avatars, covers, or other UI-first metadata in the core model.
- No semantic/vector search in V1. SQLite FTS5 is a later optional enhancement; structured query remains canonical.
- No attempt to define a new distributed multi-principal protocol in V1. Keep concurrency rules concrete and testable.

### Boundary: skills/harnesses versus Throughline

Use this rule for every proposed feature:

> It belongs in Throughline only if it is authoritative shared state or a deterministic operation over that state. Behavior, methodology, prompting, planning technique, and model-specific execution belong in skills or agent harnesses.

Examples:

| Belongs in a skill/harness | Belongs in Throughline |
|---|---|
| How to decompose a research project | A work item depends on another item and is not executable yet |
| How a coding agent writes tests | Acceptance criterion state and a completion gate |
| How an agent chooses among ready items | The server’s `list_ready_items` result |
| Prompt templates and reviewer method | A claim, handoff, decision, progress entry, artifact, or transition |
| Launching/subscribing an agent run | The durable work state that run changes |
| How to write a skill or design a subagent | The approved plan, requirements, required capabilities, output specification, artifact references, and review state for that work |
| How to research or install a tool | Sources/findings, an installation work item, explicit authority requirements for side effects, verification criteria, and resulting configuration artifact |
| Executing an install, send, publish, purchase, deployment, deletion, or API call | An exact ExternalAction proposal, authorization subject, principal-bound grant, execution status, result, and evidence |

### Domain-neutral first-class test

A workflow is first-class only if it can be completely modeled, planned, executed, reviewed, accepted, reused, and resumed without requiring a Git repository, branch, commit, pull request, source-code diff, test suite, build system, CI system, or coding-specific actor.

Setting `repository_id = NULL` on a coding-centric model does not pass this test. `WorkItem` must not know what code is. Git, filesystems, browsers, MCP installation, Slack, deployment, and other systems are capabilities, integrations, artifact providers, or external-action types—not fields of the work kernel.

## 4. Core concepts and terminology

Use these names consistently in code, tool contracts, UI, and documentation.

| Concept | Meaning |
|---|---|
| **Objective** | A durable goal that groups work items. It is not necessarily a project hierarchy node. |
| **Plan** | A versioned proposal for achieving an objective. A plan can be draft, proposed, approved, rejected, or superseded; approval commits its accepted work items without executing them. |
| **WorkItem** | The primary unit of proposed, executable, reviewable, or trackable work. It may describe research, writing, design, installation, configuration, integration, evaluation, approval, human action, or agent action—not only code. A task is a UI synonym only; do not conflate it with an MCP long-running tool task. |
| **ContextRecord** | A typed authoritative context node: requirement, constraint, assumption, finding/evidence, risk, or success metric. Each kind has its own small lifecycle where needed. |
| **AcceptanceCriterion** | A structured, individually stateful condition used to judge whether a work item is complete. |
| **OutputProfile** | An immutable, versioned, governed definition of an output class. It specifies required structure, semantic meaning, validation expectations, and deterministic acceptance conditions. Built-ins are seeded data, not hardcoded domain branches. |
| **ExpectedOutput** | A work-item-specific contract instance referencing an exact active OutputProfile version plus any narrower constraints. It describes what must be produced before work begins. |
| **OutputRevision** | An immutable produced candidate binding one or more Artifacts to one ExpectedOutput and exact OutputProfile version. Material changes create a new revision; prior validation never carries forward implicitly. |
| **ValidationRecord** | An append-only verdict and evidence record against one OutputRevision and criterion. The verifier may be a machine, policy, source/provenance check, successor, or named human. |
| **OutputRequirement** | A declared dependency on an accepted OutputRevision or compatible OutputProfile/version constraint. It makes validated outputs reusable by later work. |
| **Dependency** | A directed, typed relationship between two work items. A hard prerequisite blocks execution until its target is completed/cancelled according to policy. |
| **Blocker** | A persisted manual/externally observed reason work cannot proceed. Dependency, question, approval, phase, and capability blockers are derived; manual blockers have their own auditable lifecycle. |
| **Decision** | A concise, durable choice with rationale, alternatives, and scope. It is context, not a comment stream. |
| **Question** | An unresolved question that can request explicit human attention and can block work. |
| **Approval** | A durable request and decision for a plan, transition, output, profile proposal, or ExternalAction. For an ExternalAction, approval creates or revokes an AuthorityGrant bound to the exact action revision and principal. Approval is distinct from attention and workflow status. |
| **Artifact** | A typed external reference, such as a commit, PR, document, URL, file, or test result. Do not store large blobs in the database. |
| **Progress** | A concise checkpoint describing completed work, remaining work, discoveries, and blockers. Never raw model reasoning. |
| **Claim** | A lease-based, time-limited assertion that an actor is working a work item. |
| **Activity** | Append-only audit event recording a material state change or user/agent action. |
| **Actor** | A human, agent, or service identity, represented as a stable string plus optional type/display metadata. An Actor is called a **principal** when it is the subject of delegated authority. V1 does not authenticate these identities. |
| **Attention** | An orthogonal need for human review, a decision, clarification, or intervention. It is not a status column. |
| **Capability** | A vendor-neutral ability required or offered, such as `web_research`, `document_authoring`, `filesystem_write`, `mcp_installation`, or `human_approval`. Throughline records capability facts but does not select or launch the actor. |
| **ExecutionPolicy** | The autonomy gate for claiming/executing a WorkItem: `human_only`, `agent_may_propose`, `approval_required`, or `autonomous_with_report`. It never grants authority for an ExternalAction and is enforced only within trusted-local V1 identity. |
| **ExternalAction** | A versioned proposal for an observable side effect outside Throughline, such as install, publish, message, purchase, deletion, configuration change, deployment, permission grant, or external API call. It is not a WorkItem kind and Throughline never executes it. |
| **AuthorizationSubject** | The canonical authorization-relevant subset of one ExternalAction revision: action type, target, arguments, scope, permissions, credential requirements, and declared constraints. Descriptive metadata and progress are excluded. |
| **AuthorityGrant** | A least-authority record stating that one principal may perform one exact AuthorizationSubject within constraints and an optional expiry. V1 produces it from an explicit approval; a future deterministic policy layer may produce the same record. It is invalid when the subject hash, principal, constraints, or lifetime no longer match. |

The output terms intentionally form a pipeline rather than aliases:

```text
OutputProfile → ExpectedOutput → OutputRevision → ValidationRecord → accepted output
                                                           │
                                                           └→ OutputRequirement of later work
```

Similarly, capability and authority are independent:

```text
actor has required Capability
AND exact ExternalAction is covered by a valid AuthorityGrant
→ action is authorized for that actor
```

An accepted skill, agent definition, research method, workflow, or tool installation may provide evidence that a capability exists, but acceptance never grants an actor external authority automatically.

## 5. Architecture

### Principles

- SQLite is the source of truth. MCP, CLI, and UI all call the same application/domain service layer; none writes tables directly.
- Keep protocol state explicit. MCP clients receive and send identifiers, item versions, claim IDs, idempotency keys, and change cursors; do not rely on hidden connection/session state.
- Every mutating use case executes inside one SQLite transaction.
- Preserve append-only activity records. Current entity rows provide snapshots; activity enables deltas/audit.
- Prefer normalized relational fields for relationships and queryable facts. Use JSON only for small extensible payloads, never as a replacement for constraints/indexes.
- Enable foreign keys and WAL mode. Set a sensible busy timeout. Document that SQLite supports concurrent readers and one writer; V1 targets processes on one local host, not a globally distributed multi-writer service.
- Do not place a WAL database on an ordinary network filesystem. V1 supports one host/local filesystem; sharing across hosts requires a future server/sync architecture.
- Prefer one long-lived writer connection per server process and a bounded read pool. Apply foreign-key, WAL, and busy-timeout pragmas to every connection and test separate-process CLI/MCP contention.

### Implementation language decision: Go over Rust

The implementation target is a single cross-platform binary with embedded SQLite and a small operational footprint. Both Go and Rust satisfy that constraint. As of 2026-08-21, both have official MCP SDKs supporting MCP `2026-07-28`; the [Go SDK](https://github.com/modelcontextprotocol/go-sdk) is in the MCP Tier 1 set, while the [Rust SDK](https://github.com/modelcontextprotocol/rust-sdk) is official and rapidly maturing but was still described as beta in the 2026-07-28 MCP release material. Rust SDK 3.1.x releases have since closed substantial conformance/documentation work, so this is an ecosystem-risk difference, not a capability blocker.

| Criterion | Go | Rust |
|---|---|---|
| Single-binary delivery | Excellent; normal toolchain behavior | Excellent; static/self-contained output is practical |
| SQLite packaging | Excellent with CGo-free [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite); simple cross-compilation | Excellent with [`rusqlite` + `bundled`](https://github.com/rusqlite/rusqlite); compiles and links SQLite into the program |
| SQLite ergonomics | Familiar `database/sql`, explicit transactions, easy test setup | Very strong low-level ergonomics and type safety; synchronous `rusqlite` maps well to SQLite |
| MCP implementation risk | Lowest: official Tier 1 SDK with stable v1 API | Low-to-moderate: official SDK is capable and current, but younger and more macro/async-runtime oriented |
| Cross-compilation | Simplest when using CGo-free SQLite | Good, but bundled C SQLite means target C toolchains/linking need more release engineering |
| Domain correctness | Strong with explicit types, validators, and exhaustive tests | Strongest compile-time guarantees and enum/state modeling |
| Development/contributor cost | Lower; fast builds and a small idiomatic service/CLI surface | Higher learning/build complexity, especially around async ownership and trait boundaries |
| Runtime/resource overhead | Small and entirely acceptable for a local MCP/CLI service | Usually smaller and more controllable, but not material for this workload |
| Best reason to choose | Fastest path to a boring, portable, maintainable binary | Choose if maximal compile-time rigor and Rust expertise outweigh ecosystem/release friction |

**Decision:** implement V1 in **Go**. The product’s hard problems are domain semantics, SQLite transactions, protocol contracts, and context modeling—not memory safety or extreme throughput. Go better serves the “boringly simple infrastructure” USP and currently minimizes MCP and cross-platform packaging risk.

Recommended Go stack:

- Supported current Go release; pin the version in CI/tooling.
- Official `github.com/modelcontextprotocol/go-sdk/mcp`.
- `modernc.org/sqlite` behind `database/sql` for CGo-free builds. Pin its direct `modernc.org/libc` version as its documentation requires. Reconsider `mattn/go-sqlite3` only if a measured compatibility/performance issue justifies CGo.
- Plain SQL migrations embedded with `//go:embed`; avoid an ORM in V1.
- Struct-based input/output schemas plus startup/contract tests against the emitted MCP JSON Schema. Use a small validation library only where Go types/tags cannot express the invariant.
- Standard `flag` or a lightweight CLI package; avoid a large command framework until command complexity demands it.
- `net/http` for a future local UI/API unless a concrete need requires a framework.
- Synchronous transaction-oriented application services; do not imitate SQLite concurrency with unnecessary goroutine fan-out.

Re-evaluate Rust only if a two-day vertical-slice spike demonstrates a decisive advantage in binary size, schema modeling, or correctness with no material contributor/release penalty. The default decision is Go; implementation should not remain blocked on another language debate.

### Interfaces

```text
MCP stdio / streamable transport          CLI                 optional local web UI
              │                            │                           │
              └──────────── application use cases ──────────────────────┘
                                           │
                         repositories + transaction boundary
                                           │
                                   SQLite (.throughline/throughline.db)
```

### Local installation concept

```bash
brew install throughline            # distribution target, not a v0.1 requirement
cd my-project
throughline init                    # creates .throughline/throughline.db + config.toml
throughline mcp                     # starts the MCP server for configured transport
throughline ui                      # later: starts/opens a local human projection
```

Suggested generated layout:

```text
.throughline/
  config.toml
  throughline.db
```

`config.toml` should contain only local configuration such as schema version, item-key prefix, UI binding, and optional database path. Do not require secrets in V1.

## 6. Domain model and invariants

### Objective, plan, and work-item fields

```text
Objective
  id / key / title / description
  desired_outcome
  phase               idea | discovery | planning | execution | evaluation | completed | paused | cancelled
  version / audit fields

Plan
  id / objective_id / title / summary
  revision
  commitment_state    draft | proposed | approved | rejected | superseded
  proposed_by / proposed_at
  resolved_by / resolved_at / resolution_reason
  version / audit fields

WorkItem
  id                  stable UUID/ULID internal ID
  key                 readable identifier, e.g. TH-42 (unique within workspace)
  objective_id        required objective
  plan_id             optional plan revision that proposed/committed the item
  parent_id           optional recursive decomposition parent
  title               concise required summary
  description         optional Markdown/plain-text context
  kind                extensible slug; built-ins below
  commitment_state    proposed | accepted | rejected | superseded
  execution_status    backlog | ready | in_progress | review | done | cancelled
  priority            low | medium | high | urgent
  estimated_scope     xs | small | medium | large | unknown
  execution_policy    human_only | agent_may_propose | approval_required | autonomous_with_report
  required_actor_kind any | human | agent
  attention_state     none | needs_human_decision | needs_human_review | needs_clarification | intervention_required
  version             monotonically incrementing optimistic-concurrency version
  created_at / updated_at
  created_by / updated_by

OutputProfile
  id / name / version / description
  lifecycle_state     draft | proposed | active | rejected | superseded
  structure_json      required shape and fields
  semantics_json      meaning and domain-neutral constraints
  validation_json     required verifier kinds and acceptance expressions
  proposed_by / proposed_at / resolved_by / resolved_at
  supersedes_id / audit fields

ExpectedOutput
  id / work_item_id / name / ordinal
  output_profile_id   exact immutable profile version
  contract_json       instance-specific narrowing; never weakens the profile
  destination_hint

OutputRevision
  id / expected_output_id / revision
  output_profile_id   copied exact contract identity for audit/query
  content_digest      optional but required where the artifact provider can calculate it
  produced_by / produced_at

ValidationRecord
  id / output_revision_id / criterion_ref
  validator_kind      structure | schema | evaluation | provenance | human_review | policy | probe | successor_use
  verdict             passed | failed | waived
  score / details / evidence artifact / verifier / timestamp

ExternalAction
  id / work_item_id / action_type
  required             true | false
  current_revision
  state               proposed | authorized | executing | succeeded | failed | rejected | cancelled | expired
  title / rationale   descriptive metadata, excluded from authorization hash
  version / audit fields

ExternalActionRevision
  external_action_id / revision
  authorization_subject_json
  authorization_subject_hash
  proposed_by / proposed_at

AuthorityGrant
  id / external_action_id / action_revision
  principal_actor_id
  authorization_subject_hash
  constraints_json / expires_at
  source_approval_id / granted_by / granted_at / revoked_at
```

Recommended built-in `kind` slugs are deliberately domain-neutral:

```text
action
deliverable
research
experiment
design
writing
skill_design
agent_design
workflow_design
tool_installation
integration
configuration
evaluation
approval
human_action
milestone
```

Do not use a database `CHECK` for `kind`; keep reserved built-ins documented while allowing namespaced extensions such as `acme:legal_review`. Decision, question, assumption, finding, and requirement remain typed context records, not overloaded work-item kinds, although a work item may be created to resolve or produce one.

Seed these initial active OutputProfile definitions during `throughline init`:

```text
structured_document/v1
research_dossier/v1
decision_record/v1
skill_package/v1
agent_definition/v1
tool_installation/v1
workflow_definition/v1
generic_artifact/v1
```

They are ordinary persisted profile rows, not branches such as `if kind == research_dossier`. Agents may freely reference active profiles. Creating a definition is governed: propose a new immutable version, review/activate it, and supersede it only with another version. A profile describes structure, semantics, validation expectations, and acceptance expressions; Throughline evaluates recorded facts against that contract but does not itself perform research, run skill evaluations, or install tools.

The four state axes are orthogonal:

- objective `phase`: where collaboration is in the overall lifecycle;
- plan/work-item `commitment_state`: proposal versus approved shared commitment;
- work-item `execution_status`: progress of accepted executable work;
- `attention_state` and `execution_policy`: intervention and authority gates.
- `required_actor_kind` and capabilities: vendor-neutral performer eligibility, not automatic assignment.
- output profile/revision/validation state: what was promised, produced, proven, and accepted;
- external-action/authority state: which exact outside effect a principal may perform and what result was observed.

This prevents a plan from looking executable merely because its proposed items have a `ready`-like status.

Execution-policy semantics:

| Policy | Claim/transition rule |
|---|---|
| `human_only` | Only an actor declared as human can claim and complete the item. |
| `agent_may_propose` | An agent may claim to produce a proposal/draft, but acceptance, publication, installation, sending, or other committed side effect requires a human/authorized approval. |
| `approval_required` | An agent may execute only after a matching approval record is active. |
| `autonomous_with_report` | An eligible agent may execute without pre-approval but must record checkpoints/results and any configured review. |

Work-item execution policy never authorizes an ExternalAction by implication. A principal must also have the required capability and a matching, unexpired AuthorityGrant for the exact current action revision. A plan or work-item approval expresses commitment; it is not a wildcard permission for every side effect discovered during execution.

### Derived facts

Do not persist facts that can go stale unless they are cached with invalidation. Calculate or query:

- `dependencies_satisfied`
- `is_blocked`
- `blocking_reasons`
- `active_claim`
- acceptance completion counts
- output-profile compatibility and validation completion
- accepted output revisions
- required reusable-output satisfaction
- current external-action authorization from subject hash + principal + constraints + expiry
- ready-work eligibility

Ready eligibility at V1 is:

```text
objective.phase = execution
AND work_item.commitment_state = accepted
AND work_item.execution_status = ready
AND no unresolved hard dependency
AND no open blocking question, constraint violation, manual blocker, or required approval
AND no active claim held by another actor
AND actor satisfies declared capability/actor-kind policy where enforceable
AND all required OutputRequirements resolve to compatible accepted OutputRevisions
```

The server identifies executable candidates; it does **not** choose the one an agent must perform. `backlog` never means ready. A separate transition or approved-plan activation promotes eligible accepted items to `ready`.

### Deterministic invariants

1. A work item cannot depend on itself; dependency insertion must reject cycles for hard prerequisite edges.
2. A hard dependency cannot be treated as satisfied until the predecessor is `done` (or explicitly waived/cancelled by a documented policy).
3. At most one unexpired active claim exists per work item in V1.
4. Claims are leases, never permanent ownership. Expired claims are reclaimable. Renewing or releasing requires the claim ID and owner identity.
5. Every mutation receives `expected_version`, except create and deliberately idempotent replay. A mismatch returns a conflict with the current summary/version; it must not silently overwrite.
6. A mutation with a reused `(actor_id, idempotency_key)` must return the recorded original result and make no second change.
7. State-changing operations create exactly one or more explicit `activity` entries in the same transaction.
8. `done` is allowed only when all required acceptance criteria are satisfied, hard dependencies are satisfied, required ExpectedOutputs are accepted/explicitly waived, required ExternalActions reached an allowed terminal result, and configured review requirements are met. Return the failed requirements rather than guessing.
9. `cancelled` requires a reason. It does not automatically mark dependents done; dependents become visibly blocked unless a human/agent changes their dependencies.
10. Progress, decisions, and activity must be concise factual summaries; no raw agent transcript or concealed reasoning.
11. Artifacts are references with type and URI. Validate URI syntax; do not fetch remote content as a side effect.
12. Proposed plans and work items cannot be claimed. Approving a plan atomically accepts its included work items but does not bypass dependency, phase, authority, or approval gates.
13. No executable work is claimable while its objective is in `idea`, `discovery`, `planning`, `evaluation`, `paused`, `completed`, or `cancelled`. Planning/research actions needed during discovery are represented by an approved plan or explicit phase policy, not by silently enabling all execution.
14. Assumptions have `untested | validating | validated | invalidated | superseded`; invalidating an assumption emits activity and marks linked plans/items for attention rather than rewriting history.
15. Decisions and approval decisions are immutable records once resolved; corrections create a superseding record. Revocation/supersession must be visible to dependent work and grants.
16. Expected outputs are satisfied only by an accepted immutable OutputRevision with all required ValidationRecords passed or explicitly waived. A file path, URL, or Artifact alone is not completion evidence.
17. `execution_policy` gates claims/transitions. In local V1, identity is trusted rather than authenticated, so policy enforcement is a deterministic workflow safeguard, not a security boundary.
18. Capability matching is declarative. Throughline may filter readiness for an actor but never launches or chooses the actor/runtime.
19. An active OutputProfile version is immutable. Any structure, semantics, validation, or acceptance change creates a proposed new version; existing OutputRevisions remain bound to the old version.
20. Instance-specific ExpectedOutput constraints may narrow but never weaken the referenced OutputProfile contract.
21. An OutputRevision is immutable. Materially changed content or artifact bindings create the next revision; validation of revision N never applies to revision N+1.
22. Human judgment is valid deterministic state only when the ValidationRecord binds the named reviewer, immutable rubric/profile criterion, exact OutputRevision, verdict/score, rationale, and evidence where required.
23. A required OutputRequirement blocks readiness until an exact or version-compatible accepted OutputRevision exists. Superseding/revoking a reused output creates attention on dependents; it does not silently rewrite history.
24. `ExternalAction` is not a WorkItem kind. It represents the boundary where planned work creates an observable effect outside the authoritative database.
25. Authorization applies to the canonical AuthorizationSubject only. Metadata such as title, rationale, progress, and UI labels is excluded; action type, target, arguments, scope, permissions, credential requirements, and declared constraints are included.
26. The same canonical serialization and hash algorithm must be used for proposal, grant, authorization check, and audit. Any authorization-relevant change creates a new action revision and makes earlier grants stale.
27. A valid AuthorityGrant matches the exact action revision/subject hash, executing principal, constraints, and lifetime. Capability without a matching grant is denied; a grant never transfers to another principal implicitly.
28. Throughline never executes an ExternalAction. An external actor/harness records `executing` and a terminal result plus evidence. `succeeded` without authorization and result evidence is invalid.
29. Local V1 must not market these checks as an isolation or security boundary because actor identity is trusted. Preserve the model so authenticated network deployments can enforce it later.

### State model

The earlier single state machine is not sufficient for discovery and agentic workflow creation. Use five coordinated lifecycles plus orthogonal attention and capability requirements.

#### Objective lifecycle

```text
idea ──► discovery ──► planning ──► execution ──► evaluation ──► completed
 │          │             │             │              │
 └──────────┴─────────────┴─────────────┴──────────────┴──► cancelled
                        any active phase ──► paused ──► prior active phase
```

Transitions are not strictly linear: evaluation may return to planning or execution; new findings may return execution to planning; completed objectives may be reopened only by an explicit operation with rationale. Entering `execution` requires an approved plan or an explicitly approved no-plan exception.

#### Plan commitment lifecycle

```text
draft ──► proposed ──► approved
  │           ├──────► rejected
  │           └──────► draft       (revision requested)
  └──────────────────► superseded  (new revision replaces it)

approved ──► superseded             (a later approved plan takes over)
```

Plan approval is a first-class human/authorized-actor decision. It separates agent reasoning from organizational commitment.

#### Output-profile lifecycle

```text
draft ──► proposed ──► active
  │           ├──────► rejected
  │           └──────► draft       (revision requested)
  └──────────────────► superseded

active ──► superseded               (a later active version replaces it)
```

Active profile versions are immutable. Approval activates vocabulary; it does not execute work. Built-in profiles are seeded as already active during initialization and otherwise obey the same read/evaluation model as user-defined profiles.

#### Work-item execution lifecycle

```text
backlog ──► ready ──► in_progress ──► review ──► done
   │           │             │              │
   └───────────┴─────────────┴──────────────┴──► cancelled

in_progress ──► ready         (abandoned/released, reason required)
review ──► in_progress        (changes requested)
```

Rules:

- Only `accepted` work in an execution-enabled objective phase can enter `ready` or be claimed.
- `claim_item` may move `ready → in_progress` atomically by default; make this explicit in the tool contract.
- A claim does not itself imply approval or completion.
- `in_progress → review` requires a concise progress checkpoint and no unresolved mandatory criterion if the policy requires it.
- `review → done` requires completion gates.
- A status transition is an operation (`transition_item`), not an arbitrary `patch_item` status field.
- `patch_item` may update safe descriptive fields, priority, objective, and attention state, but never bypass transition validation.

#### External-action lifecycle

```text
proposed ──► authorized ──► executing ──► succeeded
   │              │              └──────► failed
   │              ├─────────────────────► expired
   ├──────────────► rejected
   └──────────────► cancelled
```

`authorized` is derived from a valid grant for the current subject/principal, but the transition and audit event are explicit. V1 creates grants only from explicit approval decisions and does not infer risk. A future deterministic policy layer may create the same grant record without changing ExternalAction semantics. Changing an authorization-relevant field creates a new proposed revision and makes the prior grant stale. Work-item status remains independent: an in-progress item may wait on one action while other work continues.

#### Context-record lifecycles

```text
Question:   open ──► answered | waived
Decision:   proposed ──► accepted ──► superseded
Assumption: untested ──► validating ──► validated | invalidated ──► superseded
Approval:   requested ──► approved | rejected; approved ──► revoked
Finding:    recorded ──► superseded
Requirement/Constraint: proposed ──► accepted ──► superseded | waived
```

`Attention` is derived or explicitly requested and remains independent of these states. For example, an in-progress item can simultaneously require a human decision.

### Memory projections

The same graph must support three explainable projections:

- **Intent memory:** objective, desired outcome, requirements, constraints, success metrics, risks.
- **Decision memory:** questions, assumptions, findings/evidence, decisions, approvals, rationale.
- **Execution memory:** committed plan, work items, claims, checkpoints, expected outputs, artifacts, failures, results, and activity.
- **Authority memory:** external-action revisions, authorization subjects, principals, grants/revocations, execution results, and evidence linked back to objective/plan/work.

`get_item` and the future context compiler select from these stores; they do not synthesize facts that are absent from authoritative records.

## 7. SQLite schema sketch

This is a migration-oriented sketch, not copy/paste DDL. Keep timestamps in UTC ISO-8601 or integer milliseconds consistently; use ULIDs/UUIDs consistently. Turn on `PRAGMA foreign_keys = ON`, `journal_mode = WAL`, and a busy timeout for every connection.

```sql
CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE actors (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('human', 'agent', 'service')),
  display_name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE objectives (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  description TEXT,
  desired_outcome TEXT,
  phase TEXT NOT NULL CHECK (phase IN ('idea', 'discovery', 'planning', 'execution', 'evaluation', 'completed', 'paused', 'cancelled')),
  prior_phase TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  created_by TEXT REFERENCES actors(id),
  updated_by TEXT REFERENCES actors(id)
);

CREATE TABLE plans (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  summary TEXT,
  revision INTEGER NOT NULL,
  commitment_state TEXT NOT NULL CHECK (commitment_state IN ('draft', 'proposed', 'approved', 'rejected', 'superseded')),
  proposed_by TEXT REFERENCES actors(id),
  proposed_at TEXT,
  resolved_by TEXT REFERENCES actors(id),
  resolved_at TEXT,
  resolution_reason TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(objective_id, revision)
);

CREATE TABLE work_items (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  plan_id TEXT REFERENCES plans(id) ON DELETE SET NULL,
  parent_id TEXT REFERENCES work_items(id) ON DELETE SET NULL,
  title TEXT NOT NULL,
  description TEXT,
  kind TEXT NOT NULL,
  commitment_state TEXT NOT NULL CHECK (commitment_state IN ('proposed', 'accepted', 'rejected', 'superseded')),
  execution_status TEXT NOT NULL CHECK (execution_status IN ('backlog', 'ready', 'in_progress', 'review', 'done', 'cancelled')),
  priority TEXT NOT NULL CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
  estimated_scope TEXT NOT NULL DEFAULT 'unknown' CHECK (estimated_scope IN ('xs', 'small', 'medium', 'large', 'unknown')),
  execution_policy TEXT NOT NULL DEFAULT 'approval_required' CHECK (execution_policy IN ('human_only', 'agent_may_propose', 'approval_required', 'autonomous_with_report')),
  required_actor_kind TEXT NOT NULL DEFAULT 'any' CHECK (required_actor_kind IN ('any', 'human', 'agent')),
  attention_state TEXT NOT NULL DEFAULT 'none' CHECK (attention_state IN ('none', 'needs_human_decision', 'needs_human_review', 'needs_clarification', 'intervention_required')),
  cancellation_reason TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  created_by TEXT REFERENCES actors(id),
  updated_by TEXT REFERENCES actors(id)
);

CREATE TABLE context_records (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('requirement', 'constraint', 'assumption', 'finding', 'risk', 'success_metric')),
  title TEXT NOT NULL,
  body TEXT,
  status TEXT NOT NULL,
  confidence TEXT,
  source_uri TEXT,
  supersedes_id TEXT REFERENCES context_records(id),
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  created_by TEXT REFERENCES actors(id),
  updated_by TEXT REFERENCES actors(id)
);

CREATE TABLE output_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  version INTEGER NOT NULL CHECK (version > 0),
  description TEXT,
  lifecycle_state TEXT NOT NULL CHECK (lifecycle_state IN ('draft', 'proposed', 'active', 'rejected', 'superseded')),
  structure_json TEXT NOT NULL DEFAULT '{}',
  semantics_json TEXT NOT NULL DEFAULT '{}',
  validation_json TEXT NOT NULL DEFAULT '{}',
  built_in INTEGER NOT NULL DEFAULT 0 CHECK (built_in IN (0, 1)),
  supersedes_id TEXT REFERENCES output_profiles(id),
  proposed_by TEXT REFERENCES actors(id),
  proposed_at TEXT,
  resolved_by TEXT REFERENCES actors(id),
  resolved_at TEXT,
  resolution_reason TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(name, version)
);

CREATE TABLE expected_outputs (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  output_profile_id TEXT NOT NULL REFERENCES output_profiles(id) ON DELETE RESTRICT,
  contract_json TEXT NOT NULL DEFAULT '{}',
  destination_hint TEXT,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  waived_at TEXT,
  waived_by TEXT REFERENCES actors(id),
  waiver_reason TEXT,
  ordinal INTEGER NOT NULL,
  UNIQUE(work_item_id, ordinal)
);

CREATE TABLE output_revisions (
  id TEXT PRIMARY KEY,
  expected_output_id TEXT NOT NULL REFERENCES expected_outputs(id) ON DELETE CASCADE,
  output_profile_id TEXT NOT NULL REFERENCES output_profiles(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision > 0),
  content_digest TEXT,
  acceptance_state TEXT NOT NULL DEFAULT 'produced' CHECK (acceptance_state IN ('produced', 'accepted', 'rejected', 'superseded')),
  produced_by TEXT REFERENCES actors(id),
  produced_at TEXT NOT NULL,
  accepted_by TEXT REFERENCES actors(id),
  accepted_at TEXT,
  acceptance_reason TEXT,
  UNIQUE(expected_output_id, revision)
);

CREATE TABLE output_requirements (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  required_output_revision_id TEXT REFERENCES output_revisions(id) ON DELETE RESTRICT,
  required_profile_name TEXT,
  version_constraint TEXT,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  note TEXT,
  CHECK (
    (required_output_revision_id IS NOT NULL AND required_profile_name IS NULL)
    OR (required_output_revision_id IS NULL AND required_profile_name IS NOT NULL)
  )
);

CREATE TABLE output_validations (
  id TEXT PRIMARY KEY,
  output_revision_id TEXT NOT NULL REFERENCES output_revisions(id) ON DELETE CASCADE,
  criterion_ref TEXT,
  validator_kind TEXT NOT NULL CHECK (validator_kind IN ('structure', 'schema', 'evaluation', 'provenance', 'human_review', 'policy', 'probe', 'successor_use')),
  verdict TEXT NOT NULL CHECK (verdict IN ('passed', 'failed', 'waived')),
  score REAL,
  verifier_actor_id TEXT REFERENCES actors(id),
  evidence_artifact_id TEXT REFERENCES artifacts(id) ON DELETE RESTRICT,
  details_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE TABLE capabilities (
  slug TEXT PRIMARY KEY,
  description TEXT
);

CREATE TABLE actor_capabilities (
  actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
  capability_slug TEXT NOT NULL REFERENCES capabilities(slug) ON DELETE CASCADE,
  PRIMARY KEY(actor_id, capability_slug)
);

CREATE TABLE work_item_capabilities (
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  capability_slug TEXT NOT NULL REFERENCES capabilities(slug) ON DELETE RESTRICT,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  PRIMARY KEY(work_item_id, capability_slug)
);

CREATE TABLE acceptance_criteria (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  text TEXT NOT NULL,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  status TEXT NOT NULL CHECK (status IN ('pending', 'satisfied', 'waived')),
  satisfied_at TEXT,
  satisfied_by TEXT REFERENCES actors(id),
  UNIQUE (work_item_id, ordinal)
);

CREATE TABLE dependencies (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  depends_on_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (kind IN ('hard', 'soft', 'related')),
  note TEXT,
  created_at TEXT NOT NULL,
  created_by TEXT REFERENCES actors(id),
  CHECK (work_item_id <> depends_on_item_id),
  UNIQUE (work_item_id, depends_on_item_id, kind)
);

CREATE TABLE manual_blockers (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  reason TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active', 'resolved')),
  created_by TEXT REFERENCES actors(id),
  created_at TEXT NOT NULL,
  resolved_by TEXT REFERENCES actors(id),
  resolved_at TEXT,
  resolution TEXT
);

CREATE TABLE claims (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  actor_id TEXT NOT NULL REFERENCES actors(id),
  acquired_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  released_at TEXT,
  release_reason TEXT
);
CREATE UNIQUE INDEX one_active_claim_per_item
  ON claims(work_item_id) WHERE released_at IS NULL;

CREATE TABLE progress_entries (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  actor_id TEXT REFERENCES actors(id),
  summary TEXT NOT NULL,
  completed_json TEXT NOT NULL DEFAULT '[]',
  remaining_json TEXT NOT NULL DEFAULT '[]',
  discovered_json TEXT NOT NULL DEFAULT '[]',
  blocker_json TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE decisions (
  id TEXT PRIMARY KEY,
  objective_id TEXT REFERENCES objectives(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  decision TEXT NOT NULL,
  rationale TEXT,
  alternatives_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL CHECK (status IN ('proposed', 'accepted', 'superseded')),
  supersedes_id TEXT REFERENCES decisions(id),
  decided_by TEXT REFERENCES actors(id),
  decided_at TEXT,
  created_at TEXT NOT NULL,
  CHECK (objective_id IS NOT NULL OR work_item_id IS NOT NULL)
);

CREATE TABLE questions (
  id TEXT PRIMARY KEY,
  objective_id TEXT REFERENCES objectives(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  question TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('open', 'answered', 'waived')),
  answer TEXT,
  requires_human_attention INTEGER NOT NULL DEFAULT 0 CHECK (requires_human_attention IN (0, 1)),
  created_at TEXT NOT NULL,
  resolved_at TEXT,
  CHECK (objective_id IS NOT NULL OR work_item_id IS NOT NULL)
);

CREATE TABLE external_actions (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  action_type TEXT NOT NULL,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  title TEXT NOT NULL,
  rationale TEXT,
  current_revision INTEGER NOT NULL DEFAULT 1 CHECK (current_revision > 0),
  state TEXT NOT NULL CHECK (state IN ('proposed', 'authorized', 'executing', 'succeeded', 'failed', 'rejected', 'cancelled', 'expired')),
  version INTEGER NOT NULL DEFAULT 1,
  created_by TEXT REFERENCES actors(id),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE external_action_revisions (
  external_action_id TEXT NOT NULL REFERENCES external_actions(id) ON DELETE CASCADE,
  revision INTEGER NOT NULL CHECK (revision > 0),
  authorization_subject_json TEXT NOT NULL,
  authorization_subject_hash TEXT NOT NULL,
  proposed_by TEXT REFERENCES actors(id),
  proposed_at TEXT NOT NULL,
  PRIMARY KEY(external_action_id, revision),
  UNIQUE(external_action_id, authorization_subject_hash)
);

CREATE TABLE approvals (
  id TEXT PRIMARY KEY,
  objective_id TEXT REFERENCES objectives(id) ON DELETE CASCADE,
  plan_id TEXT REFERENCES plans(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  output_profile_id TEXT REFERENCES output_profiles(id) ON DELETE CASCADE,
  output_revision_id TEXT REFERENCES output_revisions(id) ON DELETE CASCADE,
  external_action_id TEXT REFERENCES external_actions(id) ON DELETE CASCADE,
  external_action_revision INTEGER,
  approved_for_actor_id TEXT REFERENCES actors(id),
  authorization_subject_hash TEXT,
  constraints_json TEXT NOT NULL DEFAULT '{}',
  expires_at TEXT,
  request TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('requested', 'approved', 'rejected', 'revoked')),
  requested_by TEXT REFERENCES actors(id),
  requested_at TEXT NOT NULL,
  resolved_by TEXT REFERENCES actors(id),
  resolved_at TEXT,
  rationale TEXT,
  CHECK (
    (plan_id IS NOT NULL)
    + (work_item_id IS NOT NULL)
    + (output_profile_id IS NOT NULL)
    + (output_revision_id IS NOT NULL)
    + (external_action_id IS NOT NULL) = 1
  ),
  CHECK (
    external_action_id IS NULL
    OR (
      external_action_revision IS NOT NULL
      AND approved_for_actor_id IS NOT NULL
      AND authorization_subject_hash IS NOT NULL
    )
  ),
  FOREIGN KEY (external_action_id, external_action_revision)
    REFERENCES external_action_revisions(external_action_id, revision)
);

CREATE TABLE authority_grants (
  id TEXT PRIMARY KEY,
  external_action_id TEXT NOT NULL,
  external_action_revision INTEGER NOT NULL,
  principal_actor_id TEXT NOT NULL REFERENCES actors(id),
  authorization_subject_hash TEXT NOT NULL,
  constraints_json TEXT NOT NULL DEFAULT '{}',
  source_approval_id TEXT NOT NULL REFERENCES approvals(id) ON DELETE RESTRICT,
  granted_by TEXT REFERENCES actors(id),
  granted_at TEXT NOT NULL,
  expires_at TEXT,
  revoked_at TEXT,
  revocation_reason TEXT,
  FOREIGN KEY (external_action_id, external_action_revision)
    REFERENCES external_action_revisions(external_action_id, revision)
);

CREATE TABLE artifacts (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  uri TEXT NOT NULL,
  title TEXT,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  attached_by TEXT REFERENCES actors(id),
  created_at TEXT NOT NULL,
  UNIQUE(work_item_id, uri)
);

CREATE TABLE output_revision_artifacts (
  output_revision_id TEXT NOT NULL REFERENCES output_revisions(id) ON DELETE CASCADE,
  artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE RESTRICT,
  role TEXT NOT NULL DEFAULT 'primary',
  PRIMARY KEY(output_revision_id, artifact_id)
);

CREATE TABLE external_action_executions (
  id TEXT PRIMARY KEY,
  external_action_id TEXT NOT NULL,
  external_action_revision INTEGER NOT NULL,
  principal_actor_id TEXT NOT NULL REFERENCES actors(id),
  authority_grant_id TEXT NOT NULL REFERENCES authority_grants(id) ON DELETE RESTRICT,
  state TEXT NOT NULL CHECK (state IN ('executing', 'succeeded', 'failed')),
  started_at TEXT NOT NULL,
  finished_at TEXT,
  result_json TEXT NOT NULL DEFAULT '{}',
  evidence_artifact_id TEXT REFERENCES artifacts(id) ON DELETE RESTRICT,
  FOREIGN KEY (external_action_id, external_action_revision)
    REFERENCES external_action_revisions(external_action_id, revision)
);

CREATE TABLE activity (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  entity_kind TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  actor_id TEXT REFERENCES actors(id),
  event_type TEXT NOT NULL,
  summary TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE TABLE idempotency_records (
  actor_id TEXT NOT NULL REFERENCES actors(id),
  key TEXT NOT NULL,
  operation TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (actor_id, key)
);

CREATE INDEX work_items_by_status_priority ON work_items(commitment_state, execution_status, priority, updated_at);
CREATE INDEX work_items_by_objective ON work_items(objective_id, execution_status);
CREATE INDEX plans_by_objective_state ON plans(objective_id, commitment_state, revision);
CREATE INDEX context_by_objective_kind ON context_records(objective_id, kind, status);
CREATE INDEX output_profiles_by_name_state ON output_profiles(name, lifecycle_state, version);
CREATE INDEX output_revisions_by_expected_state ON output_revisions(expected_output_id, acceptance_state, revision);
CREATE INDEX output_requirements_by_item ON output_requirements(work_item_id, required);
CREATE INDEX output_validations_by_revision ON output_validations(output_revision_id, validator_kind, verdict);
CREATE INDEX dependencies_by_dependent ON dependencies(work_item_id, kind);
CREATE INDEX dependencies_by_prerequisite ON dependencies(depends_on_item_id, kind);
CREATE INDEX blockers_by_item_status ON manual_blockers(work_item_id, status);
CREATE INDEX claims_by_expiry ON claims(work_item_id, expires_at) WHERE released_at IS NULL;
CREATE INDEX external_actions_by_item_state ON external_actions(work_item_id, state, updated_at);
CREATE INDEX authority_grants_by_action_principal ON authority_grants(external_action_id, external_action_revision, principal_actor_id, revoked_at, expires_at);
CREATE INDEX activity_by_sequence ON activity(sequence);
CREATE INDEX activity_by_item_sequence ON activity(work_item_id, sequence);
```

Implementation notes:

- The partial unique claim index prevents two non-released claims. The claim operation must first release/ignore expired records transactionally or define “active” with an `expires_at > now` check plus an update strategy. Test this carefully.
- Application code, not a SQLite `CHECK`, should detect dependency cycles using a recursive CTE within the linking transaction.
- Seed built-in OutputProfile rows transactionally during initialization/migration. The domain service must not contain profile-name conditionals.
- Define one canonical JSON representation and hash algorithm for AuthorizationSubject before implementation. Persist both canonical JSON and digest; verify both on every authorization check.
- Activating an OutputProfile, accepting an OutputRevision, approving an ExternalAction, creating/revoking an AuthorityGrant, and recording each action execution must emit append-only activity in the same transaction.
- If decision/question scope needs many-to-many linkage later, replace the nullable foreign keys with link tables; do not add polymorphic foreign keys without a reason.
- Add FTS5 only when real search needs justify its migration and indexing cost.

## 8. MCP contract conventions

### Cross-cutting contract

All tool inputs/outputs use JSON Schema (generated from runtime schemas where possible). A
multi-workspace server requires an allowlisted `workspace_id` on every call; a server with exactly
one configured workspace may supply that workspace as the default. Responses always identify the
resolved workspace. Arbitrary filesystem/database paths and mutable session-level workspace
selection are forbidden; see ADR 0009.

State changing calls carry:

```json
{
  "workspace_id": "research-project",
  "actor_id": "codex:run-2026-08-21-01",
  "idempotency_key": "unique-per-actor-operation-attempt",
  "expected_version": 7
}
```

`expected_version` applies when an existing aggregate is changed. Inputs must be strict: reject unknown enum values and invalid shapes. Avoid “success: false” as a normal response when the MCP host can represent a tool error; return structured error content consistently.

Common error shape:

```json
{
  "error": {
    "code": "version_conflict",
    "message": "TH-42 changed after version 7 was read.",
    "current": { "id": "TH-42", "version": 8, "status": "review" },
    "requirements": []
  }
}
```

Recommended codes: `workspace_required`, `workspace_not_found`, `not_found`, `validation_failed`, `version_conflict`, `claim_conflict`, `claim_expired`, `transition_not_allowed`, `objective_phase_disallows_execution`, `plan_not_approved`, `approval_required`, `approval_stale`, `capability_mismatch`, `output_profile_inactive`, `output_contract_unsatisfied`, `output_revision_unaccepted`, `external_action_not_authorized`, `authority_grant_expired`, `authority_principal_mismatch`, `authorization_subject_mismatch`, `dependency_cycle`, `blocked`, `idempotency_key_reused_with_different_request`, `forbidden` (reserved for an authenticated future layer).

Set MCP tool annotations accurately as advisory host hints: read tools `readOnlyHint: true`; mutations `readOnlyHint: false`; only declare `idempotentHint: true` where the server’s idempotency design genuinely guarantees it. Do not mistake annotations for authorization or data integrity.

### V1 tool surface

The original tools remain the execution kernel. The audits add the minimum planning/context, output-contract, and delegated-authority operations needed to make domain-neutral agentic work real.

```text
READ
board_overview
list_items
list_ready_items
get_item
get_objective_context
get_changes
list_output_profiles
get_output_profile
list_outputs

OBJECTIVE / PLAN
create_objective
patch_objective
transition_objective
propose_plan
review_plan

CONTEXT / HUMAN CONTROL
record_context
record_decision
ask_question
answer_question
request_approval
resolve_approval
request_attention
block_item
unblock_item

WORK
create_item
claim_item
renew_claim
release_item
patch_item
transition_item
append_progress

OUTPUT VOCABULARY / PRODUCTION
propose_output_profile
review_output_profile
define_expected_output
attach_artifact
create_output_revision
record_validation

RELATIONSHIPS
link_dependency
unlink_dependency

EXTERNAL ACTIONS / DELEGATED AUTHORITY
propose_external_action
revise_external_action
check_action_authorization
record_external_action_execution
```

This is more than the initial 14 tools, but not breadth for its own sake: without these additions, objectives, decisions, questions, approvals, assumptions, plan proposals, governed outputs, validation, and delegated authority exist in the schema yet cannot be managed cleanly through MCP. Where tool-count testing shows model confusion, combine mechanically similar context mutations behind strict operation enums; do not collapse distinct domain concepts. MCP is one adapter over these use cases, not the place where profiles or authority “live”; CLI and UI must call the same application services.

### Objective, plan, and context tools

#### `create_objective`, `patch_objective`, `transition_objective`

Create and evolve durable intent. An objective includes title, desired outcome, initial phase, requirements/constraints/success metrics, and audit fields. `transition_objective` validates phase gates; entering execution requires an approved plan or a recorded authorized exception.

#### `propose_plan`, `review_plan`

`propose_plan` atomically creates a versioned plan plus proposed work items, hierarchy, dependencies, profile-backed ExpectedOutputs, OutputRequirements, capability requirements, and anticipated ExternalActions. It does not make items claimable or actions authorized. `review_plan` approves, rejects, or requests revision; approval accepts included items and records the approver/rationale in the same transaction but creates no wildcard AuthorityGrant.

```json
{
  "objective_id": "OBJ-12",
  "actor_id": "agent:planner-01",
  "idempotency_key": "complaint-flow-plan-v1",
  "title": "Complaint triage workflow v1",
  "summary": "Research channels, design the skill and triage agent, install approved connectors, test escalation, and deliver an operator guide.",
  "items": [
    {
      "client_ref": "research-systems",
      "kind": "research",
      "title": "Map complaint channels and existing systems",
      "required_capabilities": ["web_research", "document_reading"],
      "expected_outputs": [{"name":"System map", "profile":"research_dossier", "profile_version":1}]
    },
    {
      "client_ref": "design-skill",
      "kind": "skill_design",
      "title": "Design the complaint-triage skill",
      "depends_on": ["research-systems"],
      "expected_outputs": [{"name":"Installable skill", "profile":"skill_package", "profile_version":1}]
    },
    {
      "client_ref": "install-tools",
      "kind": "tool_installation",
      "title": "Install and configure the approved MCP connectors",
      "execution_policy": "approval_required",
      "depends_on": ["research-systems"],
      "expected_outputs": [{"name":"Verified tool setup", "profile":"tool_installation", "profile_version":1}],
      "external_actions": [
        {
          "client_ref": "install-browser-mcp",
          "action_type": "tool.install",
          "required": true,
          "target": {"package":"browser-mcp", "version":"2.1.0"},
          "scope": {"environment":"project"},
          "permissions": ["network.read", "config.write:project"],
          "credential_requirements": []
        }
      ]
    }
  ]
}
```

#### `record_context`

Creates or supersedes one `requirement`, `constraint`, `assumption`, `finding`, `risk`, or `success_metric`. Assumptions include confidence and validation state; findings may include source/evidence references. It never accepts raw chain-of-thought.

#### `record_decision`, `ask_question`, `answer_question`

Maintain durable decision memory. Decisions include outcome, rationale, alternatives, deciding actor, and optional supersession. Questions can block work and request human attention; answers are audited and may clear linked blockers.

#### `request_approval`, `resolve_approval`, `request_attention`

Approvals target a plan, work item, OutputProfile proposal, OutputRevision, or exact ExternalAction revision. Attention is orthogonal and may point to a question, decision, review, clarification, or intervention. Approval resolution requires an actor and rationale; revocation is a new audited state change and re-blocks dependent work.

For ExternalAction approval, the request must bind `external_action_id`, `revision`, `approved_for_actor_id`, current `authorization_subject_hash`, constraints, and optional expiry. Approval atomically creates an AuthorityGrant. Revocation atomically revokes the corresponding grant. If the current hash or revision differs at resolution time, return `approval_stale`; never broaden or silently regenerate the request.

#### `block_item`, `unblock_item`

Create or resolve a manual blocker with a reason, actor, timestamp, expected version, and idempotency key. Dependency/question/approval/phase/capability blockers remain derived and cannot be “unblocked” by deleting their evidence; resolve the underlying record instead.

### Output vocabulary, production, validation, and reuse

#### `list_output_profiles`, `get_output_profile`

Read active or historical persisted profile definitions. Agents may use any active profile without a separate approval. Responses include exact name/version, lifecycle, structure, semantics, validation expectations, acceptance expressions, and supersession. Built-ins are indistinguishable from other active profiles except `built_in: true`.

#### `propose_output_profile`, `review_output_profile`

Create and govern the shared vocabulary of finished work. A proposal supplies a new immutable `(name, version)` definition with `structure`, `semantics`, `validation`, and optional predecessor. It cannot reuse an existing name/version or mutate an active row. `review_output_profile` activates, rejects, or requests a revision; activation records the decision and makes the exact version available to ExpectedOutputs.

```json
{
  "actor_id": "agent:research-designer",
  "idempotency_key": "research-dossier-v2-proposal",
  "name": "research_dossier",
  "version": 2,
  "description": "Source-linked research result with explicit uncertainty.",
  "structure": {
    "required": ["question", "method", "findings", "sources", "uncertainty", "conclusion"]
  },
  "semantics": {
    "claims_require_provenance": true
  },
  "validation": {
    "required": [
      {"kind": "structure"},
      {"kind": "provenance"},
      {"kind": "human_review", "rubric": "research-quality/v1"}
    ]
  },
  "supersedes": "research_dossier/v1"
}
```

This governed proposal/activation path prevents profile-name drift while retaining extension without Throughline code changes.

#### `define_expected_output`

Describe a deliverable before execution by referencing an exact active OutputProfile version and optional stricter instance contract. Instance constraints may narrow but never weaken profile requirements.

#### `create_output_revision`, `attach_artifact`

Bind one or more attached Artifacts to a new immutable OutputRevision. The request names the ExpectedOutput, exact profile identity, next revision number, artifact roles, and optional content digest. A material artifact/content change creates another revision; it never edits an accepted revision.

#### `record_validation`

Record an externally produced validation verdict against one exact OutputRevision and criterion. Throughline verifies the record shape and deterministically reevaluates the profile acceptance expression. It does not run the research, evaluation, shell probe, or human judgment itself. When all mandatory validations exist and pass (or are explicitly waived by authorized policy), the same transaction may mark the revision accepted and emit activity.

Supported V1 validator kinds are `structure`, `schema`, `evaluation`, `provenance`, `human_review`, `policy`, `probe`, and `successor_use`. Human review must name the reviewer and immutable rubric/criterion; probe records include the external result and evidence rather than asking Throughline to execute a command.

#### `list_outputs`

Discover bounded accepted outputs for reuse by `profile_name`, version constraint, objective, producer, or recency. It is a structured query over authoritative rows, not semantic search or a package registry. `propose_plan` and `create_item` may declare `requires_outputs` by exact revision or profile/version constraint.

### External actions and delegated authority

#### `propose_external_action`, `revise_external_action`

Create the explicit boundary object for an effect outside Throughline. `metadata` contains title/rationale and does not affect authorization. `authorization_subject` contains action type, target, arguments, scope, permissions, credential requirements, and constraints; the service canonicalizes and hashes it. `revise_external_action` creates an immutable next revision. It never mutates a subject already reviewed or executed.

```json
{
  "work_item_id": "TH-83",
  "actor_id": "agent:researcher-07",
  "idempotency_key": "install-browser-mcp-r1",
  "metadata": {
    "title": "Install browser MCP",
    "rationale": "Required by the approved research workflow"
  },
  "authorization_subject": {
    "action_type": "tool.install",
    "target": {"package": "browser-mcp", "version": "2.1.0"},
    "arguments": [],
    "scope": {"environment": "project"},
    "permissions": ["network.read", "config.write:project"],
    "credential_requirements": [],
    "constraints": {"global_install": false}
  }
}
```

Changing metadata alone does not invalidate a grant. Changing any AuthorizationSubject field creates a new hash/revision and requires a matching new grant.

#### `check_action_authorization`

Read-only deterministic query used immediately before an external harness acts. Input includes action ID/revision, principal actor ID, and subject hash. It returns `AUTHORIZED` only when the current revision/hash, principal, constraints, expiry, grant status, and declared capability requirements match. Otherwise it returns a stable denial reason such as `approval_required`, `approval_stale`, `authority_principal_mismatch`, or `authority_grant_expired` plus changed fields where safely available.

This is least-authority bookkeeping, not a security boundary in trusted-local V1. The harness remains responsible for calling it and for honoring the result until authenticated enforcement exists.

#### `record_external_action_execution`

Record `start`, `succeed`, or `fail` for one exact action revision, principal, and AuthorityGrant. `start` rechecks authorization. Terminal records include structured result and required evidence; success may reference the installed version, message ID, issue URL, deployment ID, or other observed effect. Throughline never performs the effect.

### Read tools

#### `board_overview`

**Purpose:** compact token-efficient summary before an agent selects work.

```json
// input
{ "objective_id": "optional", "include_attention": true }

// output
{
  "workspace": { "id": "local", "change_cursor": "142" },
  "objectives": { "discovery": 1, "planning": 1, "execution": 2, "evaluation": 0 },
  "plans_needing_review": 1,
  "output_profiles_needing_review": 1,
  "external_actions_needing_authority": 2,
  "counts": { "backlog": 8, "ready": 3, "in_progress": 2, "review": 1, "blocked": 2, "done": 34 },
  "ready_high_priority": [
    { "id": "TH-42", "title": "Add optimistic locking", "priority": "high", "estimated_scope": "small" }
  ],
  "needs_human_attention": [
    { "id": "TH-51", "attention_state": "needs_human_decision", "title": "Choose persistence policy" }
  ]
}
```

`blocked` is a derived overview count, not necessarily a stored status.

#### `list_items`

**Purpose:** structured retrieval, never visual-column retrieval.

Input supports `objective_id`, `objective_phase[]`, `plan_id`, `commitment_state[]`, `execution_status[]`, `priority[]`, `kind[]`, `attention_state[]`, `execution_policy[]`, `required_capability[]`, `required_output_profile[]`, `claimed_by`, `blocked`, `limit`, and `cursor`. Output returns compact summaries, an opaque pagination cursor, and `change_cursor`.

#### `list_ready_items`

**Purpose:** list executable candidate work. It does not assign work.

```json
// output item
{
  "id": "TH-42",
  "title": "Add optimistic locking",
  "kind": "action",
  "commitment_state": "accepted",
  "priority": "high",
  "execution_status": "ready",
  "execution_policy": "autonomous_with_report",
  "dependencies_satisfied": true,
  "blocked": false,
  "estimated_scope": "small",
  "version": 17
}
```

Accept actor ID so an agent’s own unexpired claim can remain visible. Exclude work claimed by another actor by default; provide `include_claimed: true` only for inspection.

#### `get_item`

**Purpose:** deep but selective context retrieval.

```json
// input
{
  "id": "TH-42",
  "include": ["description", "plan", "context", "acceptance_criteria", "expected_outputs", "output_revisions", "validations", "required_outputs", "capabilities", "external_actions", "authority_grants", "dependencies", "claims", "progress", "decisions", "questions", "approvals", "artifacts", "activity"],
  "activity_limit": 20
}
```

Return stable item fields, version, derived blockers, and only requested optional sections. Every response should state the latest workspace change cursor.

#### `get_objective_context`

**Purpose:** deterministic continuation package across sessions and vendors, including planning and non-code work.

```json
{
  "objective_id": "OBJ-12",
  "actor_id": "agent:research-02",
  "include": ["intent", "decisions", "open_questions", "approved_plan", "ready_work", "accepted_outputs", "external_actions", "authority", "recent_changes", "artifacts"],
  "max_items_per_section": 20
}
```

V1 is selection-based and size-bounded, not semantically generated: return objective phase, requirements/constraints/success metrics, accepted decisions, active assumptions with validation state, findings/evidence, open questions/approvals, approved-plan revision, actor-relevant ready/claimed work, accepted/reusable outputs, current external-action authorization summaries for that actor, recent changes, and relevant artifact metadata. A later context compiler may add token budgeting and relevance ranking, but it must remain explainable and cite source records.

#### `get_changes`

**Purpose:** delta feed to avoid rereading the entire board.

```json
// input
{ "since": "142", "limit": 100, "objective_id": "optional" }

// output
{
  "changes": [
    { "sequence": 143, "item_id": "TH-42", "event_type": "status_changed", "summary": "Moved to review", "created_at": "..." },
    { "sequence": 144, "item_id": "TH-51", "event_type": "attention_requested", "summary": "Human decision needed", "created_at": "..." }
  ],
  "next_cursor": "144",
  "has_more": false
}
```

Use an opaque monotonic activity sequence as the initial cursor. Define retention/compaction policy before any deletion exists.

### Work tools

#### `create_item`

Creates one work item under a required objective, with optional plan/parent, structured criteria, profile-backed ExpectedOutputs, OutputRequirements, capabilities, anticipated ExternalActions, and initial dependencies. Creating an already accepted/ready item requires an approved-plan context or an explicitly authorized direct-work exception; otherwise default to `proposed` + `backlog`. Validate all references, active profile versions, output constraints, and dependency cycles in a transaction. Return the full compact item and `version: 1`.

```json
{
  "actor_id": "human:dennis",
  "idempotency_key": "create-auth-locking-01",
  "title": "Add optimistic locking",
  "objective_id": "OBJ-1",
  "kind": "action",
  "commitment_state": "accepted",
  "execution_status": "ready",
  "priority": "high",
  "estimated_scope": "small",
  "execution_policy": "autonomous_with_report",
  "description": "...",
  "acceptance_criteria": [
    { "text": "Concurrent updates do not silently overwrite changes", "required": true },
    { "text": "A concurrency test exists", "required": true }
  ]
}
```

#### `claim_item`

Atomically checks objective phase, plan commitment, dependencies/blockers, required reusable outputs, actor capability/work-item policy, approvals, and claim availability; then creates a lease, records activity, and normally transitions `ready → in_progress`. Claiming work does not authorize any ExternalAction attached to it.

```json
{
  "id": "TH-42",
  "actor_id": "agent:coding-01",
  "expected_version": 17,
  "idempotency_key": "run-821-claim-wg42",
  "lease_seconds": 1800,
  "transition_to_in_progress": true
}
```

Return `claim_id`, `expires_at`, the new item version/status, and the minimal context needed to continue.

#### `renew_claim`

V1 includes explicit lease renewal. It requires item ID, claim ID, owner actor ID, expected item version, idempotency key, and bounded extension seconds. Renewal fails if the claim expired, was released, ownership differs, or policy/phase now pauses execution. It emits activity without hiding a newly introduced blocker or revoked approval.

#### `release_item`

Releases an active claim held by the actor, with a reason. It must not implicitly mark work done. It may transition `in_progress → ready` only with an explicit `return_to_ready` option and policy checks. Return latest version.

#### `patch_item`

Updates non-workflow fields: title, description, parent, priority, scope, execution policy, attention state, capability requirements, acceptance criterion statuses, and not-yet-produced ExpectedOutput instances (subject to audit). Once an OutputRevision exists, changing its ExpectedOutput contract requires a replacement ExpectedOutput or approved plan revision; never reinterpret produced work under a changed contract. Input must be an explicit patch object or field mask; never accept arbitrary columns. Every update requires `expected_version` and produces activity explaining the changed fields. Commitment, plan, objective phase, and execution status use their dedicated operations.

Changing an acceptance criterion to `satisfied` must record actor/time. Waiving a required criterion requires a rationale and should request human attention by default.

#### `transition_item`

The only ordinary way to change workflow status. Input: id, target enum, expected version, actor, idempotency key, optional reason/summary. Output: updated status/version plus transition evaluation.

```json
{
  "id": "TH-42",
  "to": "done",
  "expected_version": 19,
  "actor_id": "agent:review-01",
  "idempotency_key": "review-01-complete-42",
  "summary": "Review passed; all required criteria are satisfied."
}
```

When disallowed, return `transition_not_allowed` and structured requirements, e.g. `[{"kind":"acceptance_criterion", "id":"AC-3", "message":"Concurrency test exists is pending"}]`.

#### `append_progress`

Creates a concise checkpoint, without overwriting old progress.

```json
{
  "id": "TH-42",
  "expected_version": 18,
  "actor_id": "agent:coding-01",
  "idempotency_key": "run-821-progress-2",
  "summary": "Version-column conflict detection is implemented.",
  "completed": ["Added version column", "Detected zero-row updates as conflicts"],
  "remaining": ["Add concurrency test"],
  "discovered": ["Repository update API did not expose affected-row count"],
  "blocker": null
}
```

Decide whether any progress entry bumps work-item version. Recommended: yes, because it changes the item’s visible context; avoid requiring agents to retry unrelated text patches by returning the new version every time.

### Relationship tools

#### `link_dependency`

Creates a typed edge from `item_id` to `depends_on_item_id`, runs self/cycle checks, increments the affected item version, and emits activity. Default kind `hard`.

#### `unlink_dependency`

Deletes a specific typed edge, requires expected version of the dependent item, and emits activity.

#### `attach_artifact`

Adds a typed external reference to an item. Input includes `kind`, `uri`, optional `title`/small metadata, actor, idempotency key, expected version. Duplicates must be idempotent for the same URI/kind.

### Deliberately later tools

| Tool | Why later |
|---|---|
| `handoff_item` | Valuable dedicated transfer packet: recipient role/actor, summary, artifacts, open attention items. V1 data can be represented by progress + claim + status + activity; introduce only when it clarifies real use. |
| `dependency_graph` | SQLite recursive CTEs already enable it. Design bounded depth/cycle/error output after simple dependencies work. |
| advanced `context_projection` / `continue_work` | V1 includes deterministic `get_objective_context`. Token-budgeted relevance ranking and role-specific compilation need evidence from usage to avoid premature “AI context magic.” |
| output-profile registry / compatibility solver | V1 has persisted workspace profiles, exact versions, and a minimal constraint subset. Distribution, trust, signing, and dependency solving are separate ecosystem problems. |
| policy-generated AuthorityGrants | V1 grants come from explicit approvals. A general policy language or risk engine would broaden scope and must not be smuggled into action authorization. |
| batch updates, labels, FTS5/semantic search, auth, sync | Defer until the minimal substrate proves useful. |

## 9. Agent and human workflows

### Discovery and planning loop

1. Create an objective from the human’s desired outcome; begin in `idea` or `discovery`.
2. Record requirements, constraints, success metrics, risks, assumptions, findings, questions, and evidence as typed context—not as chat residue.
3. Let agents research, design, and propose a versioned plan with recursively decomposed work, capabilities, policies, dependencies, active OutputProfile references, expected outputs, reusable-output requirements, and anticipated ExternalActions.
4. Keep plan work `proposed`; no item is claimable yet.
5. A human or authorized actor approves, rejects, edits, or requests a revision. Approval records shared commitment.
6. Transition the objective to `execution`; only accepted, dependency-satisfied, approved work becomes ready.
7. Execute through the standard claim/checkpoint/review loop.
8. During evaluation, create immutable OutputRevisions, record validation/evidence, accept qualifying outputs, and expose them for reuse; findings can return the objective to planning or execution.
9. For each outside effect, verify the exact action revision/principal has authority immediately before the connected runtime acts, then record result evidence.
10. Complete the objective while retaining intent, decision, execution, output-lineage, and authority memory.

### First-class domain-neutral reference workflow

V1 acceptance must include an end-to-end scenario such as “build a reusable research-and-reporting agent workflow,” with work items like:

```text
Objective: Build a reusable vendor-research workflow

Discovery
  - identify trusted source requirements
  - record data/privacy constraints
  - test assumptions about available connectors
  - decide output structure and review policy

Approved plan
  - research candidate tools and MCP servers          [research]
  - design the researcher subagent role/capabilities  [agent_design]
  - author the research skill                         [skill_design]
  - install/configure approved MCPs and CLIs          [tool_installation]
  - define the report directory and schema            [workflow_design]
  - run an evaluation case                            [evaluation]
  - produce skill package, config, report, and guide   [deliverables]
  - human review/approval                              [approval]

Output contracts
  - research_dossier/v1
  - skill_package/v1
  - agent_definition/v1
  - tool_installation/v1
  - workflow_definition/v1

Authority boundary
  - ExternalAction: install browser MCP in project scope
  - ExternalAction: write approved connector configuration
  - ExternalAction: publish dossier to internal destination
```

The resulting chain must be observable as authoritative state:

```text
Objective → approved Plan → WorkItem
    ├→ ExpectedOutput → OutputRevision → ValidationRecords → accepted output
    │                                              └→ OutputRequirement of later work
    └→ ExternalAction revision → AuthorityGrant(principal + subject hash)
                                → execution result → evidence
```

The expected outputs might be an installable skill directory, an agent definition, MCP/CLI configuration, a source-linked research dossier, an evaluation result, and an operator guide. The accepted skill/output may be consumed by a later objective without copying its facts into chat. The connected runtime creates files, installs tools, delegates subagents, and performs external actions; Throughline records what should happen, what is allowed, and what actually happened but never performs the side effects.

This workflow must run in a temporary directory with no Git repository and no coding artifact types. That is a mandatory architecture/acceptance test, not a demo preference.

### Standard agent loop

Server instructions should teach this workflow concisely:

1. Call `board_overview` to orient.
2. Call `list_ready_items` to discover executable candidates.
3. Call `get_item` with only needed sections before choosing a candidate.
4. Call `claim_item` before doing shared work.
5. Inspect ExpectedOutput profiles, required reusable outputs, and any ExternalActions before acting.
6. For an outside effect, propose the exact action, obtain/verify a matching grant for this principal, and call `check_action_authorization` immediately before the harness acts.
7. Do the external work in the appropriate harness/tooling; record ExternalAction start/result/evidence without implying Throughline performed it.
8. Call `append_progress` after meaningful checkpoints, discoveries, or a blocker.
9. Attach relevant artifacts, create immutable OutputRevisions, and record required validations.
10. Update criteria and use `transition_item`; never bypass the gates.
11. Release the lease if abandoning work or after successful handoff/completion according to policy.
12. Use `get_changes` rather than rereading the full workspace after an interruption.

Planning agents use `get_objective_context`, typed context tools, and plan proposal/review operations instead of claiming unapproved execution items.

The server exposes what is actionable. The agent remains responsible for choosing what best serves the user objective.

### Human loop

The UI/CLI should make it easy to inspect:

- what completed, moved to review, became blocked, or changed since last visit;
- high-priority ready work;
- claims/agents currently working;
- work requiring human decision, review, or clarification;
- proposed OutputProfiles and exact changes from prior versions;
- produced OutputRevisions, missing/failed validations, rubrics, and accepted reusable outputs;
- ExternalActions needing authority, their exact target/scope/permissions, intended principal, constraints, expiry, and result evidence;
- the item’s criteria, dependencies, decisions, questions, artifacts, concise progress, and audit timeline.

The human surface is a control surface, not a passive viewer. It must eventually allow prioritization, explicit blocking/unblocking with reasons, criterion/output changes, OutputProfile activation/rejection, revision/rubric review, plan approval, exact ExternalAction grant/revocation, request changes, pause/release/take-over actions, and assignment to a human—each through the same domain operations and activity log used by MCP. It must never summarize authority as a vague “allow this task”; show the authorization-relevant payload and principal.

An item activity view should read like a work audit, for example:

```text
09:12  Dennis changed priority: medium → high
09:14  Coding agent claimed the item
09:18  Progress: optimistic locking implemented
09:22  Acceptance criterion satisfied: conflicts are detected
09:25  Discovery: repository API hid affected-row count
09:34  Item moved to review
09:38  Review passed; item completed
```

Never render raw model chain-of-thought. Persist only actionable, human-readable summaries and rationale.

### Optional Kanban projection

The projection changes with collaboration phase while the graph remains stable:

```text
Discovery:  Ideas | Questions | Assumptions | Findings | Decisions
Planning:   Proposed | Needs review | Approved | Rejected
Execution:  Backlog | Ready | In progress | Review | Done
Evaluation: Outputs | Validation | Findings | Needs decision | Accepted
Authority:  Proposed actions | Needs grant | Authorized | Executing | Result
```

Blocked and human attention are badges/filters or dedicated queues, not permanent columns. A card should show kind, title, priority, commitment, active claim actor, capability/policy badges, criteria/output completion, derived blockers, and attention. Item detail—not the board—holds deep context.

## 10. Recommended repository structure

```text
throughline/
  go.mod
  go.sum
  README.md
  LICENSE
  CONTRIBUTING.md
  cmd/
    throughline/
      main.go
  internal/
    domain/
      work/                  # objectives, plans, items, context, claims, dependencies
      output/                # profiles, expectations, revisions, validation, reuse
      authority/             # external actions, authorization subjects, grants
      audit/                 # activity and change semantics
    app/                     # transactional use cases across domain modules
    ports/                   # repository, clock, ID generator interfaces
    sqlite/
      migrations/
      repositories/
      queries/               # readiness, overview, deltas, context projections
      transaction.go
    mcp/
      tools/
      instructions.go
      transport.go
    cli/
      init.go
      mcp.go
      ready.go
      show.go
    config/
      workspace.go
  web/                       # optional embedded UI assets later
  tests/
    contract/
    integration/
    e2e/
    fixtures/
  docs/
    architecture.md
    domain-model.md
    protocol.md
    adr/
      0001-sqlite-is-authoritative-store.md
      0002-work-graph-not-board-model.md
      0003-concurrency-and-idempotency.md
      0004-versioned-output-contracts-and-validation.md
      0005-external-actions-and-delegated-authority.md
```

Rules:

- `internal/domain` owns business semantics and cannot import MCP, CLI, UI, or a SQLite driver.
- `internal/sqlite` implements ports and owns embedded migrations/SQL queries.
- MCP and CLI are thin adapters around use cases.
- MCP request/response structs belong at the adapter boundary and map to application commands/queries; contract tests prevent JSON Schema drift from runtime validation.
- Introduce a workspace configuration/locator module rather than scattering `.throughline` filesystem logic.

## 11. V1 delivery plan

### Milestone 0 — executable foundations

- Use Go; pin toolchain/dependencies, select the license, and configure formatting, static analysis, vulnerability checks, and tests.
- Create the repository structure and CI that runs format, lint, type checks, unit tests, integration tests.
- Implement `throughline init`, configuration parsing, database creation, migrations, SQLite pragmas, and a seeded local actor strategy.
- Write ADRs for authoritative SQLite, graph-first domain, concurrency/idempotency, immutable output contracts, and exact-payload delegated authority.
- Specify and test the canonical AuthorizationSubject serialization/hash before adapter work.
- Revalidate a competitor matrix against the actual primitives (headless/local, context records, plan approval, leases, concurrency, outputs, handoffs, context projection) and review MPAC/multi-principal coordination literature before claiming novel protocol semantics.

**Exit criterion:** `throughline init` reliably creates a valid, migration-tracked `.throughline/throughline.db`, seeds the generic active OutputProfiles as data, and can reopen it without profile-name branches in domain code.

### Milestone 1 — intent and planning vertical slice

- Objective phases and structured desired outcome.
- Requirements, constraints, assumptions, findings, success metrics, questions, decisions, and approvals.
- Versioned plan proposal/review and recursive work-item decomposition.
- Capability requirements, execution policy, OutputProfile persistence/proposal/activation, and profile-backed ExpectedOutputs.
- One repository-independent scenario: propose and approve a plan to research tools, design a skill/subagent workflow, declare installation actions, and require `research_dossier/v1` plus `skill_package/v1`.

**Exit criterion:** a new agent can recover intent/decision context, propose a domain-neutral plan and any missing OutputProfile version, obtain review, and observe that only approved work and active output vocabulary become usable.

### Milestone 2 — durable execution graph

- Work item persistence across domain-neutral kinds.
- Structured acceptance criteria.
- Hard dependency linking with cycle rejection and ready query.
- Activity timeline.
- Immutable OutputRevisions, append-only ValidationRecords, deterministic acceptance, OutputRequirements, and accepted-output discovery/reuse.
- Core use cases plus CLI inspection (`ready`, `show`) if cheap.

**Exit criterion:** a user can create dependent work, produce/validate an immutable non-code output, reuse that exact accepted revision in a second objective, and observe readiness change using the application layer/CLI.

### Milestone 3 — safe multi-actor coordination

- Actors, capabilities, lease claims, explicit renewal, expiry behavior, release behavior, claim conflicts.
- Optimistic concurrency on all work-item mutations.
- Idempotency record/replay.
- Deterministic transition gate evaluation.
- Progress and artifact attachment.
- ExternalAction revisions, canonical subject hashes, principal-bound AuthorityGrants, expiry/revocation, authorization checks, and execution evidence.

**Exit criterion:** concurrent simulated agents cannot both claim an item or silently overwrite state; an actor with capability but no matching grant cannot record an action start; payload/principal changes make prior grants invalid.

### Milestone 4 — MCP first usable release

- Implement agreed V1 tools and server instructions.
- Validate JSON schemas and define error responses.
- Support stdio transport first; add other transport only when a real host requires it.
- Provide MCP configuration examples for at least two clients.
- Add `board_overview`, deterministic `get_objective_context`, and `get_changes` cursors.
- Expose governed profile/output reuse and external-action authorization/result operations.

**Exit criterion:** two independent MCP clients can continue an approved non-code workflow, discover/claim/update/transition shared work, produce/reuse validated outputs, and reconstruct the intent/authority/evidence chain with correct deltas and conflicts.

### Milestone 5 — human projection and packaging

- Improve CLI (`overview`, `context`, `ready`, `show`, plan/profile/output review, decisions/questions/approvals, action/grant inspection).
- Build a minimal optional local Kanban UI only after the MCP workflow works.
- Document install, backups (`copy the SQLite file while safely closed or via SQLite backup`), migration, and recovery.
- Package one local installation path; Homebrew is a later distribution target unless release engineering is already ready.

**Exit criterion:** a human can understand intent, plan status, current work, output contracts/revisions/validation, and the exact principal/action/scope being authorized—and approve/revoke/pause/take over—without reading an agent transcript.

### Later discovery milestones

- `handoff_item` after confirming the minimal representation is insufficient.
- recursive `dependency_graph` with bounds and useful visualization.
- deterministic context projection with explicit inputs such as objective, role, sections, recency, and a token/size budget.
- workspace/profile registries, compatibility solvers, broad policy languages, optional FTS5, labels, sync, authenticated network mode, and plugins—only in response to demonstrated need.

## 12. Testing and verification strategy

### Unit tests (core)

- Objective-phase transitions, pause/resume/reopen rules, and execution entry gate.
- Plan draft/proposal/approval/rejection/supersession and atomic commitment of included work.
- Context-record lifecycles, especially assumption invalidation, decision supersession, approval revocation, and their downstream blockers/attention.
- Every status transition, including all invalid paths and gate messages.
- Acceptance criterion completion/waiver rules.
- OutputProfile proposal/activation/rejection/supersession and immutability of active versions.
- ExpectedOutput narrowing rules; an instance cannot weaken its profile.
- OutputRevision immutability, revision isolation, validation/waiver rules, deterministic acceptance, and reuse/version compatibility.
- ValidationRecord requirements for machine, provenance, evaluation, probe, and human-rubric verdicts.
- Ready eligibility and each blocking reason.
- Execution-policy and capability filtering under the explicit trusted-local identity model.
- AuthorizationSubject canonicalization/hash stability and exclusion of metadata fields.
- ExternalAction revision/lifecycle transitions; capability-without-grant denial; principal, subject, constraint, expiry, and revocation matching.
- Approval-to-AuthorityGrant atomic creation/revocation and stale-approval detection.
- Claim ownership, expiry, renewal/reclaim policy, and release policy.
- Idempotency replay and mismatched request detection.
- Version comparison/conflict behavior.
- Input schema validation and canonical error mapping.

### SQLite integration tests

- Migrations from empty database and from every supported previous version.
- Foreign keys, unique keys, partial active-claim constraint, rollback on failure.
- Seeded OutputProfile idempotency and absence of profile-name-dependent schema/domain behavior.
- Composite ExternalAction revision references and grant/execution audit linkage.
- Recursive CTE cycle checks/dependency traversal once implemented.
- Transactional guarantee: entity mutation, activity event, and idempotency result either all commit or all roll back.
- Two separate connections/process simulations for writer contention and `busy_timeout` behavior.
- WAL database creation/reopen and backup/recovery guidance.

### MCP contract tests

- Tool input/output conforms to generated JSON Schema.
- Tool annotations match their semantic behavior.
- Errors have stable codes and useful machine-readable details.
- Read tools do not mutate state.
- Mutating-tool retry with same idempotency key returns the original response.
- Profile proposal/review, output revision/validation, and action authorization contracts preserve exact versions and hashes.
- `check_action_authorization` is read-only and returns stable machine-readable denial reasons.
- `record_external_action_execution(start)` rechecks current authority rather than trusting a prior client read.
- `get_changes` observes a monotonic, gap-free sequence for committed events and handles pagination.
- Test with a real stdio MCP client harness, not only direct function calls.

### End-to-end scenarios

1. **Repository-independent workflow creation:** in a directory with no Git metadata, create a vague objective → record requirements/constraints/assumptions → research findings → propose skill/subagent/tool-install/output plan → approve → execute → create immutable skill/config/report/guide OutputRevisions → record provenance/evaluation/human validations → accept outputs → evaluate objective.
2. **Premature execution prevention:** an agent proposes a plan and marks an item ready; claim remains rejected until plan approval and objective execution phase.
3. **Single agent execution:** create accepted item → claim → progress → satisfy criteria → attach a document/output artifact → transition to review/done.
4. **Two agents race:** both read version 7; only one claims/patches; the other gets a conflict, calls `get_item`, and retries deliberately.
5. **Blocked chain:** A depends on B; A is absent from `list_ready_items`; B completes; A appears.
6. **Agent crash:** lease expires; a second agent can safely reclaim; the activity view makes the history legible.
7. **Human intervention:** a question creates attention; human records decision/answer; a later agent discovers it via `get_objective_context`/changes and continues.
8. **Approval revocation:** an installation/send/publish grant is revoked before action; authorization returns denied, affected work/attention explains why, and history retains the original decision.
9. **Assumption invalidation:** research invalidates an assumption; linked plan/items receive attention and the objective can return to planning without losing history.
10. **Restart:** server/client restarts and needs only IDs, versions, claim IDs, and cursors—no hidden session state.
11. **Governed vocabulary:** an agent proposes `legal_memo/v1`; another actor activates it; an ExpectedOutput can then reference it. Attempting to mutate the active version fails and `legal_memo/v2` is proposed instead.
12. **Research reuse:** accept `research_dossier/v1`; a later objective declares the exact revision as an OutputRequirement and becomes ready without duplicating the dossier in chat.
13. **Revision isolation:** revision 1 is fully validated; attaching materially changed content creates revision 2, which remains unvalidated until its own records exist.
14. **Human judgment:** a structured document is accepted through an immutable rubric and named reviewer without a test command.
15. **Safe installation:** declare an MCP installation with exact version, target, permissions, credential requirements, and rollback evidence; grant it to one principal; record externally observed success and verification evidence.
16. **Payload drift:** after approval, change package version, scope, target, permissions, or credentials; the hash changes and the old approval/grant returns `approval_stale` or `authorization_subject_mismatch`.
17. **Metadata edit:** change only action title/rationale; the AuthorizationSubject hash and valid grant remain unchanged.
18. **Principal mismatch:** agent A has capability and a grant; agent B has the same capability but cannot use A’s grant.
19. **Confused-deputy guardrail:** an agent with `message.send` capability proposes an unplanned exfiltration-like action; absent an intent-linked exact grant, authorization is denied and the attempted proposal remains auditable.
20. **Authority-chain reconstruction:** query objective → approved plan → work item → action revision → authorization subject → principal-bound grant → execution → evidence without relying on an LLM explanation.

### Quality gates

- Format, lint, strict type check, unit + integration + contract tests on every change.
- Run a small load/contention smoke test before claiming multi-agent readiness.
- Include CLI/MCP examples in docs as tested snippets.
- Do not market features as supported until their transition, concurrency, and recovery behavior are tested.

## 13. Open questions and risks

| Question / risk | Why it matters | Recommended next decision |
|---|---|---|
| Language/runtime | Drives binary packaging, MCP integration, SQLite tooling | **Resolved for V1: Go** with official MCP SDK and CGo-free SQLite. Rust is the documented fallback only if a vertical slice proves a decisive advantage. |
| License | Central to open-source USP | Select before public code: Apache-2.0 or MIT are simple permissive candidates; validate contributor/governance needs. |
| Workspace model | V1 has one database per project; multi-workspace support affects every ID/tool | Start with one workspace per database. Preserve ADR 0009's explicit per-call `workspace_id` routing contract so a later allowlisted multi-workspace server does not require hidden session state. |
| Identity/auth | Local usage may only need actor strings; shared/network mode needs trust boundaries | Keep V1 local/trusted. Do not imply access control exists. |
| Claim renewal | Leases must be practical for long work | **Resolved: explicit `renew_claim` is V1.** Specify maximum duration and heartbeat guidance before implementation. |
| Status/claim coupling | Auto-transition on claim is convenient but may surprise users | Default to atomic `ready → in_progress`; expose option and document it. |
| Review gate | “Review complete” needs a reliable representation | V1 can use status/explicit reviewer transition; add a review-record model only when policy demands it. |
| Planning/context API size | First-class non-code lifecycle adds tools beyond the original execution kernel | Keep named semantic operations for objective/plan/decision/question/approval; combine only mechanically similar typed context records after tool-use testing. |
| Output vocabulary sprawl | Unrestricted agents could create `research_report`, `research-result`, and near-duplicates | Agents freely use active profiles but creation is proposal/review/activation. Active versions are immutable; seed a small generic vocabulary. |
| Output contract language | A powerful expression language can become an unsafe workflow engine | V1 supports a deliberately small declarative structure/validation/acceptance grammar; no arbitrary code in profiles. |
| Profile compatibility | Version constraints can become a package-manager problem | Support exact version and a minimal documented constraint subset; defer compatibility solving/registries. |
| Context projection | Potential differentiator, but easy to overbuild or hallucinate relevance | V1 provides deterministic `get_objective_context`; defer token-budgeted semantic ranking and log requested sections. |
| Objective phase semantics | Discovery sometimes includes executable research while general execution must remain gated | Define a narrow phase policy: discovery may execute only explicitly approved discovery-plan items; general plan items require objective `execution`. |
| Work-item kind extensibility | Hard-coded code-centric enums would undermine the product; unlimited strings reduce consistency | Ship documented built-in slugs plus namespaced extensions; do not gate correctness on the kind value. |
| Plan approval granularity | Approving a whole plan is too coarse for side effects discovered later | Plan approval commits work only. Each V1 ExternalAction needs an exact principal-bound approval/grant; policy-generated grants are later. |
| Capability and authority trust | Actor strings are not authentication, and tool availability is not permission | Enforce deterministic workflow grants in trusted local mode but label them non-security; authenticated network mode is required for a real boundary. |
| Authorization canonicalization | Different JSON ordering/default handling could make approvals unstable or bypassable | Define one canonical representation, included-field set, hash algorithm, and cross-language fixtures in ADR 0005 before implementation. |
| Grant constraints | A broad constraint language would recreate IAM/policy-engine complexity | V1 permits explicit structured equality/bounds needed by reference scenarios; no inference and no arbitrary expressions. |
| External-action lifecycle honesty | A client could claim success without performing the effect | Require principal/grant/revision binding and evidence, retain trusted-local caveat, and never phrase stored result as independently verified unless a verifier recorded it. |
| Output validation | Non-code deliverables cannot be judged by “tests pass” | Store immutable profile criteria and explicit records; methodology stays in skills/tools, and human judgment is first-class when bound to a rubric/revision. |
| SQLite concurrency | Networked shared filesystems and many remote writers are risky | State local/trusted scope plainly. Explore a single local server process before any remote/sync promise. |
| Mutable history / privacy | Activity audit needs retention rules; agent summaries can still contain sensitive data | Make local ownership explicit, document backups, provide no telemetry by default, and add redaction/export only when needed. |
| Competitor overlap | Several projects already address portions of the thesis | Hold scope line; test whether minimal setup + deterministic shared state is independently valuable. |
| Category and project naming | “Non-code” is negative positioning and `Work Graph` is already used by another product | Position as domain-neutral agentic work/delegated intent; perform naming, package-namespace, and trademark review before publication. |
| Multi-principal semantics | MCP/A2A alone do not define shared-state governance/conflict semantics; prior research flagged MPAC as relevant | Review the primary MPAC work before designing networked permissions/governance; keep V1 local and concrete. |
| MCP evolution | MCP capabilities and transport details evolve | Pin SDK/spec compatibility; avoid reliance on hidden sessions; treat MCP tool annotations as hints. |

## 14. First coding-session checklist

1. Read this document and turn unresolved design choices into short ADRs—do not silently decide them in adapter code.
2. Scaffold the Go repository and CI baseline.
3. Implement initialization/migrations plus seeded persisted OutputProfiles; prove the domain contains no profile-name branches.
4. Implement one repository-independent vertical slice: objective → context → proposed plan → approval → accepted work → ExpectedOutput → OutputRevision → ValidationRecords → accepted/reusable output.
5. Add hard dependencies, OutputRequirements, ready query, and activity in the same thin path; prove unapproved work or missing required outputs are not claimable.
6. Specify AuthorizationSubject canonicalization/hash fixtures, then add ExternalAction revisions, exact principal-bound grants, execution results/evidence, claims/renewal, concurrency, and idempotency before exposing mutations to multiple agents.
7. Expose the agreed MCP execution, planning/context, profile/output, and delegated-authority surface. Resist unrelated workspace or orchestration features.
8. Verify with two independent simulated agents, one skill/subagent/tool-install workflow, reusable outputs across objectives, and human CLI profile/action approval/revocation flows.
9. Only then decide whether phase-aware Kanban/authority projections add enough value for the next milestone.

## 15. Definition of V1 success

Throughline's architectural destination succeeds if a person can initialize one local workspace and two different MCP-capable agents can safely carry a domain-neutral objective from exploration through approved execution, accepted/reusable output, and authorized external effects across sessions. The bounded `v0.1.0` release criteria are defined separately in Product Decision 0001:

- the objective preserves desired outcome, requirements, constraints, success metrics, assumptions, findings, decisions, questions, and approvals;
- agents can propose a plan for research, writing, workflow/skill/subagent design, tool installation/configuration, evaluation, and structured outputs without prematurely executing it;
- OutputProfiles are persisted governed vocabulary; active versions are immutable and extensible without code changes;
- produced work is represented by immutable OutputRevisions, append-only validation evidence, deterministic acceptance, and reusable OutputRequirements—not merely attached files;
- human approval converts a plan proposal into shared commitment, while each outside effect remains separately represented as an exact ExternalAction;
- capability never implies authority: a valid grant binds one principal, action revision/subject hash, constraints, and lifetime, and payload/principal drift is denied;
- Throughline records action execution results/evidence and can reconstruct why/what/who/may/did, while the connected harness performs the effect;
- each agent can efficiently find genuine ready work;
- only one agent can actively hold a work lease;
- dependencies, criteria, blockers, decisions, plans, OutputProfiles, ExpectedOutputs, OutputRevisions, validations, reusable requirements, ExternalActions, grants, artifacts, and progress survive restarts;
- stale writers get explicit conflicts rather than overwriting state;
- transitions are validated by the server, not trusted to agent prose;
- humans can see what changed, what was produced, and where attention/approval is required, and can pause, reject, release, or take over;
- at least one mandatory end-to-end workflow completes and reuses an accepted output without a repository, Git operation, code artifact, automated test, build system, CI system, or pull request;
- all of this works from a single local SQLite file without Docker, Postgres, Redis, a vendor account, or a mandatory web UI.

If a proposed feature does not make this workflow safer, clearer, or more composable, it probably belongs in a skill, harness, or later product layer—not in Throughline's core.

## 16. Full-history audit record

The source conversation was reread turn by turn, not only from its cached preview. The following decisions are represented in this brief:

| Source-conversation decision | Audit result |
|---|---|
| Expose work state, not board coordinates | Preserved in the graph-first model and projection boundary. |
| Ready work, focused context, structured criteria, explicit dependencies/blockers | Preserved in queries, schema, invariants, and tools. |
| Claims are expiring leases; concurrent writes use versions and idempotency | Preserved; explicit `renew_claim` is now V1. |
| Structured checkpoints, first-class artifacts, deltas, server instructions | Preserved. |
| Deterministic transition gates; do not trust the LLM to enforce policy | Preserved and extended to plan, phase, approval, capability, and output gates. |
| Human UI is the control surface over the same state | Strengthened with approve/block/pause/release/take-over controls and phase-aware projections. |
| Human attention is orthogonal to workflow status | Preserved. |
| Board/context is authoritative across Claude, ChatGPT, Codex, humans, and custom agents | Preserved; `get_objective_context` provides deterministic continuation. |
| Idea → exploration → plan → decomposition → execution → review → learning | Previously under-modeled; now represented by objective phase, plan commitment, execution, evaluation, and memory projections. |
| Work may be an idea, question, assumption, decision, goal, deliverable, workflow step, risk, experiment, approval, human action, or agent action | Previously incomplete; now split between typed context records, plans, domain-neutral work kinds, and approvals. |
| Agents propose plans; humans approve/edit/reject before execution | Previously missing; now a V1 plan lifecycle and tool contract. |
| Recursive decomposition without fixed project hierarchy levels | Added with `parent_id` and versioned plan work. |
| Required capabilities instead of vendor-specific assignment | Added. |
| Human/agent symmetry plus explicit authority/autonomy | Strengthened: Capability is separate from principal-bound AuthorityGrant; trusted-local security caveat remains explicit. |
| First-class non-code/domain-neutral work | Elevated from positioning to a design invariant and mandatory repository-independent end-to-end acceptance test. |
| Finished work needs a contract, revision, evidence, and reuse—not only an attachment | Added as persisted governed OutputProfiles, ExpectedOutputs, immutable OutputRevisions, append-only ValidationRecords, and OutputRequirements. |
| Output vocabulary can evolve without profile-name branches | Added: active profile versions are immutable persisted rows; agents propose and authorized actors activate new versions through shared domain services/MCP/CLI/UI. |
| Capability does not imply authority | Added as a core invariant and tested denial path. |
| External side effects are independent domain objects | Added as versioned ExternalActions rather than WorkItem kinds; Throughline records but does not execute them. |
| Approval binds the exact operation and principal | Added as canonical AuthorizationSubject hash + principal + constraints + expiry, materialized as an AuthorityGrant; payload drift invalidates authority. |
| Intent, decision, execution, output-lineage, and authority memory | Added as explicit explainable projections. |
| Context compiler/`continue_work` | Basic deterministic objective context is V1; token-budgeted/semantic compilation remains later. |
| Skills teach behavior; MCP owns durable shared state and invariants | Preserved and clarified for skill/subagent/tooling creation. |
| SQLite is the source of truth; MCP/CLI/UI are interfaces | Preserved. |
| Lean local install: `throughline init`, `throughline mcp`, `throughline ui` | Preserved. |
| Open-source, headless, vendor-neutral, no Docker/Postgres/Redis/account required | Preserved. |
| Do not become another Plane/Jira or an agent orchestrator | Preserved; modeling agentic workflow design/coordination does not authorize launching agents or executing external side effects. |

The material omissions in the first handoff version were plan proposal/approval, objective phases, assumptions/findings/requirements/constraints/success metrics, recursive decomposition, governed output vocabulary/revisions/validation/reuse, capability-versus-authority separation, exact principal-bound external actions, phase-aware human projections, and a repository-independent acceptance scenario. This revision closes those omissions while retaining the lean local execution kernel and non-orchestrator boundary.
