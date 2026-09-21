package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// approveSecondRevision proposes and approves another revision of the
// harness's objective, so a test can ask what having two approved definitions
// actually does.
func approveSecondRevision(t *testing.T, h *runHarness, key string) work.Plan {
	t.Helper()
	proposed, err := app.UnwrapMutation(h.service.ProposePlan(h.ctx, app.ProposePlanCommand{
		ObjectiveID: h.objective.ID, ActorID: "agent:one", IdempotencyKey: "propose-" + key,
		Title: "A later revision", Revision: 2,
		Steps: []app.ProposedPlanStep{{
			ClientRef: "only", Key: key, Title: "The later definition's only step", Kind: "research", Required: true,
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
			ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ReviewPlan(h.ctx, app.ReviewPlanCommand{
		PlanID: proposed.Plan.ID, ReviewerActorID: "human:owner", IdempotencyKey: "approve-" + key,
		Decision: work.PlanApproved, Reason: "The later definition stands on its own.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}
	return proposed.Plan
}

// TestApprovingALaterRevisionLeavesTheEarlierOneRunnable is the heart of
// revision evolution: a new definition is something more that may be run, not
// a replacement that retires what runs are already using.
func TestApprovingALaterRevisionLeavesTheEarlierOneRunnable(t *testing.T) {
	h := newRunHarness(t, "revision-evolution.db", nil, 3)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	item := first.WorkItems[0]
	ready, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: item.ID, TargetStatus: work.StatusReady, ActorID: "agent:one",
		Reason: "Queue the first step.", ExpectedVersion: item.Version, IdempotencyKey: "ready-first",
	}))
	if err != nil {
		t.Fatal(err)
	}

	later := approveSecondRevision(t, h, "EVO-LATER")

	// The earlier revision is still approved, and its in-flight run is
	// untouched — including its work still being claimable, which is what
	// automatic supersession used to break.
	recovered, err := h.service.GetObjectiveContext(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[int]work.PlanCommitment{}
	for _, candidate := range recovered.Plans {
		states[candidate.Plan.Revision] = candidate.Plan.CommitmentState
	}
	if states[1] != work.PlanApproved || states[2] != work.PlanApproved {
		t.Fatalf("plan states after approving a later revision = %#v", states)
	}
	if _, err := app.UnwrapMutation(h.service.ClaimWorkItem(h.ctx, app.ClaimWorkItemCommand{
		WorkItemID: item.ID, ActorID: "agent:one", ExpectedVersion: ready.Version,
		LeaseDuration: time.Hour, IdempotencyKey: "claim-first",
	})); err != nil {
		t.Fatalf("approving a later revision made an in-flight run's work unclaimable: %v", err)
	}
	unchanged, err := h.service.GetPlanRun(h.ctx, first.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Run.PlanID != h.plan.ID {
		t.Fatalf("the run was rebound to another revision: %#v", unchanged.Run)
	}

	// Both revisions can be instantiated, each binding the one it names.
	fromEarlier, err := h.createRun("run-earlier", "create-earlier", "agent:one")
	if err != nil {
		t.Fatalf("the earlier revision stopped being instantiable: %v", err)
	}
	fromLater, err := app.UnwrapMutation(h.service.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: h.objective.ID, PlanID: later.ID, ActorID: "agent:one",
		IdempotencyKey: "create-later", RunKey: "run-later",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if fromEarlier.Run.PlanID != h.plan.ID || fromLater.Run.PlanID != later.ID {
		t.Fatalf("a run did not bind the revision it named: %s and %s", fromEarlier.Run.PlanID, fromLater.Run.PlanID)
	}
	if len(fromEarlier.WorkItems) != 2 || len(fromLater.WorkItems) != 1 {
		t.Fatalf("the two revisions materialized %d and %d items, want 2 and 1 — each its own definition",
			len(fromEarlier.WorkItems), len(fromLater.WorkItems))
	}
}

// TestARevisionCanCiteTheRunThatProducedIt covers the one way a run's
// experience reaches a plan: as provenance on a draft somebody still has to
// approve. No observation changes a plan by itself.
func TestARevisionCanCiteTheRunThatProducedIt(t *testing.T) {
	h := newRunHarness(t, "revision-provenance.db", nil, 2)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	proposed, err := app.UnwrapMutation(h.service.ProposePlan(h.ctx, app.ProposePlanCommand{
		ObjectiveID: h.objective.ID, ActorID: "agent:one", IdempotencyKey: "propose-derived",
		Title: "What the run taught us", Revision: 2, DerivedFromPlanRunID: run.Run.ID,
		Steps: []app.ProposedPlanStep{{
			ClientRef: "only", Key: "EVO-DERIVED", Title: "A step the run showed was needed", Kind: "research", Required: true,
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
			ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if proposed.Plan.DerivedFromPlanRunID != run.Run.ID {
		t.Fatalf("the revision lost its provenance: %#v", proposed.Plan)
	}
	// Proposing it changed nothing: it is a draft, and the run that prompted
	// it still executes the definition it was created from.
	if proposed.Plan.CommitmentState != work.PlanProposed {
		t.Fatalf("a derived revision was not a draft: %q", proposed.Plan.CommitmentState)
	}
	after, err := h.service.GetPlanRun(h.ctx, run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Run.PlanID != h.plan.ID || after.Run.Status != work.PlanRunActive {
		t.Fatalf("proposing a derived revision changed the run it cites: %#v", after.Run)
	}
	if _, err := app.UnwrapMutation(h.service.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: h.objective.ID, PlanID: proposed.Plan.ID, ActorID: "agent:one",
		IdempotencyKey: "run-derived", RunKey: "derived-1",
	})); err == nil {
		t.Fatal("an unapproved derived revision was instantiated")
	}
	// A revision may only cite a run of its own objective.
	other, err := app.UnwrapMutation(h.service.CreateObjective(h.ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "create-other-objective", Key: "OBJ-EVO-OTHER",
		Title: "Another objective", DesiredOutcome: "Cannot borrow someone else's run.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ProposePlan(h.ctx, app.ProposePlanCommand{
		ObjectiveID: other.ID, ActorID: "agent:one", IdempotencyKey: "propose-foreign-provenance",
		Title: "Citing another objective's run", Revision: 1, DerivedFromPlanRunID: run.Run.ID,
		Steps: []app.ProposedPlanStep{{
			ClientRef: "only", Key: "EVO-FOREIGN", Title: "A step", Kind: "research", Required: true,
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
			ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		}},
	})); err == nil {
		t.Fatal("a revision cited a plan run belonging to another objective")
	}
}

// TestRunLocalWorkExecutesInsideItsRunOnly is the adaptation half: an agent
// that finds something the definition did not anticipate can add executable
// work to the run it is in, and only there.
func TestRunLocalWorkExecutesInsideItsRunOnly(t *testing.T) {
	h := newRunHarness(t, "run-local-work.db", nil, 2)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	created, err := h.service.CreateWorkItem(h.ctx, app.CreateWorkItemCommand{
		ActorID: "agent:one", IdempotencyKey: "create-run-local", Key: "RUN-LOCAL-1",
		ObjectiveID: h.objective.ID, PlanRunID: run.Run.ID,
		Title: "Something the definition did not anticipate", Kind: "research",
		CommitmentState: work.ItemAccepted, ExecutionStatus: work.StatusReady,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
		ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		AttentionState: work.AttentionNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	local := created.Result
	if local.Origin != work.OriginRunLocal || local.PlanRunID != run.Run.ID || local.OriginPlanStepID != "" {
		t.Fatalf("run-local item = %#v", local)
	}
	if local.PlanID != h.plan.ID {
		t.Fatalf("run-local work did not inherit its run's revision: %#v", local)
	}
	// It executes under the run's gates like the run's other work.
	if _, err := app.UnwrapMutation(h.service.ClaimWorkItem(h.ctx, app.ClaimWorkItemCommand{
		WorkItemID: local.ID, ActorID: "agent:one", ExpectedVersion: local.Version,
		LeaseDuration: time.Hour, IdempotencyKey: "claim-run-local",
	})); err != nil {
		t.Fatalf("run-local work was not executable inside its active run: %v", err)
	}
	// It is listed with its run, and the plan definition is untouched.
	scoped, err := h.service.GetObjectiveContext(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range scoped.Plans {
		if candidate.Plan.ID == h.plan.ID && len(candidate.Steps) != 2 {
			t.Fatalf("adding run-local work changed the plan definition: %d steps", len(candidate.Steps))
		}
	}
	for _, summary := range scoped.PlanRuns {
		if summary.Run.ID == run.Run.ID && summary.WorkItems != 3 {
			t.Fatalf("the run reports %d work items, want its two steps plus the run-local one", summary.WorkItems)
		}
	}

	// It cannot be added to a run that has ended, nor to one in another
	// objective, and it dies with its run.
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "human:owner", IdempotencyKey: "cancel-run",
		ExpectedVersion: run.Run.Version, TargetStatus: work.PlanRunCancelled, Reason: "Abandoned.",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.CreateWorkItem(h.ctx, app.CreateWorkItemCommand{
		ActorID: "agent:one", IdempotencyKey: "create-run-local-after", Key: "RUN-LOCAL-2",
		ObjectiveID: h.objective.ID, PlanRunID: run.Run.ID, Title: "Too late", Kind: "research",
		CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
		ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		AttentionState: work.AttentionNone,
	}); err == nil {
		t.Fatal("work was added to a run that had already ended")
	}
	settled, err := h.service.GetWorkItem(h.ctx, local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.WorkItem.ExecutionStatus != work.StatusCancelled {
		t.Fatalf("run-local work outlived its run: %q", settled.WorkItem.ExecutionStatus)
	}
}

// TestCancellingAnOptionalStepReleasesOnlyItsOwnObligations is the precise
// scope of skipping a step: its own criteria, outputs and requirements stop
// counting, and nothing else does.
func TestCancellingAnOptionalStepReleasesOnlyItsOwnObligations(t *testing.T) {
	h := newRunHarness(t, "optional-step.db", nil, 0)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	items := runItemsByStepKey(run)
	required := items["RUN-FIRST"]
	optional := items["RUN-SECOND"]

	// Give the optional step an unmet obligation of its own.
	defined, err := app.UnwrapMutation(h.service.DefineExpectedOutput(h.ctx, app.DefineExpectedOutputCommand{
		WorkItemID: optional.ID, ActorID: "agent:one", Name: "Something the optional step would produce",
		ProfileName: "research_dossier", ProfileVersion: 1, Ordinal: 1, Required: true,
		ExpectedVersion: optional.Version, IdempotencyKey: "define-optional-output",
	}))
	if err != nil {
		t.Fatal(err)
	}
	_ = defined

	carryWorkItemToDone(t, h, required, "required")
	current, err := h.service.GetWorkItem(h.ctx, optional.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: optional.ID, TargetStatus: work.StatusCancelled, ActorID: "agent:one",
		Reason:          "The optional step was not needed with these inputs.",
		ExpectedVersion: current.WorkItem.Version, IdempotencyKey: "cancel-optional",
	})); err != nil {
		t.Fatal(err)
	}
	reread, err := h.service.GetPlanRun(h.ctx, run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "succeed",
		ExpectedVersion: reread.Run.Version, TargetStatus: work.PlanRunSucceeded,
	})); err != nil {
		t.Fatalf("a cancelled optional step's own unmet output still blocked the run: %v", err)
	}
}

// TestCancellingARequiredStepDoesNotReleaseTheRun is the other side: a step
// the definition marks required is an obligation of the run, and dropping it
// is not a way to declare success.
func TestCancellingARequiredStepDoesNotReleaseTheRun(t *testing.T) {
	h := newRunHarness(t, "required-step.db", nil, 0)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	items := runItemsByStepKey(run)
	for label, item := range map[string]work.WorkItem{"required": items["RUN-FIRST"], "optional": items["RUN-SECOND"]} {
		current, err := h.service.GetWorkItem(h.ctx, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
			WorkItemID: item.ID, TargetStatus: work.StatusCancelled, ActorID: "agent:one",
			Reason: "Dropping the " + label + " step.", ExpectedVersion: current.WorkItem.Version,
			IdempotencyKey: "cancel-" + label,
		})); err != nil {
			t.Fatal(err)
		}
	}
	reread, err := h.service.GetPlanRun(h.ctx, run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "succeed",
		ExpectedVersion: reread.Run.Version, TargetStatus: work.PlanRunSucceeded,
	}))
	if err == nil {
		t.Fatal("a run succeeded with its required step cancelled rather than done")
	}
	if !strings.Contains(err.Error(), "required plan step") {
		t.Fatalf("the refusal does not name the required step obligation: %v", err)
	}
	// Cancelling the run itself is still available, with a rationale.
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "fail",
		ExpectedVersion: reread.Run.Version, TargetStatus: work.PlanRunFailed,
		Reason: "The required step could not be done with these inputs.",
	})); err != nil {
		t.Fatal(err)
	}
}

// TestAStartedEffectIsNotReleasedByCancellingItsStep draws the line the
// decision draws: cancelling a step releases the actions that never started,
// not the one that already happened in the world.
func TestAStartedEffectIsNotReleasedByCancellingItsStep(t *testing.T) {
	h := newRunHarness(t, "started-effect.db", nil, 0)
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	items := runItemsByStepKey(run)
	// The optional step depends on the required one, so the required step is
	// finished before the optional step's effect can start at all.
	carryWorkItemToDone(t, h, items["RUN-FIRST"], "required")
	optional, err := h.service.GetWorkItem(h.ctx, items["RUN-SECOND"].ID)
	if err != nil {
		t.Fatal(err)
	}
	started := beginEffectOn(t, h, optional.WorkItem, "optional")

	current, err := h.service.GetWorkItem(h.ctx, optional.WorkItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: optional.WorkItem.ID, TargetStatus: work.StatusCancelled, ActorID: "agent:one",
		Reason: "Dropping the optional step mid-flight.", ExpectedVersion: current.WorkItem.Version,
		IdempotencyKey: "cancel-optional",
	})); err != nil {
		t.Fatal(err)
	}
	reread, err := h.service.GetPlanRun(h.ctx, run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "succeed-too-early",
		ExpectedVersion: reread.Run.Version, TargetStatus: work.PlanRunSucceeded,
	})); err == nil {
		t.Fatal("a run succeeded while an effect it had authorized was still under way")
	}

	// Recording what the effect actually did releases the run.
	if _, err := app.UnwrapMutation(h.service.CompleteExternalActionExecution(h.ctx, app.CompleteExternalActionExecutionCommand{
		ExecutionID: started.execution, ActorID: "agent:one", ExpectedActionVersion: started.actionVersion,
		IdempotencyKey: "complete-optional", State: authority.ExecutionSucceeded,
		Result: json.RawMessage(`{"status":"published"}`), EvidenceArtifactID: started.artifact,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "succeed",
		ExpectedVersion: reread.Run.Version, TargetStatus: work.PlanRunSucceeded,
	})); err != nil {
		t.Fatalf("recording the effect's result did not release the run: %v", err)
	}
}

