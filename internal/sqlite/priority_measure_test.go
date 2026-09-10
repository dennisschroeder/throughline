package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// TestObjectivePriorityAndAppetiteSurvivePatchAndReopen is REP-06's first
// criterion for objectives: both fields must persist through a patch and
// still read back correctly after the database is closed and reopened, not
// only within one open connection.
func TestObjectivePriorityAndAppetiteSurvivePatchAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "priority.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "priority-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "priority-objective", Key: "OBJ-PRIORITY",
		Title: "Carries priority and appetite", DesiredOutcome: "Values survive.", Phase: work.ObjectivePlanning,
		Priority: work.PriorityHigh,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if objective.Priority != work.PriorityHigh {
		t.Fatalf("priority at creation = %q, want high", objective.Priority)
	}

	appetite := work.Measure{Value: 200, Unit: "tokens", Basis: work.MeasureEstimated}
	patched, err := app.UnwrapMutation(service.PatchObjective(ctx, app.PatchObjectiveCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "priority-appetite",
		ExpectedVersion: objective.Version, Appetite: &appetite,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if patched.Appetite != appetite {
		t.Fatalf("appetite after patch = %+v, want %+v", patched.Appetite, appetite)
	}

	// An unrelated patch must not disturb either field.
	title := "Renamed, priority and appetite unchanged"
	renamed, err := app.UnwrapMutation(service.PatchObjective(ctx, app.PatchObjectiveCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "priority-rename",
		ExpectedVersion: patched.Version, Title: &title,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Priority != work.PriorityHigh || renamed.Appetite != appetite {
		t.Fatalf("an unrelated patch changed priority/appetite: %+v", renamed)
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := app.NewService(reopened.Store(), &testIDs{}, testClock{}).GetObjectiveContext(ctx, objective.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Objective.Priority != work.PriorityHigh || restarted.Objective.Appetite != appetite {
		t.Fatalf("after reopening: priority=%q appetite=%+v, want high and %+v", restarted.Objective.Priority, restarted.Objective.Appetite, appetite)
	}
}

// TestWorkItemMeasureSurvivesPatchAndReopen mirrors the objective test for a
// work item's Measure, which sits beside the existing coarse EstimatedScope
// hint rather than replacing it.
func TestWorkItemMeasureSurvivesPatchAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "measure.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "measure-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "measure-objective", Key: "OBJ-MEASURE",
		Title: "Carries a measured item", DesiredOutcome: "The measure survives.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	item, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
		ActorID: "human:owner", IdempotencyKey: "measure-item", Key: "TH-MEASURE", ObjectiveID: objective.ID,
		Title: "Carries the measure", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeLarge, ExecutionPolicy: work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if item.EstimatedScope != work.ScopeLarge {
		t.Fatalf("estimated scope = %q, want the coarse hint to remain settable alongside measure", item.EstimatedScope)
	}

	measure := work.Measure{Value: 350000, Unit: "bytes", Basis: work.MeasureMeasured}
	patched, err := app.UnwrapMutation(service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "measure-patch",
		ExpectedVersion: item.Version, Measure: &measure,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if patched.Measure != measure {
		t.Fatalf("measure after patch = %+v, want %+v", patched.Measure, measure)
	}
	if patched.EstimatedScope != work.ScopeLarge {
		t.Fatal("patching measure changed the unrelated estimated scope")
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := app.NewService(reopened.Store(), &testIDs{}, testClock{}).GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.WorkItem.Measure != measure {
		t.Fatalf("after reopening, measure = %+v, want %+v", restarted.WorkItem.Measure, measure)
	}
}

// TestContextNonGoalAndAffectedTransitionThroughTheService is REP-06's second
// criterion exercised through the actual app.Service mutation an agent calls,
// not only through the domain functions in isolation.
func TestContextNonGoalAndAffectedTransitionThroughTheService(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "context-kinds.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "kind-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "kind-objective", Key: "OBJ-KINDS",
		Title: "Carries the new context kinds", DesiredOutcome: "Both kinds transition correctly.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}

	for _, kind := range []work.ContextKind{work.ContextNonGoal, work.ContextAffected} {
		created, err := app.UnwrapMutation(service.RecordContext(ctx, app.RecordContextCommand{
			ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "kind-record-" + string(kind),
			Kind: kind, Title: "Explicitly scoped", Status: work.ContextProposed,
		}))
		if err != nil {
			t.Fatalf("%s: record: %v", kind, err)
		}
		accepted, err := app.UnwrapMutation(service.TransitionContext(ctx, app.TransitionContextCommand{
			ContextRecordID: created.ID, ActorID: "human:owner", IdempotencyKey: "kind-accept-" + string(kind),
			ExpectedVersion: created.Version, TargetStatus: work.ContextAccepted,
		}))
		if err != nil {
			t.Fatalf("%s: proposed->accepted: %v", kind, err)
		}
		if accepted.Status != work.ContextAccepted {
			t.Fatalf("%s: status = %q, want accepted", kind, accepted.Status)
		}
	}
}
