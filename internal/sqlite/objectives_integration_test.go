package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// TestObjectivesAreReadableWithoutWorkItems is the first criterion. Every path
// that needed a list of objectives used to derive one from the work items, so an
// objective became visible only once it had its first item — which is exactly
// backwards, since the moment a person most wants to address an objective is
// right after creating it.
func TestObjectivesAreReadableWithoutWorkItems(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "objectives.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "objectives-owner"})); err != nil {
		t.Fatal(err)
	}
	empty, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "objectives-empty", Key: "OBJ-ZZ-EMPTY",
		Title: "Has no work items at all", DesiredOutcome: "It is still addressable.", Phase: work.ObjectiveIdea,
	}))
	if err != nil {
		t.Fatal(err)
	}

	objectives, err := service.ListObjectives(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(objectives) != 1 || objectives[0].ID != empty.ID {
		t.Fatalf("ListObjectives = %#v, want the objective that has no items", objectives)
	}

	// Addressable by identifier and by the key a person writes down.
	for _, reference := range []string{empty.ID, "OBJ-ZZ-EMPTY"} {
		resolved, err := service.ResolveObjective(ctx, reference)
		if err != nil {
			t.Fatalf("ResolveObjective(%q) = %v", reference, err)
		}
		if resolved.ID != empty.ID {
			t.Fatalf("ResolveObjective(%q) = %q, want %q", reference, resolved.ID, empty.ID)
		}
	}
	if _, err := service.ResolveObjective(ctx, "OBJ-DOES-NOT-EXIST"); err == nil {
		t.Fatal("resolving an unknown reference succeeded")
	}

	// A second objective, this one with an item, must not shadow the first.
	withItem, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "objectives-full", Key: "OBJ-AA-FULL",
		Title: "Has one work item", DesiredOutcome: "Both are listed.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
		ActorID: "human:owner", IdempotencyKey: "objectives-item", Key: "TH-FULL", ObjectiveID: withItem.ID,
		Title: "The only item", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone,
	})); err != nil {
		t.Fatal(err)
	}
	objectives, err = service.ListObjectives(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(objectives) != 2 {
		t.Fatalf("ListObjectives returned %d objectives, want both", len(objectives))
	}
	// OBJ-ZZ-EMPTY was created first and has the lower identifier, so insertion
	// order and identifier order both put it first. Only an order by key puts
	// OBJ-AA-FULL there, which is what makes this assertion worth making.
	if objectives[0].Key != "OBJ-AA-FULL" || objectives[1].Key != "OBJ-ZZ-EMPTY" {
		t.Fatalf("ListObjectives order = %q, %q, want them ordered by key", objectives[0].Key, objectives[1].Key)
	}
}
