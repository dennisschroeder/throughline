# Reusable plan runs — acceptance scenarios

The twenty scenarios `OBJ-REUSABLE-PLAN-RUNS` is judged against, each traced to the accepted decision
it comes from and to the test that runs it.

## Why this document exists

The objective's final slice requires "the complete twenty acceptance scenarios in the approved
reusable-plan-runs specification". No such specification exists as a retrievable artifact: it is not
in this repository, and the objective carries no context record holding it — only twenty-nine
accepted decisions, which are where its content actually lives.

These scenarios are therefore reconstructed from those decisions rather than copied from a document.
Each one names the decision it derives from, so the reconstruction can be checked against the record
rather than trusted. If Dennis has a specification in mind that differs from this, the difference
will be visible here rather than buried in an assertion that twenty scenarios passed.

## The scenarios

| # | Scenario | Decision | Test |
|---|---|---|---|
| 1 | An approved revision can be instantiated any number of times; each instantiation is a separate run with its own work. | `01a085d3-d3bd` every approved revision is instantiable | `TestPlanRunMaterializesEveryStepOnce`, `TestLaterRunLeavesTheEarlierRunUntouched` |
| 2 | Creating a run materializes one fresh work item per plan step, with its own criteria, outputs, requirements, capabilities and actions. | `01a085d3-d3bf` runs create fresh work items | `TestPlanRunMaterializesEveryStepOnce` |
| 3 | A run is created active, with every required binding supplied; there is no preparatory state and no separate activation. | `01a0920a-e44a` runs come into being atomically active | `TestNewPlanRunIsActiveImmediately`, `TestFailedPlanRunCreationStoresNeitherRunNorWorkItems` |
| 4 | A creation that fails any check stores neither the run nor any work item, and consumes neither the run key nor a sequence number. | `01a0920a-e44a` atomic creation | `TestFailedPlanRunCreationStoresNeitherRunNorWorkItems` |
| 5 | The same run key, revision and bindings return the existing run, whichever actor asks and across a process restart. | `01a08690-5dad` the harness supplies an opaque run key; `01a08fac-7e37` replay precedes default resolution | `TestRunKeyReplayIsActorIndependentAndPrecedesCapacity`, `TestARunKeyReplaysAcrossAProcessRestart` |
| 6 | The same run key with a different revision or different bindings is a conflict, and the refusal changes nothing. | `01a08690-5dad` conflicting content conflicts | `TestRunKeyReplayIsActorIndependentAndPrecedesCapacity`, `TestARunKeyReplaysAcrossAProcessRestart` |
| 7 | An objective permits only as many active runs as it says, counted across every revision; replay is resolved before capacity. | `01a085e9-a2f4` objectives limit concurrent runs safely | `TestObjectiveCapsConcurrentPlanRuns`, `TestARaisedLimitPermitsExactlyThatManyActiveRuns`, `TestCapacityCountsActiveRunsAcrossRevisions` |
| 8 | Two concurrent creations cannot exceed the limit, and the loser is refused by a rule rather than by a lock. | `01a085e9-a2f4` Throughline limits concurrency atomically | `TestConcurrentCreationCannotExceedTheLimit` |
| 9 | A second run of the same revision shares no identity with the first, and the first is unchanged afterwards and after reopen. | `01a085d3-d3bb` every run stays permanently separate | `TestASecondRunSharesNoIdentityWithTheFirst` |
| 10 | Work belonging to a run can be claimed and advanced only while that run is active. | `01a08fab-9594` the active run gates every new execution | `TestRunOwnedWorkExecutesOnlyWhileItsRunIsActive` |
| 11 | Work recorded outside a run is never executable, and stays retractable. | `01a08fac-f3d0` unplanned work outside a run is not executable | `TestWorkProposedOutsideARunIsNotExecutable` |
| 12 | Starting an external effect, and checking whether one is authorized, require an active run; an effect already under way may still record its result. | `01a08fab-9594` the active run gates external action start | `TestAClosedRunRefusesNewEffectsButKeepsRecordingStartedOnes` |
| 13 | A run ends only when asked, as succeeded, failed or cancelled; success requires every required step done, every remaining item terminal, and all output and action obligations met. | `01a0866c-3962` runs end only explicitly; `01a0920a-e44f` the status carries the result | `TestSucceedingARunRequiresItsObligations`, `TestClosePlanRunEnforcesItsTargets` |
| 14 | Failing or cancelling a run requires a rationale, cancels its remaining work and releases its claims in the same transaction, and is irreversible. | `01a08b2d-a30e` terminal runs close their open work atomically; `01a086d5-0f71` cancelled and failed are different | `TestClosingARunCancelsItsRemainderAndReleasesItsClaims`, `TestClosingARunSettlesALapsedClaim` |
| 15 | Cancelling a run never moves its objective, and cancelling is available outside objective execution. | `01a08a4f-28bd` closing a run never changes the objective; `01a08fab-9594` administrative cancellation | `TestClosingARunRequiresARationaleAndNeverMovesItsObjective`, `TestAdministrativeCancellationWorksOutsideExecution` |
| 16 | Approving a later revision retires no earlier one; every approved revision stays instantiable and no run is rebound. | `01a085d3-d3c2` durable change means a new revision; `01a0867d-3cda` run experience returns only through review | `TestApprovingALaterRevisionLeavesTheEarlierOneRunnable` |
| 17 | An agent can add executable work inside an active run; it has no plan step, ends with the run, and changes the definition not at all. | `01a08612-34c1` plans stay agent-led frames | `TestRunLocalWorkExecutesInsideItsRunOnly` |
| 18 | A required step is a success obligation; an optional step may be cancelled with a rationale, releasing only its own local obligations, and an effect that already started still owes its result. | `01a08669-3e74` every step is materialized; `01a0920a-e452` success aggregates only work-item obligations | `TestCancellingAnOptionalStepReleasesOnlyItsOwnObligations`, `TestCancellingARequiredStepDoesNotReleaseTheRun`, `TestAStartedEffectIsNotReleasedByCancellingItsStep` |
| 19 | A run reaches an earlier run's result only by binding one exact accepted output revision; bindings are immutable and external references carry their own identity. | `01a085f2-fda6` cross-run inputs bind exact outputs; `01a08b2c-8ca3` external inputs need immutable identity | `TestARunBindsAnEarlierRunsAcceptedOutput`, `TestARunCannotBindAnUnacceptedOrForeignOutput`, `TestExternalBindingsNeedImmutableIdentity` |
| 20 | Work that predates plan runs keeps its data and behaviour as marked legacy execution; the migration synthesizes no run, key, binding, step or activity, and reusing an old plan needs a newly reviewed revision. | `01a0920a-e454` legacy plans are not reconstructed as definitions | `TestMigration0018PreservesExistingWorkAsLegacyExecution`, `TestALegacyPlanCannotBeRunWithoutANewRevision` |

## What none of them do

No scenario asks Throughline to schedule anything, to evaluate a condition or choose a branch, to
select or launch an agent, to perform an external effect, to resolve a latest or previous run, or to
move an objective's phase on its own. Each of those is a recorded non-goal, and
`TestTheObjectiveDoesNoneOfItsNonGoals` asserts the negative directly rather than leaving it to the
absence of a feature.