// TestARunBindsAnEarlierRunsAcceptedOutput is the cross-run input: the only
// way a later run reaches an earlier one's result, and it names the exact
// revision rather than asking for the latest of anything.
func TestARunBindsAnEarlierRunsAcceptedOutput(t *testing.T) {
	h := newRunHarness(t, "cross-run-input.db", []app.ProposedPlanInput{{Name: "prior", Required: true, Ordinal: 1}}, 3)
	producing, err := h.createRun("run-1", "create-1", "agent:one",
		app.RunInputBindingCommand{Name: "prior", Value: "bootstrap"})
	if err != nil {
		t.Fatal(err)
	}
	accepted := acceptAnOutputOn(t, h, producing.WorkItems[0], "producer")

	consuming, err := h.createRun("run-2", "create-2", "agent:one",
		app.RunInputBindingCommand{Name: "prior", OutputRevisionID: accepted})
	if err != nil {
		t.Fatalf("a later run could not bind an earlier run's accepted output: %v", err)
	}
	if len(consuming.Bindings) != 1 || consuming.Bindings[0].Kind != work.BindingOutputRevision ||
		consuming.Bindings[0].OutputRevisionID != accepted {
		t.Fatalf("cross-run binding = %#v", consuming.Bindings)
	}
	if consuming.Run.ID == producing.Run.ID {
		t.Fatal("the consuming run is the producing run")
	}
	// The binding is immutable, and there is no way to ask for "the previous
	// run's output" — the caller named a revision.
	if _, err := h.database.db.ExecContext(h.ctx,
		"UPDATE run_input_bindings SET output_revision_id = NULL WHERE plan_run_id = ?", consuming.Run.ID); err == nil {
		t.Fatal("a run input binding was edited after its run was created")
	}
}

