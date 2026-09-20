package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// runHarness is one objective with one approved two-step plan revision, in
// execution and ready to be run. Every test below starts from the same shape,
// so what each one asserts is the only thing that differs.
type runHarness struct {
	t         *testing.T
	ctx       context.Context
	service   *app.Service
	path      string
	objective work.Objective
	plan      work.Plan
}

func newRunHarness(t *testing.T, name string, inputs []app.ProposedPlanInput, maxConcurrentRuns int) *runHarness {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), name)
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})
	harness := &runHarness{t: t, ctx: ctx, service: service, path: path}
	for _, actor := range []app.RegisterActorCommand{
		{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-owner"},
		{Actor: work.Actor{ID: "agent:one", Kind: work.ActorTypeAgent, DisplayName: "Agent One"}, IdempotencyKey: "register-one"},
		{Actor: work.Actor{ID: "agent:two", Kind: work.ActorTypeAgent, DisplayName: "Agent Two"}, IdempotencyKey: "register-two"},
	} {
		if _, err := app.UnwrapMutation(service.RegisterActor(ctx, actor)); err != nil {
			t.Fatal(err)
		}
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "create-run-objective", Key: "OBJ-RUNS",
		Title: "Run an approved revision repeatedly", DesiredOutcome: "Every run is separate and auditable.",
		Phase: work.ObjectivePlanning, MaxConcurrentRuns: maxConcurrentRuns,
	}))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.UnwrapMutation(service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: objective.ID, ActorID: "agent:one", IdempotencyKey: "propose-run-plan",
		Title: "Two-step reusable plan", Revision: 1, Inputs: inputs,
		Steps: []app.ProposedPlanStep{
			{
				ClientRef: "first", Key: "RUN-FIRST", Title: "First step", Kind: "research", Required: true,
				Priority: work.PriorityHigh, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
				AcceptanceCriteria: []app.ProposedAcceptanceCriterion{{Text: "The first step produced something checkable.", Required: true, Ordinal: 1}},
			},
			{
				ClientRef: "second", Key: "RUN-SECOND", Title: "Second step", Kind: "research", Required: false,
				Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
				DependsOn: []string{"first"},
			},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ReviewPlan(ctx, app.ReviewPlanCommand{
		PlanID: plan.Plan.ID, ReviewerActorID: "human:owner", IdempotencyKey: "approve-run-plan",
		Decision: work.PlanApproved, Reason: "The definition is complete.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.TransitionObjective(ctx, app.TransitionObjectiveCommand{
		ObjectiveID: objective.ID, TargetPhase: work.ObjectiveExecution, ActorID: "human:owner",
		IdempotencyKey: "execute-run-objective", Reason: "Run the approved revision.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}
	harness.objective = objective
	harness.plan = plan.Plan
	return harness
}

func (h *runHarness) createRun(runKey, idempotencyKey, actorID string, bindings ...app.RunInputBindingCommand) (ports.PlanRunContext, error) {
	h.t.Helper()
	return app.UnwrapMutation(h.service.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: h.objective.ID, PlanID: h.plan.ID, ActorID: actorID,
		IdempotencyKey: idempotencyKey, RunKey: runKey, Bindings: bindings,
	}))
}

// TestPlanRunMaterializesEveryStepOnce covers the positive creation path: one
// active run, one fresh work item per step with its own child data, and the
// plan's step prerequisites reproduced between this run's own items.
func TestPlanRunMaterializesEveryStepOnce(t *testing.T) {
	h := newRunHarness(t, "materialize.db", nil, 0)
	run, err := h.createRun("first-run", "create-first", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	if run.Run.Status != work.PlanRunActive || run.Run.Sequence != 1 {
		t.Fatalf("run = %#v", run.Run)
	}
	if len(run.WorkItems) != 2 {
		t.Fatalf("materialized %d work items, want 2: %#v", len(run.WorkItems), run.WorkItems)
	}
	first := run.WorkItems[0]
	second := run.WorkItems[1]
	if first.Key != "RUN-FIRST/1" || second.Key != "RUN-SECOND/1" {
		t.Fatalf("materialized keys = %q, %q", first.Key, second.Key)
	}
	for _, item := range run.WorkItems {
		if item.Origin != work.OriginPlanStep || item.PlanRunID != run.Run.ID || item.OriginPlanStepID == "" {
			t.Fatalf("materialized item lost its provenance: %#v", item)
		}
		if item.CommitmentState != work.ItemAccepted || item.ExecutionStatus != work.StatusBacklog {
			t.Fatalf("materialized item state = %#v", item)
		}
	}
	firstContext, err := h.service.GetWorkItem(h.ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstContext.AcceptanceCriteria) != 1 || firstContext.AcceptanceCriteria[0].Status != work.AcceptancePending {
		t.Fatalf("copied acceptance criteria = %#v", firstContext.AcceptanceCriteria)
	}
	secondContext, err := h.service.GetWorkItem(h.ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondContext.Dependencies) != 1 || secondContext.Dependencies[0].DependsOnItemID != first.ID {
		t.Fatalf("the run did not reproduce the step prerequisite: %#v", secondContext.Dependencies)
	}
}

// TestFailedPlanRunCreationStoresNeitherRunNorWorkItems is the rollback path.
// A creation that fails anywhere must leave nothing behind, so the next attempt
// starts from a clean objective rather than from a half-materialized run.
func TestFailedPlanRunCreationStoresNeitherRunNorWorkItems(t *testing.T) {
	h := newRunHarness(t, "rollback.db", []app.ProposedPlanInput{{Name: "window", Required: true, Ordinal: 1}}, 0)
	if _, err := h.createRun("doomed", "create-doomed", "agent:one"); err == nil {
		t.Fatal("a run missing its required binding was created")
	}
	if _, err := h.createRun("doomed-2", "create-doomed-2", "agent:one", app.RunInputBindingCommand{
		Name: "window", OutputRevisionID: "00000000-0000-7000-8000-000000000000",
	}); err == nil {
		t.Fatal("a run binding an output revision that does not exist was created")
	}
	items, err := h.service.ListWorkItems(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.WorkItem.ObjectiveID == h.objective.ID {
			t.Fatalf("a failed run creation left work item %s behind", item.WorkItem.Key)
		}
	}
	// The run key is free again, which is only true if nothing was stored.
	run, err := h.createRun("doomed", "create-recovered", "agent:one", app.RunInputBindingCommand{Name: "window", Value: "2026-Q1"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Run.Sequence != 1 {
		t.Fatalf("a failed creation consumed a run sequence: %#v", run.Run)
	}
}

// TestRunKeyReplayIsActorIndependentAndPrecedesCapacity covers the
// deduplication contract: the key names the execution instance, not the
// caller's retry, so a different actor replaying it finds the same run rather
// than being refused for capacity that run itself occupies.
func TestRunKeyReplayIsActorIndependentAndPrecedesCapacity(t *testing.T) {
	h := newRunHarness(t, "replay.db", []app.ProposedPlanInput{{Name: "window", Required: true, Ordinal: 1}}, 0)
	binding := app.RunInputBindingCommand{Name: "window", Value: "2026-Q1"}
	first, err := h.createRun("shared-key", "create-by-one", "agent:one", binding)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := h.createRun("shared-key", "create-by-two", "agent:two", binding)
	if err != nil {
		t.Fatalf("a different actor replaying the run key was refused: %v", err)
	}
	if replayed.Run.ID != first.Run.ID || len(replayed.WorkItems) != len(first.WorkItems) {
		t.Fatalf("replay returned a different run: %#v", replayed.Run)
	}
	var conflict app.RunConflictError
	if _, err := h.createRun("shared-key", "create-conflicting", "agent:two", app.RunInputBindingCommand{Name: "window", Value: "2026-Q2"}); !errors.As(err, &conflict) {
		t.Fatalf("the same key with different bindings = %v, want a run conflict", err)
	}
	if replayed.Run.Sequence != 1 {
		t.Fatalf("a replay created a second run: %#v", replayed.Run)
	}
}

// TestObjectiveCapsConcurrentPlanRuns covers the safe default and a
// deliberately raised limit. The cap counts active runs across revisions,
// which is why closing one frees the slot.
func TestObjectiveCapsConcurrentPlanRuns(t *testing.T) {
	h := newRunHarness(t, "capacity.db", nil, 0)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	var capacity app.RunCapacityError
	if _, err := h.createRun("run-2", "create-2", "agent:one"); !errors.As(err, &capacity) {
		t.Fatalf("a second concurrent run under the default limit = %v, want a capacity refusal", err)
	}
	if capacity.Limit != 1 || capacity.Active != 1 {
		t.Fatalf("capacity refusal = %#v", capacity)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: first.Run.ID, ActorID: "agent:one", IdempotencyKey: "cancel-1", ExpectedVersion: first.Run.Version,
		TargetStatus: work.PlanRunCancelled, Reason: "Freeing the slot deliberately.",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.createRun("run-2", "create-2-again", "agent:one"); err != nil {
		t.Fatalf("closing a run did not free its capacity: %v", err)
	}
}

func TestARaisedLimitPermitsExactlyThatManyActiveRuns(t *testing.T) {
	h := newRunHarness(t, "capacity-two.db", nil, 2)
	if _, err := h.createRun("run-1", "create-1", "agent:one"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.createRun("run-2", "create-2", "agent:one"); err != nil {
		t.Fatalf("the second run under a limit of two was refused: %v", err)
	}
	var capacity app.RunCapacityError
	if _, err := h.createRun("run-3", "create-3", "agent:one"); !errors.As(err, &capacity) || capacity.Limit != 2 {
		t.Fatalf("the third run under a limit of two = %v", err)
	}
}

// TestLaterRunLeavesTheEarlierRunUntouched is the separateness invariant: two
// runs of the same approved revision share no identity, and the first run's
// state is byte-identical before and after the second exists.
func TestLaterRunLeavesTheEarlierRunUntouched(t *testing.T) {
	h := newRunHarness(t, "separate.db", nil, 2)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	before, err := h.service.GetPlanRun(h.ctx, first.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.createRun("run-2", "create-2", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.ID == first.Run.ID || second.Run.Sequence != 2 {
		t.Fatalf("second run = %#v", second.Run)
	}
	identities := make(map[string]bool, len(first.WorkItems))
	for _, item := range first.WorkItems {
		identities[item.ID] = true
	}
	for _, item := range second.WorkItems {
		if identities[item.ID] {
			t.Fatalf("the two runs share work item %s", item.ID)
		}
	}
	after, err := h.service.GetPlanRun(h.ctx, first.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Run, before.Run) || len(after.WorkItems) != len(before.WorkItems) {
		t.Fatalf("the first run changed when the second was created:\n before %#v\n after  %#v", before.Run, after.Run)
	}
	for index, item := range after.WorkItems {
		if !reflect.DeepEqual(item, before.WorkItems[index]) {
			t.Fatalf("first run work item %d changed:\n before %#v\n after  %#v", index, before.WorkItems[index], item)
		}
	}
}

// TestRunStateSurvivesDatabaseReopen is the durability claim, checked against a
// second process's view rather than the one that wrote it.
func TestRunStateSurvivesDatabaseReopen(t *testing.T) {
	h := newRunHarness(t, "reopen.db", []app.ProposedPlanInput{{Name: "window", Required: true, Ordinal: 1}}, 0)
	created, err := h.createRun("run-1", "create-1", "agent:one", app.RunInputBindingCommand{
		Name: "window", Locator: "https://example.invalid/archive", SourceVersion: "v7",
	})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(h.ctx, h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(h.ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(reopened.Store(), &planningIDs{}, &planningClock{})
	recovered, err := service.GetPlanRun(h.ctx, created.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovered.Run, created.Run) || len(recovered.WorkItems) != len(created.WorkItems) {
		t.Fatalf("run did not survive reopen:\n written  %#v\n recovered %#v", created.Run, recovered.Run)
	}
	if len(recovered.Bindings) != 1 || recovered.Bindings[0].Kind != work.BindingExternal || recovered.Bindings[0].SourceVersion != "v7" {
		t.Fatalf("bindings did not survive reopen: %#v", recovered.Bindings)
	}
}

// TestExternalBindingsNeedImmutableIdentity pins the rule that a bare locator
// cannot say what a run worked from. Throughline validates the shape and never
// fetches the source.
func TestExternalBindingsNeedImmutableIdentity(t *testing.T) {
	h := newRunHarness(t, "binding-identity.db", []app.ProposedPlanInput{{Name: "window", Required: true, Ordinal: 1}}, 0)
	if _, err := h.createRun("bare", "create-bare", "agent:one", app.RunInputBindingCommand{
		Name: "window", Locator: "https://example.invalid/archive",
	}); err == nil {
		t.Fatal("a locator without a source version or digest was accepted")
	}
	if _, err := h.createRun("undeclared", "create-undeclared", "agent:one", app.RunInputBindingCommand{
		Name: "not-declared", Value: "x",
	}); err == nil {
		t.Fatal("a binding for an input the plan does not declare was accepted")
	}
	if _, err := h.createRun("digest", "create-digest", "agent:one", app.RunInputBindingCommand{
		Name: "window", Locator: "file:///archive.md", Digest: "sha256:abc",
	}); err != nil {
		t.Fatalf("a locator with a content digest was refused: %v", err)
	}
}

// TestRunOwnedWorkExecutesOnlyWhileItsRunIsActive is the gate. It is the one
// rule that makes a closed run mean something: its work stops being executable
// the moment the run ends.
func TestRunOwnedWorkExecutesOnlyWhileItsRunIsActive(t *testing.T) {
	h := newRunHarness(t, "gate.db", nil, 0)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	first := run.WorkItems[0]
	ready, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: first.ID, TargetStatus: work.StatusReady, ActorID: "agent:one",
		Reason: "Queue the first step.", ExpectedVersion: first.Version, IdempotencyKey: "ready-first",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "cancel-run", ExpectedVersion: run.Run.Version,
		TargetStatus: work.PlanRunCancelled, Reason: "Abandoned before any work started.",
	})); err != nil {
		t.Fatal(err)
	}
	// Closing the run cancelled the item, so its version moved; re-read it, or
	// the claim would be refused for staleness before the gate is consulted.
	_ = ready
	settled, err := h.service.GetWorkItem(h.ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	var claimGate app.ClaimGateError
	_, err = app.UnwrapMutation(h.service.ClaimWorkItem(h.ctx, app.ClaimWorkItemCommand{
		WorkItemID: first.ID, ActorID: "agent:one", ExpectedVersion: settled.WorkItem.Version,
		LeaseDuration: time.Hour, IdempotencyKey: "claim-after-close",
	}))
	if !errors.As(err, &claimGate) {
		t.Fatalf("claiming work of a cancelled run = %v, want a claim gate refusal", err)
	}
	if !containsClaimRequirement(claimGate.Requirements, work.ClaimRequirementRunActive) {
		t.Fatalf("claim refusal did not name the run gate: %#v", claimGate.Requirements)
	}
}

// TestWorkProposedOutsideARunIsNotExecutable covers the other half of the
// gate: create_item still records an idea, and the idea stays an idea until
// someone takes it into a run.
func TestWorkProposedOutsideARunIsNotExecutable(t *testing.T) {
	h := newRunHarness(t, "unplanned.db", nil, 0)
	created, err := h.service.CreateWorkItem(h.ctx, app.CreateWorkItemCommand{
		// Attaching it to the approved revision is the old bypass: before plan
		// runs, this alone made an item executable. The run gate is what now
		// refuses it.
		ActorID: "agent:one", IdempotencyKey: "create-unplanned", Key: "RUN-IDEA", ObjectiveID: h.objective.ID,
		PlanID: h.plan.ID, Title: "An idea recorded mid-flight", Kind: "research", CommitmentState: work.ItemAccepted,
		ExecutionStatus: work.StatusReady, Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
		ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent, AttentionState: work.AttentionNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	item := created.Result
	if item.Origin != work.OriginUnplanned {
		t.Fatalf("directly created item origin = %q, want unplanned", item.Origin)
	}
	// It cannot be claimed and it cannot be advanced, however it was recorded:
	// the refusal names the run gate rather than some incidental precondition.
	var claimGate app.ClaimGateError
	_, err = app.UnwrapMutation(h.service.ClaimWorkItem(h.ctx, app.ClaimWorkItemCommand{
		WorkItemID: item.ID, ActorID: "agent:one", ExpectedVersion: item.Version,
		LeaseDuration: time.Hour, IdempotencyKey: "claim-unplanned",
	}))
	if !errors.As(err, &claimGate) || !containsClaimRequirement(claimGate.Requirements, work.ClaimRequirementRunActive) {
		t.Fatalf("claiming an unplanned proposal = %v, %#v", err, claimGate.Requirements)
	}
	var gate app.TransitionGateError
	if _, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: item.ID, TargetStatus: work.StatusInProgress, ActorID: "agent:one",
		Reason: "Try to execute an unplanned proposal.", ExpectedVersion: item.Version, IdempotencyKey: "start-unplanned",
	})); !errors.As(err, &gate) || !containsTransitionRequirement(gate.Requirements, work.TransitionRequirementRunActive) {
		t.Fatalf("advancing an unplanned proposal = %v, %#v", err, gate.Requirements)
	}
	// Withdrawing it stays possible: cancelling is not execution, and a
	// proposal nobody wants must not be stuck forever.
	if _, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: item.ID, TargetStatus: work.StatusCancelled, ActorID: "agent:one",
		Reason: "The idea was not worth taking into a run.", ExpectedVersion: item.Version, IdempotencyKey: "cancel-unplanned",
	})); err != nil {
		t.Fatalf("cancelling an unplanned proposal was refused: %v", err)
	}
}

// TestSucceedingARunRequiresItsObligations covers the success gate and the
// fact that an optional step may be skipped while a required one may not.
func TestSucceedingARunRequiresItsObligations(t *testing.T) {
	h := newRunHarness(t, "succeed.db", nil, 0)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "premature-success",
		ExpectedVersion: run.Run.Version, TargetStatus: work.PlanRunSucceeded,
	})); err == nil {
		t.Fatal("a run with unfinished required work was allowed to succeed")
	}
	first := carryWorkItemToDone(t, h, run.WorkItems[0], "first")
	_ = first
	// The optional second step is cancelled with a rationale rather than done.
	second := run.WorkItems[1]
	if _, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: second.ID, TargetStatus: work.StatusCancelled, ActorID: "agent:one",
		Reason: "The optional step was not needed this time.", ExpectedVersion: second.Version, IdempotencyKey: "cancel-second",
	})); err != nil {
		t.Fatal(err)
	}
	current, err := h.service.GetPlanRun(h.ctx, run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	succeeded, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "succeed",
		ExpectedVersion: current.Run.Version, TargetStatus: work.PlanRunSucceeded,
	}))
	if err != nil {
		t.Fatalf("a run whose required work is done and whose optional work is cancelled could not succeed: %v", err)
	}
	if succeeded.Run.Status != work.PlanRunSucceeded {
		t.Fatalf("closed run = %#v", succeeded.Run)
	}
	// A terminal run is irreversible.
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "close-again",
		ExpectedVersion: succeeded.Run.Version, TargetStatus: work.PlanRunFailed, Reason: "Reconsidered.",
	})); err == nil {
		t.Fatal("a terminal run was closed a second time")
	}
}

