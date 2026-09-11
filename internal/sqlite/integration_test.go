package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

func TestInitializationAndDomainNeutralVerticalSlice(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "throughline.db")
	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}

	assertInitializationState(t, database)

	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID:        "human:owner",
		IdempotencyKey: "create-objective-integration",
		Key:            "OBJ-DOSSIER",
		Title:          "Produce a renewable-energy policy research dossier",
		Description:    "Synthesize source-linked policy evidence for a human decision.",
		DesiredOutcome: "A reviewed dossier with findings, sources, and explicit uncertainty.",
		Phase:          work.ObjectivePlanning,
	}))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.UnwrapMutation(service.CreatePlan(ctx, app.CreatePlanCommand{
		ActorID:         "human:owner",
		IdempotencyKey:  "create-plan-integration",
		ObjectiveID:     objective.ID,
		Title:           "Research dossier plan",
		Summary:         "Collect sources, synthesize findings, and submit the dossier for review.",
		Revision:        1,
		CommitmentState: work.PlanDraft,
	}))
	if err != nil {
		t.Fatal(err)
	}
	item, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
		ActorID:           "human:owner",
		IdempotencyKey:    "create-work-item-integration",
		Key:               "TH-1",
		ObjectiveID:       objective.ID,
		PlanID:            plan.ID,
		Title:             "Synthesize the policy evidence",
		Description:       "Produce the dossier without performing any external action.",
		Kind:              "research",
		CommitmentState:   work.ItemProposed,
		ExecutionStatus:   work.StatusBacklog,
		Priority:          work.PriorityHigh,
		EstimatedScope:    work.ScopeMedium,
		ExecutionPolicy:   work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny,
		AttentionState:    work.AttentionNeedsHumanReview,
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.UnwrapMutation(service.DefineExpectedOutput(ctx, app.DefineExpectedOutputCommand{
		ActorID:         "human:owner",
		WorkItemID:      item.ID,
		Name:            "Policy research dossier",
		ProfileName:     "research_dossier",
		ProfileVersion:  1,
		Contract:        json.RawMessage(`{"jurisdiction":"EU","source_recency":"five_years","validation":{"required":[{"kind":"evaluation","criterion_ref":"instance_contract"}]}}`),
		DestinationHint: "dossiers/renewable-energy-policy.md",
		Required:        true,
		Ordinal:         1,
		ExpectedVersion: item.Version,
		IdempotencyKey:  "define-policy-output",
	}))
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Objective.ID != objective.ID || result.Plan == nil || result.Plan.ID != plan.ID || result.WorkItem.ID != item.ID {
		t.Fatalf("broken structured context chain: %#v", result)
	}
	if len(result.ExpectedOutputs) != 1 {
		t.Fatalf("expected one output, got %d", len(result.ExpectedOutputs))
	}
	profile := result.ExpectedOutputs[0].Profile
	if profile.Name != "research_dossier" || profile.Version != 1 || profile.LifecycleState != output.ProfileActive {
		t.Fatalf("unexpected profile binding: %#v", profile)
	}
}