// startedEffect carries the identifiers a test needs to finish an effect it
// deliberately left under way.
type startedEffect struct {
	execution     string
	actionVersion int
	artifact      string
}

func beginEffectOn(t *testing.T, h *runHarness, item work.WorkItem, label string) startedEffect {
	t.Helper()
	ready, err := app.UnwrapMutation(h.service.TransitionWorkItem(h.ctx, app.TransitionWorkItemCommand{
		WorkItemID: item.ID, TargetStatus: work.StatusReady, ActorID: "agent:one",
		Reason: "Queue " + label + ".", ExpectedVersion: item.Version, IdempotencyKey: "ready-effect-" + label,
	}))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := app.UnwrapMutation(h.service.ClaimWorkItem(h.ctx, app.ClaimWorkItemCommand{
		WorkItemID: item.ID, ActorID: "agent:one", ExpectedVersion: ready.Version,
		LeaseDuration: time.Hour, IdempotencyKey: "claim-effect-" + label, TransitionToInProgress: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := app.UnwrapMutation(h.service.AttachArtifact(h.ctx, app.AttachArtifactCommand{
		WorkItemID: item.ID, ActorID: "agent:one", ExpectedVersion: claimed.WorkItem.Version,
		IdempotencyKey: "artifact-effect-" + label, Kind: "document", URI: "throughline://effects/" + label, Title: "Evidence",
	}))
	if err != nil {
		t.Fatal(err)
	}
	proposed, err := app.UnwrapMutation(h.service.ProposeExternalAction(h.ctx, app.ProposeExternalActionCommand{
		WorkItemID: item.ID, ActorID: "agent:one", ExpectedVersion: artifact.WorkItem.Version,
		IdempotencyKey: "propose-effect-" + label, Required: true, Title: "Publish from " + label,
		Rationale: "An effect that starts before the step is dropped.",
		Subject:   json.RawMessage(`{"action_type":"knowledge.publish","target":{"collection":"runs"},"arguments":[],"scope":{},"permissions":["knowledge.write"],"credential_requirements":[],"constraints":{}}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	requested, err := app.UnwrapMutation(h.service.RequestExternalActionApproval(h.ctx, app.RequestExternalActionApprovalCommand{
		ActionID: proposed.Action.ID, ActorID: "agent:one", ExpectedActionVersion: proposed.Action.Version,
		ExpectedSubjectHash: proposed.Revision.AuthorizationSubjectHash, IdempotencyKey: "request-effect-" + label,
		ApprovedForActorID: "agent:one", Constraints: json.RawMessage(`{}`), Request: "Authorize it.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := app.UnwrapMutation(h.service.ResolveExternalActionApproval(h.ctx, app.ResolveExternalActionApprovalCommand{
		ApprovalID: requested.ID, ActorID: "human:owner", ExpectedActionVersion: proposed.Action.Version,
		IdempotencyKey: "resolve-effect-" + label, Decision: authority.ApprovalApproved, Rationale: "Appropriate.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	started, err := app.UnwrapMutation(h.service.StartExternalActionExecution(h.ctx, app.StartExternalActionExecutionCommand{
		ActionID: resolved.Action.ID, ActorID: "agent:one", ExpectedActionVersion: resolved.Action.Version,
		IdempotencyKey: "start-effect-" + label, SubjectHash: proposed.Revision.AuthorizationSubjectHash,
		AuthorityGrantID: resolved.Grant.ID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	return startedEffect{execution: started.Execution.ID, actionVersion: started.Action.Version, artifact: artifact.Artifact.ID}
}

// acceptAnOutputOn produces and accepts one output on a run's work item and
// returns the accepted revision, so a later run has an exact thing to bind.
func acceptAnOutputOn(t *testing.T, h *runHarness, item work.WorkItem, label string) string {
	t.Helper()
	defined, err := app.UnwrapMutation(h.service.DefineExpectedOutput(h.ctx, app.DefineExpectedOutputCommand{
		WorkItemID: item.ID, ActorID: "agent:one", Name: "Dossier from " + label, ProfileName: "research_dossier",
		ProfileVersion: 1, Ordinal: 1, Required: true, ExpectedVersion: item.Version, IdempotencyKey: "define-" + label,
	}))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := app.UnwrapMutation(h.service.CreateOutputRevision(h.ctx, app.CreateOutputRevisionCommand{
		ExpectedOutputID: defined.ID, ActorID: "agent:one", IdempotencyKey: "revision-" + label,
		ContentDigest: "sha256:" + label,
		Artifacts:     []app.OutputArtifactInput{{Kind: "document", URI: "file:///" + label + ".md", Title: "Dossier", Role: "primary"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for index, command := range []app.RecordValidationCommand{
		{CriterionRef: "structure", ValidatorKind: output.ValidatorStructure, Verdict: output.VerdictPassed, VerifierActorID: "agent:one"},
		{CriterionRef: "provenance", ValidatorKind: output.ValidatorProvenance, Verdict: output.VerdictPassed, VerifierActorID: "agent:one"},
		{CriterionRef: "human_review", ValidatorKind: output.ValidatorHumanReview, Verdict: output.VerdictPassed, VerifierActorID: "human:owner"},
	} {
		command.OutputRevisionID = revision.ID
		command.IdempotencyKey = "validate-" + label + "-" + string(rune('a'+index))
		command.Details = json.RawMessage(`{"summary":"Checked.","rationale":"The dossier separates evidence from uncertainty."}`)
		revision, err = app.UnwrapMutation(h.service.RecordValidation(h.ctx, command))
		if err != nil {
			t.Fatal(err)
		}
	}
	if revision.AcceptanceState != output.RevisionAccepted {
		t.Fatalf("the fixture output was not accepted: %q", revision.AcceptanceState)
	}
	return revision.ID
}

// TestEveryPlanStepIsMaterializedExactlyOnce is the counting claim: a run
// holds one work item per step, no step twice and none missing, and a second
// run of the same definition does the same thing again from scratch.
func TestEveryPlanStepIsMaterializedExactlyOnce(t *testing.T) {
	h := newRunHarness(t, "materialize-once.db", nil, 2)
	steps, err := h.service.GetObjectiveContext(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	var definition []work.PlanStep
	for _, candidate := range steps.Plans {
		if candidate.Plan.ID == h.plan.ID {
			definition = candidate.Steps
		}
	}
	if len(definition) == 0 {
		t.Fatal("the harness plan has no steps")
	}
	for _, key := range []string{"run-1", "run-2"} {
		run, err := h.createRun(key, "create-"+key, "agent:one")
		if err != nil {
			t.Fatal(err)
		}
		perStep := map[string]int{}
		for _, item := range run.WorkItems {
			perStep[item.OriginPlanStepID]++
		}
		if len(perStep) != len(definition) {
			t.Fatalf("%s materialized %d distinct steps, want %d", key, len(perStep), len(definition))
		}
		for _, step := range definition {
			if perStep[step.ID] != 1 {
				t.Fatalf("%s materialized step %s %d times, want once", key, step.Key, perStep[step.ID])
			}
		}
	}
}

// TestARunCannotBindAnUnacceptedOrForeignOutput pins what a cross-run input
// may name: one exact accepted revision, nothing provisional and nothing
// invented.
func TestARunCannotBindAnUnacceptedOrForeignOutput(t *testing.T) {
	h := newRunHarness(t, "binding-limits.db", []app.ProposedPlanInput{{Name: "prior", Required: true, Ordinal: 1}}, 3)
	producing, err := h.createRun("run-1", "create-1", "agent:one",
		app.RunInputBindingCommand{Name: "prior", Value: "bootstrap"})
	if err != nil {
		t.Fatal(err)
	}
	item := producing.WorkItems[0]
	defined, err := app.UnwrapMutation(h.service.DefineExpectedOutput(h.ctx, app.DefineExpectedOutputCommand{
		WorkItemID: item.ID, ActorID: "agent:one", Name: "An unfinished dossier", ProfileName: "research_dossier",
		ProfileVersion: 1, Ordinal: 1, Required: true, ExpectedVersion: item.Version, IdempotencyKey: "define-unaccepted",
	}))
	if err != nil {
		t.Fatal(err)
	}
	produced, err := app.UnwrapMutation(h.service.CreateOutputRevision(h.ctx, app.CreateOutputRevisionCommand{
		ExpectedOutputID: defined.ID, ActorID: "agent:one", IdempotencyKey: "revision-unaccepted",
		ContentDigest: "sha256:unaccepted",
		Artifacts:     []app.OutputArtifactInput{{Kind: "document", URI: "file:///unaccepted.md", Title: "Draft", Role: "primary"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if produced.AcceptanceState == output.RevisionAccepted {
		t.Fatalf("the fixture revision was already accepted: %#v", produced)
	}
	if _, err := h.createRun("run-unaccepted", "create-unaccepted", "agent:one",
		app.RunInputBindingCommand{Name: "prior", OutputRevisionID: produced.ID}); err == nil {
		t.Fatal("a run bound an output revision that was never accepted")
	}
	if _, err := h.createRun("run-missing", "create-missing", "agent:one",
		app.RunInputBindingCommand{Name: "prior", OutputRevisionID: "00000000-0000-7000-8000-000000000000"}); err == nil {
		t.Fatal("a run bound an output revision that does not exist")
	}
}

// TestWorkCannotHangOffAnotherRunsWork is a regression: runs are separate, so
// a structural parent link across them would make one run's shape depend on
// another's. Only the objective was checked before, which let it through.
func TestWorkCannotHangOffAnotherRunsWork(t *testing.T) {
	h := newRunHarness(t, "cross-run-parent.db", nil, 2)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.createRun("run-2", "create-2", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	// Run-local work may not be parented onto another run's work.
	if _, err := h.service.CreateWorkItem(h.ctx, app.CreateWorkItemCommand{
		ActorID: "agent:one", IdempotencyKey: "create-cross-run-child", Key: "CROSS-RUN-1",
		ObjectiveID: h.objective.ID, PlanRunID: second.Run.ID, ParentID: first.WorkItems[0].ID,
		Title: "A child of another run's work", Kind: "research",
		CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
		ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		AttentionState: work.AttentionNone,
	}); err == nil {
		t.Fatal("run-local work was parented onto another run's work")
	}
	// Nor may work outside any run adopt a run's work as its parent.
	if _, err := h.service.CreateWorkItem(h.ctx, app.CreateWorkItemCommand{
		ActorID: "agent:one", IdempotencyKey: "create-unplanned-child", Key: "CROSS-RUN-2",
		ObjectiveID: h.objective.ID, ParentID: first.WorkItems[0].ID,
		Title: "An idea hanging off a run", Kind: "research",
		CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
		ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		AttentionState: work.AttentionNone,
	}); err == nil {
		t.Fatal("work outside a run was parented onto a run's work")
	}
	// Patching a parent afterwards cannot get around it either.
	sibling, err := h.service.CreateWorkItem(h.ctx, app.CreateWorkItemCommand{
		ActorID: "agent:one", IdempotencyKey: "create-run-local-sibling", Key: "CROSS-RUN-3",
		ObjectiveID: h.objective.ID, PlanRunID: second.Run.ID,
		Title: "Run-local work of the second run", Kind: "research",
		CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
		ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		AttentionState: work.AttentionNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(h.service.PatchWorkItem(h.ctx, app.PatchWorkItemCommand{
		WorkItemID: sibling.Result.ID, ActorID: "agent:one", IdempotencyKey: "patch-cross-run-parent",
		ExpectedVersion: sibling.Result.Version, ParentID: stringPointer(first.WorkItems[0].ID),
	})); err == nil {
		t.Fatal("a parent belonging to another run was patched in")
	}
	// The same run's work is a legitimate parent.
	if _, err := app.UnwrapMutation(h.service.PatchWorkItem(h.ctx, app.PatchWorkItemCommand{
		WorkItemID: sibling.Result.ID, ActorID: "agent:one", IdempotencyKey: "patch-same-run-parent",
		ExpectedVersion: sibling.Result.Version, ParentID: stringPointer(second.WorkItems[0].ID),
	})); err != nil {
		t.Fatalf("a parent inside the same run was refused: %v", err)
	}
}

// TestDependenciesCannotCrossRuns is the ordering half of run separateness. A
// dependency decides when work becomes ready, so an edge across runs would
// make one run wait on another — and the sanctioned way to use an earlier
// run's result is to bind its exact accepted output revision.
func TestDependenciesCannotCrossRuns(t *testing.T) {
	h := newRunHarness(t, "cross-run-dependency.db", nil, 2)
	first, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.createRun("run-2", "create-2", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	child := second.WorkItems[0]
	_, err = app.UnwrapMutation(h.service.LinkDependency(h.ctx, app.LinkDependencyCommand{
		WorkItemID: child.ID, DependsOnWorkItemID: first.WorkItems[0].ID, Kind: work.DependencyHard,
		ActorID: "agent:one", ExpectedVersion: child.Version, IdempotencyKey: "link-cross-run",
	}))
	if err == nil {
		t.Fatal("a dependency was linked across two runs")
	}
	if !strings.Contains(err.Error(), "plan run") {
		t.Fatalf("the refusal does not name the run boundary: %v", err)
	}
	// Inside one run it is ordinary.
	if _, err := app.UnwrapMutation(h.service.LinkDependency(h.ctx, app.LinkDependencyCommand{
		WorkItemID: second.WorkItems[0].ID, DependsOnWorkItemID: second.WorkItems[1].ID, Kind: work.DependencySoft,
		ActorID: "agent:one", ExpectedVersion: child.Version, IdempotencyKey: "link-same-run",
	})); err != nil {
		t.Fatalf("a dependency inside one run was refused: %v", err)
	}
}

// TestALegacyPlanCannotBeRunWithoutANewRevision is the contraction promise:
// work that predates plan runs keeps working, and the plan it hung off is not
// quietly turned into a definition. Reusing it means writing a revision
// somebody reviews.
func TestALegacyPlanCannotBeRunWithoutANewRevision(t *testing.T) {
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
	database, err := Open(ctx, filepath.Join(t.TempDir(), "legacy-reuse.db"))
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
	const fixture = "2026-08-21T15:00:00.000000000Z"
	for _, statement := range []string{
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, updated_by, version, created_at, updated_at)
		 VALUES ('objective-legacy', 'OBJ-LEGACY-REUSE', 'Legacy objective', '', 'Work from before runs.', 'execution', 'human:owner', 2, '` + fixture + `', '` + fixture + `')`,
		`INSERT INTO plans (id, objective_id, title, summary, revision, commitment_state, version, created_at, updated_at)
		 VALUES ('plan-legacy', 'objective-legacy', 'Legacy plan', '', 1, 'approved', 2, '` + fixture + `', '` + fixture + `')`,
		`INSERT INTO work_items (id, key, objective_id, plan_id, title, description, kind, commitment_state, execution_status,
		   priority, estimated_scope, execution_policy, required_actor_kind, attention_state, version, created_at, updated_at)
		 VALUES ('item-legacy', 'TH-LEGACY-REUSE', 'objective-legacy', 'plan-legacy', 'Legacy work', '', 'research', 'accepted', 'done',
		   'medium', 'small', 'autonomous_with_report', 'agent', 'none', 4, '` + fixture + `', '` + fixture + `')`,
		`INSERT INTO actors (id, kind, display_name, version, created_at) VALUES ('agent:one', 'agent', 'Agent', 1, '` + fixture + `')`,
		`INSERT INTO actors (id, kind, display_name, version, created_at) VALUES ('human:owner', 'human', 'Owner', 1, '` + fixture + `')`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})

	// The legacy plan is approved, and still cannot be run: it holds work, not
	// a definition, and Throughline will not invent one from it.
	_, err = app.UnwrapMutation(service.CreatePlanRun(ctx, app.CreatePlanRunCommand{
		ObjectiveID: "objective-legacy", PlanID: "plan-legacy", ActorID: "agent:one",
		IdempotencyKey: "run-legacy", RunKey: "legacy-1",
	}))
	if err == nil {
		t.Fatal("a legacy plan was instantiated as though it were a definition")
	}
	if !strings.Contains(err.Error(), "no plan steps") {
		t.Fatalf("the refusal does not say the plan has no definition: %v", err)
	}

	// Reuse goes through a new revision that someone reviews.
	proposed, err := app.UnwrapMutation(service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: "objective-legacy", ActorID: "agent:one", IdempotencyKey: "propose-from-legacy",
		Title: "The legacy plan, written as a definition", Revision: 2,
		Steps: []app.ProposedPlanStep{{
			ClientRef: "only", Key: "TH-LEGACY-REWRITTEN", Title: "Legacy work, reviewed afresh", Kind: "research", Required: true,
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
			ExecutionPolicy: work.PolicyAutonomousWithReport, RequiredActorKind: work.ActorAgent,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ReviewPlan(ctx, app.ReviewPlanCommand{
		PlanID: proposed.Plan.ID, ReviewerActorID: "human:owner", IdempotencyKey: "approve-from-legacy",
		Decision: work.PlanApproved, Reason: "Rewritten from what the legacy plan demonstrably contained.", ExpectedVersion: 1,
	})); err != nil {
		t.Fatal(err)
	}
	run, err := app.UnwrapMutation(service.CreatePlanRun(ctx, app.CreatePlanRunCommand{
		ObjectiveID: "objective-legacy", PlanID: proposed.Plan.ID, ActorID: "agent:one",
		IdempotencyKey: "run-rewritten", RunKey: "rewritten-1",
	}))
	if err != nil {
		t.Fatalf("the reviewed revision could not be run: %v", err)
	}
	if len(run.WorkItems) != 1 {
		t.Fatalf("the reviewed revision materialized %d items, want 1", len(run.WorkItems))
	}
	// And the legacy work is untouched by any of it.
	legacy, err := service.GetWorkItem(ctx, "item-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.WorkItem.Origin != work.OriginLegacy || legacy.WorkItem.PlanRunID != "" ||
		legacy.WorkItem.ExecutionStatus != work.StatusDone || legacy.WorkItem.Version != 4 {
		t.Fatalf("legacy work changed while its plan was being rewritten: %#v", legacy.WorkItem)
	}
}

// TestTheObjectiveDoesNoneOfItsNonGoals asserts the negatives directly rather
// than leaving them to the absence of a feature, because an absence is exactly
// what a later change removes without noticing.
func TestTheObjectiveDoesNoneOfItsNonGoals(t *testing.T) {
	h := newRunHarness(t, "non-goals.db", nil, 2)
	objectiveBefore, err := h.service.ResolveObjective(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Creating a run schedules nothing and moves no phase.
	run, err := h.createRun("run-1", "create-1", "agent:one")
	if err != nil {
		t.Fatal(err)
	}
	afterCreate, err := h.service.ResolveObjective(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterCreate.Phase != objectiveBefore.Phase || afterCreate.Version != objectiveBefore.Version {
		t.Fatalf("creating a run moved its objective: %#v", afterCreate)
	}

	// No step is chosen for anyone: every step is materialized, and all of the
	// run's work sits in backlog until somebody moves it.
	for _, item := range run.WorkItems {
		if item.ExecutionStatus != work.StatusBacklog {
			t.Fatalf("a materialized item was advanced for the caller: %#v", item)
		}
	}

	// Nothing resolves a run for the caller: creating one requires naming the
	// revision, and there is no way to ask for the latest.
	if _, err := app.UnwrapMutation(h.service.CreatePlanRun(h.ctx, app.CreatePlanRunCommand{
		ObjectiveID: h.objective.ID, ActorID: "agent:one", IdempotencyKey: "run-without-revision", RunKey: "no-revision",
	})); err == nil {
		t.Fatal("a run was created without naming a plan revision")
	}

	// Finishing every piece of a run's work does not finish the run: only an
	// explicit close does, and it still does not move the objective.
	for _, item := range run.WorkItems {
		current, err := h.service.GetWorkItem(h.ctx, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		carryWorkItemToDone(t, h, current.WorkItem, "non-goal-"+item.Key)
	}
	stillActive, err := h.service.GetPlanRun(h.ctx, run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillActive.Run.Status != work.PlanRunActive {
		t.Fatalf("a run ended itself once its work was done: %#v", stillActive.Run)
	}
	if _, err := app.UnwrapMutation(h.service.ClosePlanRun(h.ctx, app.ClosePlanRunCommand{
		PlanRunID: run.Run.ID, ActorID: "agent:one", IdempotencyKey: "close",
		ExpectedVersion: stillActive.Run.Version, TargetStatus: work.PlanRunSucceeded,
	})); err != nil {
		t.Fatal(err)
	}
	afterClose, err := h.service.ResolveObjective(h.ctx, h.objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterClose.Phase != objectiveBefore.Phase || afterClose.Version != objectiveBefore.Version {
		t.Fatalf("closing a run moved its objective: %#v", afterClose)
	}
}

// TestAMigratedWorkspaceIsStableAcrossRepeatedReopens closes the migration
// story: a populated pre-run database is upgraded once, and every later
// process that opens it sees the same thing and changes nothing by looking.
func TestAMigratedWorkspaceIsStableAcrossRepeatedReopens(t *testing.T) {
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
	path := filepath.Join(t.TempDir(), "repeated-reopen.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:before] {
		if err := database.applyMigration(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	const fixture = "2026-08-21T15:00:00.000000000Z"
	for _, statement := range []string{
		`INSERT INTO objectives (id, key, title, description, desired_outcome, phase, updated_by, version, created_at, updated_at)
		 VALUES ('objective-reopen', 'OBJ-REOPEN', 'Populated before runs', '', 'Stable across reopens.', 'execution', 'human:owner', 2, '` + fixture + `', '` + fixture + `')`,
		`INSERT INTO plans (id, objective_id, title, summary, revision, commitment_state, version, created_at, updated_at)
		 VALUES ('plan-reopen', 'objective-reopen', 'Legacy plan', '', 1, 'approved', 2, '` + fixture + `', '` + fixture + `')`,
		`INSERT INTO work_items (id, key, objective_id, plan_id, title, description, kind, commitment_state, execution_status,
		   priority, estimated_scope, execution_policy, required_actor_kind, attention_state, version, created_at, updated_at)
		 VALUES ('item-reopen', 'TH-REOPEN', 'objective-reopen', 'plan-reopen', 'Legacy work', '', 'research', 'accepted', 'ready',
		   'medium', 'small', 'autonomous_with_report', 'agent', 'none', 3, '` + fixture + `', '` + fixture + `')`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	// Every later open sees exactly the same state, and migrating again is a
	// no-op rather than a second upgrade.
	var previous []string
	for attempt := 1; attempt <= 3; attempt++ {
		reopened, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("reopen %d: %v", attempt, err)
		}
		if err := reopened.Migrate(ctx); err != nil {
			t.Fatalf("reopen %d migrate: %v", attempt, err)
		}
		var applied int
		if err := reopened.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil {
			t.Fatal(err)
		}
		if applied != len(migrations) {
			t.Fatalf("reopen %d applied %d migrations, want %d", attempt, applied, len(migrations))
		}
		snapshot := []string{}
		for _, query := range []string{
			"SELECT origin || '/' || COALESCE(plan_run_id, '-') || '/' || execution_status || '/' || version FROM work_items WHERE id = 'item-reopen'",
			"SELECT mode || '/' || max_concurrent_runs || '/' || phase || '/' || version FROM objectives WHERE id = 'objective-reopen'",
			"SELECT CAST(COUNT(*) AS TEXT) FROM plan_runs",
			"SELECT CAST(COUNT(*) AS TEXT) FROM plan_steps",
			"SELECT commitment_state || '/' || version FROM plans WHERE id = 'plan-reopen'",
		} {
			var value string
			if err := reopened.db.QueryRowContext(ctx, query).Scan(&value); err != nil {
				t.Fatal(err)
			}
			snapshot = append(snapshot, value)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
		if previous != nil && !reflect.DeepEqual(snapshot, previous) {
			t.Fatalf("reopen %d saw different state:\n before %#v\n now    %#v", attempt, previous, snapshot)
		}
		previous = snapshot
	}
	if previous[0] != "legacy/-/ready/3" {
		t.Fatalf("the legacy work item changed across reopens: %q", previous[0])
	}
	if previous[1] != "finite/1/execution/2" {
		t.Fatalf("the migrated objective changed across reopens: %q", previous[1])
	}
	if previous[2] != "0" || previous[3] != "0" {
		t.Fatalf("reopening invented runs or steps: %q runs, %q steps", previous[2], previous[3])
	}
}