// TestClosingARunCancelsItsRemainderAndReleasesItsClaims is the atomic
// settlement: a terminal run may not leave work that still looks executable or
// a lease nobody can release.
func TestClosingARunCancelsItsRemainderAndReleasesItsClaims(t *testing.T) {
	h := newRunHarness(t, "settle.db", nil, 0)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	first := run.WorkItems[0]
	ready, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: first.ID, TargetStatus: work.StatusReady, ActorID: "agent:one",
		Reason: "Queue the first step.", ExpectedVersion: first.Version, IdempotencyKey: "ready-first",
	}))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := app.UnwrapMutation(h.service.ClaimWorkItem(h.ctx, app.ClaimWorkItemCommand{
		WorkItemID: first.ID, ActorID: "agent:one", ExpectedVersion: ready.Version,
		LeaseDuration: time.Hour, IdempotencyKey: "claim-first",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "human:owner", IdempotencyKey: "fail-run",
		ExpectedVersion: run.Run.Version, TargetStatus: work.PlanRunFailed, Reason: "The source archive was withdrawn.",
	})); err != nil {
		t.Fatal(err)
	}
	settled, err := h.service.GetPlanRun(h.ctx, run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range settled.WorkItems {
		if item.ExecutionStatus != work.StatusCancelled {
			t.Fatalf("a failed run left %s in %q", item.Key, item.ExecutionStatus)
		}
	}
	context, err := h.service.GetWorkItem(h.ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range context.Claims {
		if candidate.ID == claimed.Claim.ID && candidate.ReleasedAt.IsZero() {
			t.Fatalf("a failed run left claim %s open", candidate.ID)
		}
	}
}

// TestClosingARunRequiresARationaleAndNeverMovesItsObjective pins two separate
// promises that are easy to break together: a failed or cancelled run must say
// why, and ending a run is not a statement about the objective.
func TestClosingARunRequiresARationaleAndNeverMovesItsObjective(t *testing.T) {
	h := newRunHarness(t, "rationale.db", nil, 0)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "fail-without-reason",
		ExpectedVersion: run.Run.Version, TargetStatus: work.PlanRunFailed,
	})); err == nil {
		t.Fatal("a run was failed without a rationale")
	}
	before, err := h.service.ResolveObjective(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "cancel-with-reason",
		ExpectedVersion: run.Run.Version, TargetStatus: work.PlanRunCancelled, Reason: "Superseded by a corrected revision.",
	})); err != nil {
		t.Fatal(err)
	}
	after, err := h.service.ResolveObjective(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Phase != before.Phase || after.Version != before.Version {
		t.Fatalf("closing a run moved its objective: before %#v, after %#v", before, after)
	}
}

