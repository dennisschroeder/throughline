package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// artifactFixture is one accepted work item ready to carry attached artifacts.
func artifactFixture(t *testing.T, name string) (context.Context, *app.Service, work.WorkItem) {
	t.Helper()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "artifact-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "artifact-objective", Key: "OBJ-ARTIFACT",
		Title: "Carries relative artifacts", DesiredOutcome: "References survive a relocation.", Phase: work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	item, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
		ActorID: "human:owner", IdempotencyKey: "artifact-item", Key: "TH-ARTIFACT", ObjectiveID: objective.ID,
		Title: "Carries the artifact", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone,
	}))
	if err != nil {
		t.Fatal(err)
	}
	return ctx, service, item
}

// TestAttachArtifactAcceptsAndPersistsAWorkspaceRelativeReference is REP-05's
// first artifact criterion, exercised through the actual mutation an agent
// calls rather than only through the domain constructor.
func TestAttachArtifactAcceptsAndPersistsAWorkspaceRelativeReference(t *testing.T) {
	ctx, service, item := artifactFixture(t, "relative.db")

	attached, err := app.UnwrapMutation(service.AttachArtifact(ctx, app.AttachArtifactCommand{
		WorkItemID: item.ID, ActorID: "human:owner", ExpectedVersion: item.Version, IdempotencyKey: "attach-relative",
		Kind: "document", URI: "workspace:docs/report.md", Title: "The report",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if attached.Artifact.URI != "workspace:docs/report.md" {
		t.Fatalf("artifact URI = %q, want the workspace-relative reference unchanged", attached.Artifact.URI)
	}

	itemContext, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(itemContext.Artifacts) != 1 || itemContext.Artifacts[0].URI != "workspace:docs/report.md" {
		t.Fatalf("artifacts after attach = %#v", itemContext.Artifacts)
	}
}

// TestAttachArtifactRejectsAWorkspaceReferenceThatEscapesTheRoot exercises the
// containment half of REP-05's second artifact criterion end to end: the
// mutation must fail with a domain error, not persist a reference that would
// point outside the workspace once resolved.
func TestAttachArtifactRejectsAWorkspaceReferenceThatEscapesTheRoot(t *testing.T) {
	ctx, service, item := artifactFixture(t, "escaping.db")

	_, err := service.AttachArtifact(ctx, app.AttachArtifactCommand{
		WorkItemID: item.ID, ActorID: "human:owner", ExpectedVersion: item.Version, IdempotencyKey: "attach-escaping",
		Kind: "document", URI: "workspace:../outside.md",
	})
	if err == nil {
		t.Fatal("attaching an artifact that escapes the workspace root succeeded")
	}

	itemContext, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(itemContext.Artifacts) != 0 {
		t.Fatalf("artifacts after a rejected attach = %#v, want none persisted", itemContext.Artifacts)
	}
}

// TestAttachArtifactDeduplicatesEquivalentAbsoluteReferences is REP-05's second
// artifact criterion, the dedup half. attachArtifactMutation already looks an
// artifact up by URI before creating a new one; this fails if that lookup ever
// again compares two differently-spelled references to the same file as
// distinct, which is exactly the gap normalizeArtifactURI closes.
func TestAttachArtifactDeduplicatesEquivalentAbsoluteReferences(t *testing.T) {
	ctx, service, item := artifactFixture(t, "dedup.db")

	first, err := app.UnwrapMutation(service.AttachArtifact(ctx, app.AttachArtifactCommand{
		WorkItemID: item.ID, ActorID: "human:owner", ExpectedVersion: item.Version, IdempotencyKey: "attach-first",
		Kind: "document", URI: "file:///workspace/./docs/report.md",
	}))
	if err != nil {
		t.Fatal(err)
	}

	second, err := app.UnwrapMutation(service.AttachArtifact(ctx, app.AttachArtifactCommand{
		WorkItemID: item.ID, ActorID: "human:owner", ExpectedVersion: first.WorkItem.Version, IdempotencyKey: "attach-second",
		Kind: "document", URI: "file:///workspace/docs/report.md",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if second.Artifact.ID != first.Artifact.ID {
		t.Fatalf("attaching an equivalent absolute URI created a second artifact %q, want the existing %q", second.Artifact.ID, first.Artifact.ID)
	}

	itemContext, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(itemContext.Artifacts) != 1 {
		t.Fatalf("artifacts after two equivalent attaches = %#v, want exactly one", itemContext.Artifacts)
	}
}
