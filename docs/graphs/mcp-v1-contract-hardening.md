# MCP V1 contract hardening

Frozen execution graph for the remaining hardening work. The intentionally dirty
worktree from `783a340` is the baseline; no reset, discard, or commit is part of
this graph.

```text
N0 Plan/contract [agent, fan-in owner]
  -> N1 Attention targets [agent]
  -> N3 Contract audit [agent, read-only]
N1 + N3 (all owned-file reports valid) -> N4 Fan-in [agent]
N4 -> N4a Item-composite semantics [agent]
N4 -> N4b Runtime-schema/idempotency audit [agent, read-only]
N4a + N4b -> N5 Verify [command]
N5 exit 0 for all commands -> N6 Standards + Spec review [agents]
N5 non-zero -> N7 Fix [agent] -> N5 (budget 3; then report)
N6 zero findings -> handoff
N6 findings -> N7 Fix -> N5 -> N6 (budget 5; repeated finding or spent budget -> human gate)
```

N1 owns `internal/app/service.go`, `internal/mcp/server.go`,
`internal/app/service_test.go`, and `internal/mcp/server_test.go`. N3 and N4b
are read-only. N2, the mutation replay matrix, follows N1 and is folded into
the fan-in because it shares `internal/mcp/server_test.go`.

The normal `.agents/` scratch directory cannot be created in this sandbox, so
node state is carried in agent results and recorded in this graph's delivery
annotation.

## Delivery annotation

- N1 completed durable, target-aware attention associations.
- N2 completed the omitted-mutation replay/mismatch MCP matrix.
- N4a completed transactional compound item creation; N5a completed safe
  compound item patching.
- N5 verification passed: formatting, vet, normal and CGo-free builds, full
  tests, targeted MCP tests, and the real two-client stdio smoke.
- Five review passes ran. The fifth Standards and Spec passes both reported
  `CLEAN` after their findings were fixed and reverified.

## Feedback

The dirty baseline made global ownership checks unsuitable, so node ownership
was validated from explicit file scopes and focused checks. The replay matrix
needed a reusable scenario fixture; treating that as a first-class node exposed
an output-schema validator defect that the narrow smoke had not exercised.