// TestAdministrativeCancellationWorksOutsideExecution is the deliberately
// narrow exception: a paused objective must still be able to give back the
// capacity and the leases an abandoned run is holding.
func TestAdministrativeCancellationWorksOutsideExecution(t *testing.T) {
	h := newRunHarness(t, "administrative.db", nil, 0)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	objective, err := h.service.ResolveObjective(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.TransitionObjective(h.ctx, app.TransitionObjectiveCommand{
		ObjectiveID: h.objective.ID, TargetPhase: work.ObjectivePaused, ActorID: "human:owner",
		IdempotencyKey: "pause", Reason: "Paused while the source is unavailable.", ExpectedVersion: objective.Version,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "human:owner", IdempotencyKey: "succeed-while-paused",
		ExpectedVersion: run.Run.Version, TargetStatus: work.PlanRunSucceeded,
	})); err == nil {
		t.Fatal("a paused objective's run was declared successful")
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "human:owner", IdempotencyKey: "cancel-while-paused",
		ExpectedVersion: run.Run.Version, TargetStatus: work.PlanRunCancelled, Reason: "Reclaiming the capacity while paused.",
	})); err != nil {
		t.Fatalf("administrative cancellation outside execution was refused: %v", err)
	}
}

// TestOnlyAnApprovedRevisionOfThisObjectiveCanBeRun covers the two selection
// checks that keep a run bound to something a person actually approved.
func TestOnlyAnApprovedRevisionOfThisObjectiveCanBeRun(t *testing.T) {
	h := newRunHarness(t, "selection.db", nil, 0)
	draft, err := app.UnwrapMutation(h.service.ProposePlan(h.ctx, app.ProposePlanCommand{
		ObjectiveID: h.objective.ID, ActorID: "agent:one", IdempotencyKey: "propose-unapproved",
		Title: "An unreviewed revision", Revision: 2,
		Steps: []app.ProposedPlanStep{{
			ClientRef: "only", Key: "RUN-DRAFT", Title: "Draft step", Kind: "research", Required: true,
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
			ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: h.objective.ID, PlanID: draft.Plan.ID, ActorID: "agent:one",
		IdempotencyKey: "run-unapproved", RunKey: "draft-1",
	})); err == nil {
		t.Fatal("an unapproved revision was instantiated")
	}
	other, err := app.UnwrapMutation(h.service.CreateObjective(h.ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "create-other-objective", Key: "OBJ-OTHER",
		Title: "Another objective", DesiredOutcome: "Never runs someone else's plan.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: other.ID, PlanID: h.plan.ID, ActorID: "agent:one",
		IdempotencyKey: "run-foreign", RunKey: "foreign-1",
	})); err == nil {
		t.Fatal("an objective ran another objective's plan revision")
	}
}

// carryWorkItemToDone takes one materialized item through the ordinary
// execution path, so a closure test asserts against work that really was done.
func carryWorkItemToDone(t *testing.T, h *runHarness, item work.WorkItem, label string) work.WorkItem {
	t.Helper()
	current, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: item.ID, TargetStatus: work.StatusReady, ActorID: "agent:one",
		Reason: "Queue " + label + ".", ExpectedVersion: item.Version, IdempotencyKey: "ready-" + label,
	}))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := app.UnwrapMutation(h.service.ClaimWorkItem(h.ctx, app.ClaimWorkItemCommand{
		WorkItemID: item.ID, ActorID: "agent:one", ExpectedVersion: current.Version,
		LeaseDuration: time.Hour, IdempotencyKey: "claim-" + label, TransitionToInProgress: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	current = claimed.WorkItem
	itemContext, err := h.service.GetWorkItem(h.ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for index, criterion := range itemContext.AcceptanceCriteria {
		if _, err := app.UnwrapMutation(h.service.ResolveAcceptanceCriterion(h.ctx, app.ResolveAcceptanceCriterionCommand{
			CriterionID: criterion.ID, Status: work.AcceptanceSatisfied, ActorID: "agent:one",
			Rationale: "The step produced what it promised.", ExpectedWorkItemVersion: current.Version,
			IdempotencyKey: "resolve-" + label + "-" + string(rune('a'+index)),
		})); err != nil {
			t.Fatal(err)
		}
		current.Version++
	}
	for _, target := range []work.ExecutionStatus{work.StatusReview, work.StatusDone} {
		current, err = app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
			WorkItemID: item.ID, TargetStatus: target, ActorID: "agent:one",
			Reason: "Advance " + label + ".", ExpectedVersion: current.Version, IdempotencyKey: string(target) + "-" + label,
		}))
		if err != nil {
			t.Fatal(err)
		}
	}
	return current
}