func TestMutationEffectsReplayAcrossRestartAndLegacyResponseUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "effects.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:effects", Kind: work.ActorTypeHuman, DisplayName: "Effects"}, IdempotencyKey: "register"})); err != nil {
		t.Fatal(err)
	}
	command := app.CreateObjectiveCommand{ActorID: "human:effects", IdempotencyKey: "objective", Key: "OBJ-EFFECTS", Title: "Effect replay", Phase: work.ObjectiveIdea}
	created, err := service.CreateObjective(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	objective := created.Result
	effects := created.Effects
	if len(effects) != 1 || effects[0].Kind != "objective" || effects[0].ID != objective.ID || effects[0].Version != objective.Version {
		t.Fatalf("first mutation effects = %#v, want committed objective version", effects)
	}
	var response string
	if err := database.db.QueryRowContext(ctx, "SELECT response_json FROM idempotency_records WHERE actor_id = ? AND key = ?", command.ActorID, command.IdempotencyKey).Scan(&response); err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err := json.Unmarshal([]byte(response), &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "effects")
	legacyResponse, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, "UPDATE idempotency_records SET response_json = ? WHERE actor_id = ? AND key = ?", legacyResponse, command.ActorID, command.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	replayService := app.NewService(reopened.Store(), &testIDs{fail: true}, testClock{})
	replayedMutation, err := replayService.CreateObjective(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if replayedMutation.Result.ID != objective.ID || !slices.Equal(replayedMutation.Effects, created.Effects) {
		t.Fatalf("restart replay = %#v, want %#v", replayedMutation, created)
	}
	var replayedResponse string
	if err := reopened.db.QueryRowContext(ctx, "SELECT response_json FROM idempotency_records WHERE actor_id = ? AND key = ?", command.ActorID, command.IdempotencyKey).Scan(&replayedResponse); err != nil {
		t.Fatal(err)
	}
	if replayedResponse != string(legacyResponse) {
		t.Fatalf("restart replay rewrote idempotency response\ngot:  %s\nwant: %s", replayedResponse, legacyResponse)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	legacyDatabase, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacyDatabase.Close()
	legacyReplay, err := app.NewService(legacyDatabase.Store(), &testIDs{fail: true}, testClock{}).CreateObjective(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if legacyReplay.Result.ID != objective.ID || len(legacyReplay.Effects) != 1 || legacyReplay.Effects[0].Kind != "objective" || legacyReplay.Effects[0].ID != objective.ID || legacyReplay.Effects[0].Version != objective.Version {
		t.Fatalf("legacy replay = %#v, want original objective effect", legacyReplay)
	}
}

func TestCreateObjectiveReplayDoesNotAllocateAnID(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "idempotency.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ids := &testIDs{}
	service := app.NewService(database.Store(), ids, testClock{})
	command := app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "replay-objective", Key: "OBJ-REPLAY", Title: "Replay safely", DesiredOutcome: "No second ID allocation", Phase: work.ObjectiveIdea}
	first, err := app.UnwrapMutation(service.CreateObjective(ctx, command))
	if err != nil {
		t.Fatal(err)
	}
	allocated := ids.next
	ids.fail = true
	replayed, err := app.UnwrapMutation(service.CreateObjective(ctx, command))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != first.ID || ids.next != allocated {
		t.Fatalf("replay = %#v, allocated IDs = %d, want original %#v and %d", replayed, ids.next, first, allocated)
	}
	command.Title = "Changed request"
	if _, err := app.UnwrapMutation(service.CreateObjective(ctx, command)); !errors.Is(err, ports.ErrIdempotencyMismatch) {
		t.Fatalf("mismatched request error = %v", err)
	}
}

func TestCreateWorkItemPersistsInitialExecutionGraphAtomically(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "compound-work-item.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-owner-compound"})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.AssignActorCapability(ctx, app.AssignActorCapabilityCommand{ActorID: "human:owner", GrantedBy: "human:owner", Capability: "web_research", Description: "Research web sources", IdempotencyKey: "assign-compound-capability"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "create-compound-objective", Key: "OBJ-COMPOUND", Title: "Create an execution graph", DesiredOutcome: "Initial work-item details are durable together.", Phase: work.ObjectivePlanning}))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.UnwrapMutation(service.ProposePlan(ctx, app.ProposePlanCommand{ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "propose-compound-plan", Title: "Execution graph plan", Revision: 1, Items: []app.ProposedWorkItem{{ClientRef: "seed", Key: "TH-SEED", Title: "Seed the approved plan", Kind: "research", Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAny}}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.ReviewPlan(ctx, app.ReviewPlanCommand{PlanID: plan.Plan.ID, ReviewerActorID: "human:owner", IdempotencyKey: "approve-compound-plan", Decision: work.PlanApproved, Reason: "The initial item is ready for execution.", ExpectedVersion: 1})); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UnwrapMutation(service.TransitionObjective(ctx, app.TransitionObjectiveCommand{ObjectiveID: objective.ID, TargetPhase: work.ObjectiveExecution, ActorID: "human:owner", IdempotencyKey: "execute-compound-objective", Reason: "Approved plan is executing.", ExpectedVersion: 1})); err != nil {
		t.Fatal(err)
	}

	command := app.CreateWorkItemCommand{
		ActorID: "human:owner", IdempotencyKey: "create-compound-item", Key: "TH-COMPOUND", ObjectiveID: objective.ID, PlanID: plan.Plan.ID,
		Title: "Create a fully specified item", Description: "Persist every initial graph edge in one transaction.", Kind: "research",
		CommitmentState: work.ItemAccepted, ExecutionStatus: work.StatusReady, Priority: work.PriorityHigh, EstimatedScope: work.ScopeMedium,
		ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorHuman, AttentionState: work.AttentionNone,
		RequiredCapabilities: []string{"web_research"},
		AcceptanceCriteria:   []app.ProposedAcceptanceCriterion{{Text: "The graph is recovered as one context.", Required: true, Ordinal: 1}},
		ExpectedOutputs:      []app.ProposedExpectedOutput{{Name: "Compound dossier", ProfileName: "research_dossier", ProfileVersion: 1, Required: true, Ordinal: 1}},
		OutputRequirements:   []app.ProposedOutputRequirement{{RequiredProfileName: "research_dossier", VersionConstraint: "=1", Required: true, Note: "Reuse an accepted dossier."}},
		ExternalActions:      []app.ProposedExternalAction{{Required: true, Title: "Publish the reviewed dossier", Rationale: "Publication is externally authorized.", AuthorizationSubject: json.RawMessage(`{"action_type":"document.publish","target":{"repository":"research"},"arguments":[],"scope":{},"permissions":["document.write"],"credential_requirements":[],"constraints":{}}`)}},
		Dependencies:         []app.CreateWorkItemDependency{{DependsOnWorkItemID: plan.Items[0].WorkItem.ID, Kind: work.DependencyHard, Note: "Use the approved plan seed."}},
	}
	created, err := service.CreateWorkItem(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	item := created.Result
	replayedMutation, err := service.CreateWorkItem(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if replayedMutation.Result.ID != item.ID || !slices.Equal(replayedMutation.Effects, created.Effects) {
		t.Fatalf("replayed mutation = %#v, want %#v", replayedMutation, created)
	}
	contextResult, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(contextResult.RequiredCapabilities) != 1 || len(contextResult.AcceptanceCriteria) != 1 || len(contextResult.ExpectedOutputs) != 1 || len(contextResult.OutputRequirements) != 1 || len(contextResult.ExternalActions) != 1 || len(contextResult.Dependencies) != 1 {
		t.Fatalf("compound item context = %#v", contextResult)
	}
	if contextResult.WorkItem.CommitmentState != work.ItemAccepted || contextResult.WorkItem.ExecutionStatus != work.StatusReady {
		t.Fatalf("compound item state = %#v", contextResult.WorkItem)
	}
	wantEffects := map[string]int{
		"work_item:" + item.ID:                                                          1,
		"work_item_capability:" + item.ID + ":web_research":                             1,
		"acceptance_criterion:" + contextResult.AcceptanceCriteria[0].ID:                1,
		"expected_output:" + contextResult.ExpectedOutputs[0].ExpectedOutput.ID:         1,
		"output_requirement:" + contextResult.OutputRequirements[0].ID:                  1,
		"external_action:" + contextResult.ExternalActions[0].Action.ID:                 1,
		"external_action_revision:" + contextResult.ExternalActions[0].Action.ID + ":1": 1,
		"dependency:" + contextResult.Dependencies[0].ID:                                1,
	}
	assertEffects(t, created.Effects, wantEffects)

	command.IdempotencyKey = "create-premature-accepted-item"
	command.PlanID = ""
	if _, err := app.UnwrapMutation(service.CreateWorkItem(ctx, command)); err == nil {
		t.Fatal("expected accepted ready item without an approved plan to be rejected")
	}
}

func TestActivityOnlyMutationHasEmptyEffects(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "activity-only.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	mutation, err := service.RequestAttention(ctx, app.RequestAttentionCommand{
		TargetKind: "review", TargetID: "review:effects", ActorID: "human:owner",
		IdempotencyKey: "request-review-attention", AttentionState: work.AttentionNeedsHumanReview,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mutation.Effects == nil || len(mutation.Effects) != 0 {
		t.Fatalf("activity-only effects = %#v, want non-nil empty list", mutation.Effects)
	}
	replayed, err := service.RequestAttention(ctx, app.RequestAttentionCommand{
		TargetKind: "review", TargetID: "review:effects", ActorID: "human:owner",
		IdempotencyKey: "request-review-attention", AttentionState: work.AttentionNeedsHumanReview,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Effects == nil || len(replayed.Effects) != 0 {
		t.Fatalf("activity-only replay effects = %#v, want non-nil empty list", replayed.Effects)
	}
}

func assertEffects(t *testing.T, effects []app.Effect, want map[string]int) {
	t.Helper()
	got := make(map[string]int, len(effects))
	for _, effect := range effects {
		key := effect.Kind + ":" + effect.ID
		if _, duplicate := got[key]; duplicate {
			t.Fatalf("duplicate effect %q in %#v", key, effects)
		}
		got[key] = effect.Version
	}
	if !maps.Equal(got, want) {
		t.Fatalf("effects = %#v, want %#v", got, want)
	}
}

func TestPatchWorkItemPersistsNonWorkflowGraphFields(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "patch-work-item.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "create-patch-objective", Key: "OBJ-PATCH", Title: "Patch non-workflow fields", DesiredOutcome: "Safe graph edits are durable.", Phase: work.ObjectivePlanning}))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{ActorID: "human:owner", IdempotencyKey: "create-patch-parent", Key: "TH-PATCH-PARENT", ObjectiveID: objective.ID, Title: "Parent", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog, Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone}))
	if err != nil {
		t.Fatal(err)
	}
	child, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{ActorID: "human:owner", IdempotencyKey: "create-patch-child", Key: "TH-PATCH-CHILD", ObjectiveID: objective.ID, Title: "Child", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog, Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone, RequiredCapabilities: []string{"retained_capability", "removed_capability"}, AcceptanceCriteria: []app.ProposedAcceptanceCriterion{{Text: "The update is recovered from SQLite.", Required: true, Ordinal: 1}}}))
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.GetWorkItem(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := []string{"retained_capability", "added_capability"}
	mutation, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
		WorkItemID: child.ID, ActorID: "human:owner", IdempotencyKey: "patch-child-graph", ExpectedVersion: child.Version, ParentID: &parent.ID, RequiredCapabilities: &capabilities,
		AcceptanceCriterionResolutions: []app.PatchAcceptanceCriterionResolution{{CriterionID: before.AcceptanceCriteria[0].ID, Status: work.AcceptanceSatisfied, Rationale: "Checked the durable context."}},
		ExpectedOutputsToAdd:           []app.ProposedExpectedOutput{{Name: "Patched dossier", ProfileName: "research_dossier", ProfileVersion: 1, Required: true, Ordinal: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	patched := mutation.Result
	if patched.Version != child.Version+1 || patched.ParentID != parent.ID {
		t.Fatalf("patched item = %#v", patched)
	}
	after, err := service.GetWorkItem(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The store lists required capabilities by slug, so compare sets rather than
	// the order the patch happened to pass them in.
	if !slices.Equal(after.RequiredCapabilities, []string{"added_capability", "retained_capability"}) || len(after.AcceptanceCriteria) != 1 || after.AcceptanceCriteria[0].Status != work.AcceptanceSatisfied || after.AcceptanceCriteria[0].ResolvedBy != "human:owner" || len(after.ExpectedOutputs) != 1 || after.ExpectedOutputs[0].ExpectedOutput.Version != 1 {
		t.Fatalf("patched item context = %#v", after)
	}
	assertEffects(t, mutation.Effects, map[string]int{
		"work_item_capability:" + child.ID + ":removed_capability":      1,
		"capability:added_capability":                                   1,
		"work_item_capability:" + child.ID + ":added_capability":        1,
		"acceptance_criterion:" + before.AcceptanceCriteria[0].ID:       2,
		"expected_output:" + after.ExpectedOutputs[0].ExpectedOutput.ID: 1,
		"work_item:" + child.ID:                                         2,
	})
	var retainedVersion int
	if err := database.db.QueryRowContext(ctx, "SELECT version FROM work_item_capabilities WHERE work_item_id = ? AND capability_slug = ?", child.ID, "retained_capability").Scan(&retainedVersion); err != nil {
		t.Fatal(err)
	}
	if retainedVersion != 1 {
		t.Fatalf("retained capability version = %d, want 1", retainedVersion)
	}
	if _, err := app.UnwrapMutation(service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: parent.ID, ActorID: "human:owner", IdempotencyKey: "patch-parent-cycle", ExpectedVersion: parent.Version, ParentID: &child.ID})); err == nil {
		t.Fatal("expected recursive parent update to be rejected")
	}
	if _, err := app.UnwrapMutation(service.PatchWorkItem(ctx, app.PatchWorkItemCommand{WorkItemID: child.ID, ActorID: "human:owner", IdempotencyKey: "patch-stale-child", ExpectedVersion: child.Version, Title: stringPointer("stale update")})); err == nil {
		t.Fatal("expected stale patch to be rejected")
	}
}

func stringPointer(value string) *string { return &value }

func assertInitializationState(t *testing.T, database *Database) {
	t.Helper()
	queries := []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM schema_migrations", 14},
		{"SELECT COUNT(*) FROM output_profiles", 8},
		{"SELECT COUNT(*) FROM output_profiles WHERE lifecycle_state = 'active' AND built_in = 1", 8},
		{"PRAGMA foreign_keys", 1},
		{"PRAGMA busy_timeout", 5000},
	}
	for _, query := range queries {
		var got int
		if err := database.db.QueryRow(query.query).Scan(&got); err != nil {
			t.Fatalf("query %q: %v", query.query, err)
		}
		if got != query.want {
			t.Fatalf("query %q returned %d, want %d", query.query, got, query.want)
		}
	}
	rows, err := database.db.Query("SELECT name, version FROM output_profiles ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantProfiles := []string{
		"agent_definition/v1",
		"decision_record/v1",
		"generic_artifact/v1",
		"research_dossier/v1",
		"skill_package/v1",
		"structured_document/v1",
		"tool_installation/v1",
		"workflow_definition/v1",
	}
	var profiles []string
	for rows.Next() {
		var name string
		var version int
		if err := rows.Scan(&name, &version); err != nil {
			t.Fatal(err)
		}
		profiles = append(profiles, name+"/v"+fmt.Sprint(version))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(profiles, wantProfiles) {
		t.Fatalf("seeded profiles = %v, want %v", profiles, wantProfiles)
	}
	var journalMode string
	if err := database.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode %q, want wal", journalMode)
	}
}

type testIDs struct {
	next int
	fail bool
}

func (ids *testIDs) New() (string, error) {
	if ids.fail {
		return "", errors.New("ID allocation must not happen during replay")
	}
	ids.next++
	return time.Date(2026, 8, 21, 0, 0, ids.next, 0, time.UTC).Format("20060102T150405.000000000"), nil
}

type testClock struct{}

func (testClock) Now() time.Time {
	return time.Date(2026, 8, 21, 12, 30, 0, 0, time.UTC)
}