func containsClaimRequirement(requirements []work.ClaimRequirement, code work.ClaimRequirementCode) bool {
	for _, requirement := range requirements {
		if requirement.Code == code {
			return true
		}
	}
	return false
}

func containsTransitionRequirement(requirements []work.TransitionRequirement, code work.TransitionRequirementCode) bool {
	for _, requirement := range requirements {
		if requirement.Code == code {
			return true
		}
	}
	return false
}

// TestMigration0018PreservesExistingWorkAsLegacyExecution is the migration
// promise. Work that predates plan runs keeps every field it had and stays
// executable, and the migration invents nothing to explain it: no run, no run
// key, no binding and no plan step is synthesized on its behalf.
func TestMigration0018PreservesExistingWorkAsLegacyExecution(t *testing.T) {
	ctx := context.Background()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	before := -1
	for index, migration := range migrations {
		if migration.version == 18 {
			before = index
		}
	}
	if before < 0 {
		t.Fatal("the reusable plan runs migration is missing")
	}
	database, err := Open(ctx, filepath.Join(t.TempDir(), "legacy-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:before] {
		if err := database.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	const fixtureTime = "2026-08-21T15:00:00.000000000Z"
	for _, statement := range []string{
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, updated_by, version, created_at, updated_at)
		 VALUES ('objective-legacy', 'OBJ-LEGACY', 'Legacy objective', '', 'Work recorded before plan runs.', 'execution', 'human:owner', 2, '` + fixtureTime + `', '` + fixtureTime + `')`,
		`INSERT INTO plans (id, objective_id, title, summary, revision, commitment_state, version, created_at, updated_at)
		 VALUES ('plan-legacy', 'objective-legacy', 'Legacy plan', '', 1, 'approved', 2, '` + fixtureTime + `', '` + fixtureTime + `')`,
		`INSERT INTO work_items (id, key, objective_id, plan_id, title, description, kind, commitment_state, execution_status,
		   priority, estimated_scope, execution_policy, required_actor_kind, attention_state, version, created_at, updated_at)
		 VALUES ('item-legacy', 'TH-LEGACY', 'objective-legacy', 'plan-legacy', 'Legacy work', '', 'research', 'accepted', 'ready',
		   'medium', 'small', 'autonomous_with_report', 'agent', 'none', 3, '` + fixtureTime + `', '` + fixtureTime + `')`,
		`INSERT INTO actors (id, kind, display_name, version, created_at) VALUES ('agent:legacy', 'agent', 'Legacy agent', 1, '` + fixtureTime + `')`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	for _, probe := range []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM plan_runs", 0},
		{"SELECT COUNT(*) FROM run_input_bindings", 0},
		{"SELECT COUNT(*) FROM plan_steps", 0},
		{"SELECT COUNT(*) FROM plan_inputs", 0},
		{"SELECT COUNT(*) FROM work_items WHERE origin = 'legacy'", 1},
		{"SELECT COUNT(*) FROM work_items WHERE plan_run_id IS NOT NULL OR origin_plan_step_id IS NOT NULL", 0},
	} {
		var got int
		if err := database.db.QueryRowContext(ctx, probe.query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != probe.want {
			t.Fatalf("%s = %d, want %d — the migration invented state it cannot know", probe.query, got, probe.want)
		}
	}

	// The objective defaults to the safe values rather than to nothing.
	var mode string
	var limit int
	if err := database.db.QueryRowContext(ctx, "SELECT mode, max_concurrent_runs FROM objectives WHERE id = 'objective-legacy'").Scan(&mode, &limit); err != nil {
		t.Fatal(err)
	}
	if mode != string(work.ObjectiveFinite) || limit != 1 {
		t.Fatalf("migrated objective mode = %q, max concurrent runs = %d", mode, limit)
	}

	// And legacy work keeps the behaviour it had: it has no run, and the run
	// gate does not hold it.
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})
	claimed, err := app.UnwrapMutation(service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{
		WorkItemID: "item-legacy", ActorID: "agent:legacy", ExpectedVersion: 3,
		LeaseDuration: time.Hour, IdempotencyKey: "claim-legacy",
	}))
	if err != nil {
		t.Fatalf("migrated legacy work became unexecutable: %v", err)
	}
	if claimed.WorkItem.Origin != work.OriginLegacy || claimed.WorkItem.PlanRunID != "" {
		t.Fatalf("migrated legacy work item = %#v", claimed.WorkItem)
	}
}
