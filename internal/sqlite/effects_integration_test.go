package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// TestBulkPlanApprovalReportsEveryAcceptedEntity covers the bulk path: one
// review_plan transaction commits the plan and every item it accepted, so the
// mutation must name all of them with the versions it actually committed.
func TestBulkPlanApprovalReportsEveryAcceptedEntity(t *testing.T) {
	ctx := context.Background()
	service := newEffectsService(t, "bulk-plan.db")
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-bulk-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "bulk-objective", Key: "OBJ-BULK", Title: "Approve a plan in one write", DesiredOutcome: "Every accepted item is reported.", Phase: work.ObjectivePlanning}))
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "bulk-plan", Title: "Three-item plan", Revision: 1,
		Items: []app.ProposedWorkItem{
			{ClientRef: "one", Key: "TH-BULK-1", Title: "First", Kind: "research", Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAny},
			{ClientRef: "two", Key: "TH-BULK-2", Title: "Second", Kind: "research", Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAny},
			{ClientRef: "three", Key: "TH-BULK-3", Title: "Third", Kind: "research", Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAny},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := proposal.Result
	wantProposal := map[string]int{"plan:" + plan.Plan.ID: 1}
	for _, item := range plan.Items {
		wantProposal["work_item:"+item.WorkItem.ID] = 1
	}
	assertEffects(t, proposal.Effects, wantProposal)

	approval, err := service.ReviewPlan(ctx, app.ReviewPlanCommand{PlanID: plan.Plan.ID, ReviewerActorID: "human:owner", IdempotencyKey: "bulk-review", Decision: work.PlanApproved, Reason: "All three items are committed together.", ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Approving the plan also records the reviewer's approval, which is a
	// governed entity of its own and therefore an effect.
	wantApproval := map[string]int{"plan:" + plan.Plan.ID: 2}
	for _, item := range plan.Items {
		wantApproval["work_item:"+item.WorkItem.ID] = 2
	}
	approvalEffects := 0
	for _, effect := range approval.Effects {
		if effect.Kind == "approval" {
			approvalEffects++
			wantApproval["approval:"+effect.ID] = 1
		}
	}
	if approvalEffects != 1 {
		t.Fatalf("plan approval produced %d approval effects, want 1: %#v", approvalEffects, approval.Effects)
	}
	assertEffects(t, approval.Effects, wantApproval)
	assertEffectOrderStable(t, approval.Effects)

	// The plan is reported before the items it accepted, and the items keep the
	// order the approved plan declared them in.
	if approval.Effects[0].Kind != "plan" || approval.Effects[0].ID != plan.Plan.ID {
		t.Fatalf("first approval effect = %#v, want the plan", approval.Effects[0])
	}
	for index, item := range plan.Items {
		effect := approval.Effects[index+1]
		if effect.Kind != "work_item" || effect.ID != item.WorkItem.ID {
			t.Fatalf("approval effect %d = %#v, want work item %s", index+1, effect, item.WorkItem.ID)
		}
	}
}

// TestOutputProfileActivationReportsSupersededPredecessor covers supersession:
// activating a new version also rewrites the version it replaces, and both
// carry their committed state versions.
func TestOutputProfileActivationReportsSupersededPredecessor(t *testing.T) {
	ctx := context.Background()
	service := newEffectsService(t, "profile-supersession.db")
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-profile-owner"})); err != nil {
		t.Fatal(err)
	}
	predecessor := findProfileVersion(t, ctx, service, "research_dossier", 1)
	proposed, err := service.ProposeOutputProfile(ctx, app.ProposeOutputProfileCommand{
		ActorID: "human:owner", IdempotencyKey: "propose-dossier-v2", Name: "research_dossier", Version: 2,
		Description: "Source-linked research result with explicit uncertainty.",
		Structure:   json.RawMessage(`{"required":["question","method","findings","sources","uncertainty","conclusion"]}`),
		Semantics:   json.RawMessage(`{"claims_require_provenance":true}`),
		Validation:  json.RawMessage(`{"required":[{"kind":"structure"},{"kind":"provenance"}]}`),
		Supersedes:  "research_dossier/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEffects(t, proposed.Effects, map[string]int{"output_profile:" + proposed.Result.ID: 1})

	activated, err := service.ReviewOutputProfile(ctx, app.ReviewOutputProfileCommand{
		ProfileID: proposed.Result.ID, ReviewerActorID: "human:owner", IdempotencyKey: "activate-dossier-v2",
		ExpectedVersion: proposed.Result.StateVersion, Decision: output.ProfileActive, Reason: "The new version narrows nothing.",
	})
	if err != nil {
		t.Fatal(err)
	}
	superseded := findProfileVersion(t, ctx, service, "research_dossier", 1)
	if superseded.LifecycleState != output.ProfileSuperseded {
		t.Fatalf("predecessor lifecycle = %q, want superseded", superseded.LifecycleState)
	}
	assertEffects(t, activated.Effects, map[string]int{
		"output_profile:" + proposed.Result.ID: activated.Result.StateVersion,
		"output_profile:" + predecessor.ID:     superseded.StateVersion,
	})
	assertEffectOrderStable(t, activated.Effects)
}

// TestUnlinkDependencyReportsLastPersistedVersion covers a delete: the effect
// must carry the version the row actually had, never a version invented by
// incrementing past the delete.
func TestUnlinkDependencyReportsLastPersistedVersion(t *testing.T) {
	ctx := context.Background()
	service := newEffectsService(t, "unlink-dependency.db")
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-unlink-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "unlink-objective", Key: "OBJ-UNLINK", Title: "Remove an edge", DesiredOutcome: "A delete reports the version it deleted.", Phase: work.ObjectivePlanning}))
	if err != nil {
		t.Fatal(err)
	}
	first, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{ActorID: "human:owner", IdempotencyKey: "unlink-first", Key: "TH-UNLINK-1", ObjectiveID: objective.ID, Title: "First", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog, Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{ActorID: "human:owner", IdempotencyKey: "unlink-second", Key: "TH-UNLINK-2", ObjectiveID: objective.ID, Title: "Second", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog, Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone}))
	if err != nil {
		t.Fatal(err)
	}
	linked, err := service.LinkDependency(ctx, app.LinkDependencyCommand{WorkItemID: second.ID, DependsOnWorkItemID: first.ID, Kind: work.DependencyHard, ActorID: "human:owner", ExpectedVersion: second.Version, IdempotencyKey: "unlink-link"})
	if err != nil {
		t.Fatal(err)
	}
	dependencyID := linked.Result.ID
	assertEffects(t, linked.Effects, map[string]int{
		"dependency:" + dependencyID: 1,
		"work_item:" + second.ID:     second.Version + 1,
	})

	unlinked, err := service.UnlinkDependency(ctx, app.UnlinkDependencyCommand{WorkItemID: second.ID, DependsOnWorkItemID: first.ID, Kind: work.DependencyHard, ActorID: "human:owner", ExpectedVersion: second.Version + 1, IdempotencyKey: "unlink-remove"})
	if err != nil {
		t.Fatal(err)
	}
	assertEffects(t, unlinked.Effects, map[string]int{
		"dependency:" + dependencyID: 1,
		"work_item:" + second.ID:     second.Version + 2,
	})
	assertEffectOrderStable(t, unlinked.Effects)
}

// TestRevokingActionApprovalRaisesTheActionVersion covers the authority path:
// revocation ends the action's authority, so the action changes too and the
// mutation must say so.
func TestRevokingActionApprovalRaisesTheActionVersion(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "revoke-approval.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})
	for _, command := range []app.RegisterActorCommand{
		{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Research owner"}, IdempotencyKey: "register-owner"},
		{Actor: work.Actor{ID: "agent:researcher", Kind: work.ActorTypeAgent, DisplayName: "Research agent"}, IdempotencyKey: "register-researcher"},
	} {
		if _, err := app.UnwrapMutation(service.RegisterActor(ctx, command)); err != nil {
			t.Fatal(err)
		}
	}
	item := createReadyResearchItem(t, ctx, service)
	subject := json.RawMessage(`{"action_type":"knowledge.publish","target":{"collection":"research-library"},"arguments":[],"scope":{"workspace":"local"},"permissions":["knowledge.write"],"credential_requirements":[],"constraints":{"audience":"internal"}}`)
	action, err := app.UnwrapMutation(service.ProposeExternalAction(ctx, app.ProposeExternalActionCommand{
		WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "revoke-propose",
		Required: true, Title: "Publish the research dossier", Rationale: "The item requires an external publication record.", Subject: subject,
	}))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := app.UnwrapMutation(service.RequestExternalActionApproval(ctx, app.RequestExternalActionApprovalCommand{
		ActionID: action.Action.ID, ActorID: "agent:researcher", ExpectedActionVersion: action.Action.Version,
		ExpectedSubjectHash: action.Revision.AuthorizationSubjectHash, IdempotencyKey: "revoke-request",
		ApprovedForActorID: "agent:researcher", Constraints: json.RawMessage(`{"audience":"internal"}`),
		ExpiresAt: ptrTime(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), Request: "Authorize this exact internal publication.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	granted, err := app.UnwrapMutation(service.ResolveExternalActionApproval(ctx, app.ResolveExternalActionApprovalCommand{
		ApprovalID: approval.ID, ActorID: "human:owner", ExpectedActionVersion: action.Action.Version,
		IdempotencyKey: "revoke-resolve", Decision: authority.ApprovalApproved, Rationale: "The scope is appropriate.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if granted.Grant == nil {
		t.Fatal("approval did not create a grant")
	}

	revoked, err := service.RevokeExternalActionApproval(ctx, app.RevokeExternalActionApprovalCommand{
		ApprovalID: approval.ID, ActorID: "human:owner", ExpectedActionVersion: granted.Action.Version,
		IdempotencyKey: "revoke-grant", Rationale: "The publication audience changed.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Result.Approval.Version != granted.Approval.Version+1 {
		t.Fatalf("approval version after revocation = %d, want %d", revoked.Result.Approval.Version, granted.Approval.Version+1)
	}
	if revoked.Result.Action.Version != granted.Action.Version+1 {
		t.Fatalf("action version after revocation = %d, want %d", revoked.Result.Action.Version, granted.Action.Version+1)
	}
	if revoked.Result.Action.State == authority.ActionAuthorized {
		t.Fatal("revoking the only grant left the action advertised as authorized")
	}
	assertEffects(t, revoked.Effects, map[string]int{
		"action_approval:" + approval.ID:       granted.Approval.Version + 1,
		"authority_grant:" + granted.Grant.ID:  granted.Grant.Version + 1,
		"external_action:" + granted.Action.ID: granted.Action.Version + 1,
	})
	assertEffectOrderStable(t, revoked.Effects)

	decision, err := service.CheckActionAuthorization(ctx, app.CheckActionAuthorizationQuery{
		ActionID: granted.Action.ID, ActorID: "agent:researcher", SubjectHash: action.Revision.AuthorizationSubjectHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Authorized {
		t.Fatal("authorization survived the revocation")
	}
}

// TestEffectOrderIsIdenticalAcrossRuns runs the same mutation twice against two
// fresh databases: an unordered map iteration anywhere in a write path would
// show up here as a different effect sequence.
func TestEffectOrderIsIdenticalAcrossRuns(t *testing.T) {
	ctx := context.Background()
	run := func(name string) []app.Effect {
		service := newEffectsService(t, name)
		if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-order-owner"})); err != nil {
			t.Fatal(err)
		}
		objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "order-objective", Key: "OBJ-ORDER", Title: "Stable effect order", DesiredOutcome: "Effects arrive in one order.", Phase: work.ObjectivePlanning}))
		if err != nil {
			t.Fatal(err)
		}
		created, err := service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
			ActorID: "human:owner", IdempotencyKey: "order-item", Key: "TH-ORDER", ObjectiveID: objective.ID,
			Title: "Ordered graph", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose,
			RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone,
			RequiredCapabilities: []string{"zeta_capability", "alpha_capability", "mu_capability"},
			AcceptanceCriteria: []app.ProposedAcceptanceCriterion{
				{Text: "First criterion.", Required: true, Ordinal: 1},
				{Text: "Second criterion.", Required: true, Ordinal: 2},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return created.Effects
	}
	first, second := run("order-a.db"), run("order-b.db")
	if len(first) != len(second) {
		t.Fatalf("effect counts differ: %d and %d", len(first), len(second))
	}
	// IDs are deterministic here, so comparing only kind and version would let
	// any permutation among same-kind, same-version effects pass unnoticed.
	if !slices.Equal(first, second) {
		t.Fatalf("effect order differs:\nrun a: %#v\nrun b: %#v", first, second)
	}
	assertEffectOrderStable(t, first)
}

func findProfileVersion(t *testing.T, ctx context.Context, service *app.Service, name string, version int) output.Profile {
	t.Helper()
	profiles, err := service.ListOutputProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range profiles {
		if profile.Name == name && profile.Version == version {
			return profile
		}
	}
	t.Fatalf("output profile %s/v%d is not persisted", name, version)
	return output.Profile{}
}

func newEffectsService(t *testing.T, name string) *app.Service {
	t.Helper()
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return app.NewService(database.Store(), &testIDs{}, testClock{})
}

// assertEffectOrderStable rejects a duplicate entity in one effect list: the
// collector reports one entry per entity, carrying the version it committed
// last, not one entry per write.
func assertEffectOrderStable(t *testing.T, effects []app.Effect) {
	t.Helper()
	seen := make(map[string]bool, len(effects))
	for _, effect := range effects {
		key := effect.Kind + ":" + effect.ID
		if seen[key] {
			t.Fatalf("effect %q appears more than once in %#v", key, effects)
		}
		seen[key] = true
		if effect.Version < 1 {
			t.Fatalf("effect %q has version %d", key, effect.Version)
		}
		if effect.Kind == "" || effect.ID == "" {
			t.Fatalf("effect %#v is missing kind or id", effect)
		}
	}
}

// TestLegacyReplayThatCannotBeReconstructedFailsWithoutASecondWrite covers the
// upgrade fixture for an operation whose transaction touched more than the
// entity it returned. Guessing an incomplete effect set would be worse than
// failing, and re-running the mutation would be worse still.
func TestLegacyReplayThatCannotBeReconstructedFailsWithoutASecondWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-replay.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-legacy-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "legacy-objective", Key: "OBJ-LEGACY", Title: "Refuse an unupgradable replay", DesiredOutcome: "An old response never re-runs a mutation.", Phase: work.ObjectivePlanning}))
	if err != nil {
		t.Fatal(err)
	}
	command := app.CreateWorkItemCommand{
		ActorID: "human:owner", IdempotencyKey: "legacy-item", Key: "TH-LEGACY", ObjectiveID: objective.ID,
		Title: "Has collateral rows", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
		Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose,
		RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone,
		AcceptanceCriteria: []app.ProposedAcceptanceCriterion{{Text: "The criterion is collateral.", Required: true, Ordinal: 1}},
	}
	created, err := service.CreateWorkItem(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Effects) < 2 {
		t.Fatalf("create effects = %#v, want the item and its criterion", created.Effects)
	}
	stripEffectsFromIdempotencyRecord(t, ctx, database, command.ActorID, command.IdempotencyKey)

	var itemsBefore, criteriaBefore int
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM work_items").Scan(&itemsBefore); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM acceptance_criteria").Scan(&criteriaBefore); err != nil {
		t.Fatal(err)
	}

	if _, err := service.CreateWorkItem(ctx, command); !errors.Is(err, app.ErrLegacyIdempotencyReplay) {
		t.Fatalf("legacy replay error = %v, want ErrLegacyIdempotencyReplay", err)
	}

	var itemsAfter, criteriaAfter int
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM work_items").Scan(&itemsAfter); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM acceptance_criteria").Scan(&criteriaAfter); err != nil {
		t.Fatal(err)
	}
	if itemsAfter != itemsBefore || criteriaAfter != criteriaBefore {
		t.Fatalf("refused replay still wrote: items %d -> %d, criteria %d -> %d", itemsBefore, itemsAfter, criteriaBefore, criteriaAfter)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	// The refusal survives a restart rather than depending on process memory.
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	restarted := app.NewService(reopened.Store(), &testIDs{fail: true}, testClock{})
	if _, err := restarted.CreateWorkItem(ctx, command); !errors.Is(err, app.ErrLegacyIdempotencyReplay) {
		t.Fatalf("legacy replay after restart = %v, want ErrLegacyIdempotencyReplay", err)
	}
}

// stripEffectsFromIdempotencyRecord rewrites a stored response into the shape
// the previous binary would have written: no top-level effects member, and none
// of the named result fields, which is how a version column introduced by this
// change looks in a response that predates it.
func stripEffectsFromIdempotencyRecord(t *testing.T, ctx context.Context, database *Database, actorID, key string, dropResultFields ...string) {
	t.Helper()
	var response string
	if err := database.db.QueryRowContext(ctx, "SELECT response_json FROM idempotency_records WHERE actor_id = ? AND key = ?", actorID, key).Scan(&response); err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal([]byte(response), &stored); err != nil {
		t.Fatal(err)
	}
	delete(stored, "effects")
	if result, ok := stored["result"].(map[string]any); ok {
		for _, field := range dropResultFields {
			delete(result, field)
		}
	}
	legacy, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, "UPDATE idempotency_records SET response_json = ? WHERE actor_id = ? AND key = ?", legacy, actorID, key); err != nil {
		t.Fatal(err)
	}
}

// TestApprovalKindsStayDistinguishable covers the one collected table that holds
// three different governed entities. A caller filtering effects by kind must be
// able to tell a plan approval from a work-item execution approval from an
// external-action approval.
func TestApprovalKindsStayDistinguishable(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "approval-kinds.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})
	for _, command := range []app.RegisterActorCommand{
		{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-kinds-owner"},
		{Actor: work.Actor{ID: "agent:researcher", Kind: work.ActorTypeAgent, DisplayName: "Researcher"}, IdempotencyKey: "register-kinds-researcher"},
	} {
		if _, err := app.UnwrapMutation(service.RegisterActor(ctx, command)); err != nil {
			t.Fatal(err)
		}
	}
	item := createReadyResearchItem(t, ctx, service)

	execution, err := service.ApproveWorkItemExecution(ctx, app.ApproveWorkItemExecutionCommand{
		WorkItemID: item.ID, ActorID: "human:owner", ApprovedForID: "agent:researcher", ExpectedVersion: item.Version,
		IdempotencyKey: "kinds-execution-approval", Request: "Approve the bounded research run.", Rationale: "The scope is ready.",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertHasEffect(t, execution.Effects, "execution_approval", execution.Result.ID)

	subject := json.RawMessage(`{"action_type":"knowledge.publish","target":{"collection":"research-library"},"arguments":[],"scope":{"workspace":"local"},"permissions":["knowledge.write"],"credential_requirements":[],"constraints":{}}`)
	action, err := app.UnwrapMutation(service.ProposeExternalAction(ctx, app.ProposeExternalActionCommand{
		WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version + 1,
		IdempotencyKey: "kinds-propose", Required: true, Title: "Publish the dossier", Rationale: "An external record is required.", Subject: subject,
	}))
	if err != nil {
		t.Fatal(err)
	}
	actionApproval, err := service.RequestExternalActionApproval(ctx, app.RequestExternalActionApprovalCommand{
		ActionID: action.Action.ID, ActorID: "agent:researcher", ExpectedActionVersion: action.Action.Version,
		ExpectedSubjectHash: action.Revision.AuthorizationSubjectHash, IdempotencyKey: "kinds-request",
		ApprovedForActorID: "agent:researcher", Constraints: json.RawMessage(`{}`), Request: "Authorize this exact publication.",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertHasEffect(t, actionApproval.Effects, "action_approval", actionApproval.Result.ID)
}

func assertHasEffect(t *testing.T, effects []app.Effect, kind, id string) {
	t.Helper()
	for _, effect := range effects {
		if effect.Kind == kind && effect.ID == id {
			if effect.Version < 1 {
				t.Fatalf("effect %s:%s has version %d", kind, id, effect.Version)
			}
			return
		}
	}
	t.Fatalf("effects %#v do not contain %s:%s", effects, kind, id)
}

// TestCapabilityRemovalOrderIsIdenticalAcrossRuns pins the one write loop that
// removes several rows at once. Deleting in Go map order returned the same set
// of effects in a different sequence on every call, which makes any consumer
// that diffs or golden-tests a receipt flaky.
func TestCapabilityRemovalOrderIsIdenticalAcrossRuns(t *testing.T) {
	ctx := context.Background()
	run := func(name string) []string {
		service := newEffectsService(t, name)
		if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-removal-owner"})); err != nil {
			t.Fatal(err)
		}
		objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "removal-objective", Key: "OBJ-REMOVAL", Title: "Remove several capabilities", DesiredOutcome: "Removal order is stable.", Phase: work.ObjectivePlanning}))
		if err != nil {
			t.Fatal(err)
		}
		item, err := app.UnwrapMutation(service.CreateWorkItem(ctx, app.CreateWorkItemCommand{
			ActorID: "human:owner", IdempotencyKey: "removal-item", Key: "TH-REMOVAL", ObjectiveID: objective.ID,
			Title: "Has five capabilities", Kind: "research", CommitmentState: work.ItemProposed, ExecutionStatus: work.StatusBacklog,
			Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall, ExecutionPolicy: work.PolicyAgentMayPropose,
			RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone,
			RequiredCapabilities: []string{"alpha", "beta", "gamma", "delta", "epsilon"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		kept := []string{"alpha"}
		patched, err := service.PatchWorkItem(ctx, app.PatchWorkItemCommand{
			WorkItemID: item.ID, ActorID: "human:owner", IdempotencyKey: "removal-patch",
			ExpectedVersion: item.Version, RequiredCapabilities: &kept,
		})
		if err != nil {
			t.Fatal(err)
		}
		var removed []string
		for _, effect := range patched.Effects {
			if effect.Kind == "work_item_capability" {
				removed = append(removed, effect.ID)
			}
		}
		if len(removed) != 4 {
			t.Fatalf("removal effects = %#v, want the four dropped capabilities", patched.Effects)
		}
		return removed
	}
	first := run("removal-a.db")
	for index := 0; index < 6; index++ {
		again := run(fmt.Sprintf("removal-b-%d.db", index))
		if !slices.Equal(first, again) {
			t.Fatalf("capability removal effect order is unstable:\nrun 0: %v\nrun %d: %v", first, index+1, again)
		}
	}
}

// TestEffectCollectorSurvivesRolledBackWritesAndConnectionLoss pins both halves
// of the collector's lifecycle: it is installed once by Migrate so a failed
// write does not tear it down, and a transaction still rebuilds it if it is
// missing, because the driver may hand out a replacement connection.
func TestEffectCollectorSurvivesRolledBackWritesAndConnectionLoss(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "collector-lifecycle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	installed := func() int {
		t.Helper()
		var count int
		if err := database.db.QueryRowContext(ctx, `
SELECT count(*) FROM temp.sqlite_master
WHERE (type = 'table' AND name = 'mutation_effects')
   OR (type = 'trigger' AND name LIKE 'effects\_%' ESCAPE '\')`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if got := installed(); got != effectObjectCount {
		t.Fatalf("collector objects after Migrate = %d, want %d", got, effectObjectCount)
	}

	// A write transaction that fails must not take the collector down with it.
	failure := errors.New("deliberate rollback")
	if err := database.Store().WithinTransaction(ctx, func(ports.Repository) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("rolled back transaction error = %v", err)
	}
	if got := installed(); got != effectObjectCount {
		t.Fatalf("collector objects after a rolled back write = %d, want %d", got, effectObjectCount)
	}

	// Losing the collector is still recoverable without restarting the process.
	if _, err := database.db.ExecContext(ctx, "DROP TABLE temp.mutation_effects"); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	created, err := service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "collector-owner"})
	if err != nil {
		t.Fatal(err)
	}
	assertEffects(t, created.Effects, map[string]int{"actor:human:owner": 1})
	if got := installed(); got != effectObjectCount {
		t.Fatalf("collector objects after recovery = %d, want %d", got, effectObjectCount)
	}
}

// TestLegacyReplayOfSupersedingContextRefusesRatherThanUnderreporting pins the
// boundary of the legacy upgrade. record_context writes one row on its own and
// two when it supersedes a predecessor, and the predecessor's committed version
// is not derivable from the stored result. Returning the single reconstructible
// effect would be a wrong answer, which is worse than the refusal the design
// chose for everything else it cannot rebuild.
func TestLegacyReplayOfSupersedingContextRefusesRatherThanUnderreporting(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "legacy-context.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-context-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "context-objective", Key: "OBJ-CONTEXT", Title: "Supersede a requirement", DesiredOutcome: "A superseding record reports both rows.", Phase: work.ObjectivePlanning}))
	if err != nil {
		t.Fatal(err)
	}
	plain := app.RecordContextCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "context-first",
		Kind: work.ContextRequirement, Title: "The first requirement", Body: "Original wording.", Status: work.ContextAccepted,
	}
	first, err := service.RecordContext(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	assertEffects(t, first.Effects, map[string]int{"context_record:" + first.Result.ID: 1})

	superseding := app.RecordContextCommand{
		ObjectiveID: objective.ID, ActorID: "human:owner", IdempotencyKey: "context-second",
		Kind: work.ContextRequirement, Title: "The revised requirement", Body: "Revised wording.", Status: work.ContextAccepted,
		SupersedesID: first.Result.ID,
	}
	second, err := service.RecordContext(ctx, superseding)
	if err != nil {
		t.Fatal(err)
	}
	// The live mutation reports both rows, including the predecessor's new version.
	assertEffects(t, second.Effects, map[string]int{
		"context_record:" + second.Result.ID: 1,
		"context_record:" + first.Result.ID:  first.Result.Version + 1,
	})

	// A response written before effects existed can still be replayed for the
	// plain case, because that one is exactly reconstructible.
	stripEffectsFromIdempotencyRecord(t, ctx, database, plain.ActorID, plain.IdempotencyKey)
	replayedPlain, err := service.RecordContext(ctx, plain)
	if err != nil {
		t.Fatalf("plain legacy replay = %v, want the reconstructed effect", err)
	}
	assertEffects(t, replayedPlain.Effects, map[string]int{"context_record:" + first.Result.ID: 1})

	// The superseding case must refuse instead of under-reporting.
	stripEffectsFromIdempotencyRecord(t, ctx, database, superseding.ActorID, superseding.IdempotencyKey)
	var records int
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM context_records").Scan(&records); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordContext(ctx, superseding); !errors.Is(err, app.ErrLegacyIdempotencyReplay) {
		t.Fatalf("superseding legacy replay = %v, want ErrLegacyIdempotencyReplay", err)
	}
	var recordsAfter int
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM context_records").Scan(&recordsAfter); err != nil {
		t.Fatal(err)
	}
	if recordsAfter != records {
		t.Fatalf("refused replay wrote context records: %d -> %d", records, recordsAfter)
	}
}

// TestRevokingApprovalDuringExecutionStillRevokes pins the boundary of the
// revocation change. An approval stays approved while its action executes, so
// revoking authority mid-flight is reachable, and 'executing' has no edge to
// 'expired'. Refusing the revocation there would be the worst outcome: the
// grant would stay valid precisely when someone is trying to withdraw it.
func TestRevokingApprovalDuringExecutionStillRevokes(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "revoke-executing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})
	for _, command := range []app.RegisterActorCommand{
		{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "register-exec-owner"},
		{Actor: work.Actor{ID: "agent:researcher", Kind: work.ActorTypeAgent, DisplayName: "Researcher"}, IdempotencyKey: "register-exec-researcher"},
	} {
		if _, err := app.UnwrapMutation(service.RegisterActor(ctx, command)); err != nil {
			t.Fatal(err)
		}
	}
	item := createReadyResearchItem(t, ctx, service)
	if _, err := app.UnwrapMutation(service.AssignActorCapability(ctx, app.AssignActorCapabilityCommand{
		ActorID: "agent:researcher", Capability: "research", Description: "Can conduct source research.",
		GrantedBy: "human:owner", IdempotencyKey: "exec-capability",
	})); err != nil {
		t.Fatal(err)
	}
	subject := json.RawMessage(`{"action_type":"knowledge.publish","target":{"collection":"research-library"},"arguments":[],"scope":{"workspace":"local"},"permissions":["knowledge.write"],"credential_requirements":[],"constraints":{}}`)
	action, err := app.UnwrapMutation(service.ProposeExternalAction(ctx, app.ProposeExternalActionCommand{
		WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "exec-propose",
		Required: true, Title: "Publish the dossier", Rationale: "An external record is required.", Subject: subject,
	}))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := app.UnwrapMutation(service.RequestExternalActionApproval(ctx, app.RequestExternalActionApprovalCommand{
		ActionID: action.Action.ID, ActorID: "agent:researcher", ExpectedActionVersion: action.Action.Version,
		ExpectedSubjectHash: action.Revision.AuthorizationSubjectHash, IdempotencyKey: "exec-request",
		ApprovedForActorID: "agent:researcher", Constraints: json.RawMessage(`{}`), Request: "Authorize this publication.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	granted, err := app.UnwrapMutation(service.ResolveExternalActionApproval(ctx, app.ResolveExternalActionApprovalCommand{
		ApprovalID: approval.ID, ActorID: "human:owner", ExpectedActionVersion: action.Action.Version,
		IdempotencyKey: "exec-resolve", Decision: authority.ApprovalApproved, Rationale: "The scope is appropriate.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	started, err := app.UnwrapMutation(service.StartExternalActionExecution(ctx, app.StartExternalActionExecutionCommand{
		ActionID: granted.Action.ID, ActorID: "agent:researcher", ExpectedActionVersion: granted.Action.Version,
		IdempotencyKey: "exec-start", SubjectHash: action.Revision.AuthorizationSubjectHash, AuthorityGrantID: granted.Grant.ID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if started.Action.State != authority.ActionExecuting {
		t.Fatalf("action state after start = %q, want executing", started.Action.State)
	}

	revoked, err := service.RevokeExternalActionApproval(ctx, app.RevokeExternalActionApprovalCommand{
		ApprovalID: approval.ID, ActorID: "human:owner", ExpectedActionVersion: started.Action.Version,
		IdempotencyKey: "exec-revoke", Rationale: "The publication audience changed mid-flight.",
	})
	if err != nil {
		t.Fatalf("revoking authority during execution failed: %v", err)
	}
	if revoked.Result.Action.Version != started.Action.Version+1 {
		t.Fatalf("action version after revocation = %d, want %d", revoked.Result.Action.Version, started.Action.Version+1)
	}
	if revoked.Result.Action.State != authority.ActionExecuting {
		t.Fatalf("action state after mid-flight revocation = %q, want the lifecycle left alone", revoked.Result.Action.State)
	}
	assertHasEffect(t, revoked.Effects, "external_action", granted.Action.ID)
	assertHasEffect(t, revoked.Effects, "authority_grant", granted.Grant.ID)
	assertHasEffect(t, revoked.Effects, "action_approval", approval.ID)

	decision, err := service.CheckActionAuthorization(ctx, app.CheckActionAuthorizationQuery{
		ActionID: granted.Action.ID, ActorID: "agent:researcher", SubjectHash: action.Revision.AuthorizationSubjectHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Authorized {
		t.Fatal("authority survived a mid-execution revocation")
	}
}

// TestLegacyReplayRefusesWhenTheVersionFieldItselfIsNewer covers the other half
// of the upgrade fixture. The schema upgrade is one thing; a response written
// before the version field existed is another. register_actor is the case:
// work.Actor gained its Version in this change, so a genuinely old response
// decodes to version zero. Reporting that would be worse than refusing, and the
// CHECK the schema advertises says no effect may carry it.
func TestLegacyReplayRefusesWhenTheVersionFieldItselfIsNewer(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "legacy-actor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	command := app.RegisterActorCommand{
		Actor:          work.Actor{ID: "human:legacy", Kind: work.ActorTypeHuman, DisplayName: "Legacy owner"},
		IdempotencyKey: "legacy-actor",
	}
	created, err := service.RegisterActor(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	assertEffects(t, created.Effects, map[string]int{"actor:human:legacy": 1})

	// The previous binary knew nothing of Actor.Version, so its response carried
	// no such field at all.
	stripEffectsFromIdempotencyRecord(t, ctx, database, command.Actor.ID, command.IdempotencyKey, "Version")

	var actors int
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM actors").Scan(&actors); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterActor(ctx, command); !errors.Is(err, app.ErrLegacyIdempotencyReplay) {
		t.Fatalf("legacy actor replay = %v, want ErrLegacyIdempotencyReplay", err)
	}
	var actorsAfter int
	if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM actors").Scan(&actorsAfter); err != nil {
		t.Fatal(err)
	}
	if actorsAfter != actors {
		t.Fatalf("refused replay wrote actors: %d -> %d", actors, actorsAfter)
	}
}

// TestEveryEntityTableIsCollected gates the first acceptance criterion against
// the schema itself. effectTables is hand-maintained, so without this a merge
// that drops an entry, or a migration that adds a table nobody lists, silently
// stops reporting those entities and no other check notices: the generated
// model's digest does not move for a migration, and every existing test asserts
// only the effects it happens to expect.
func TestEveryEntityTableIsCollected(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "collected.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := database.db.QueryContext(ctx, `
SELECT name FROM main.sqlite_master
WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	// Deliberately uncollected. Activity and idempotency rows are internal
	// records of a mutation, not entities it changed; schema_migrations is the
	// migration ledger.
	uncollected := map[string]string{
		"activity":            "internal record of a mutation, never an effect",
		"idempotency_records": "internal record of a mutation, never an effect",
		"schema_migrations":   "migration ledger, not workspace state",
	}
	collected := make(map[string]bool, len(effectTables))
	for _, table := range effectTables {
		if collected[table.table] {
			t.Fatalf("effectTables lists %q twice", table.table)
		}
		collected[table.table] = true
	}

	var missing []string
	present := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		present[name] = true
		if collected[name] || uncollected[name] != "" {
			continue
		}
		missing = append(missing, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Fatalf("tables in the schema that no mutation would report as an effect: %v\n"+
			"add each to effectTables, or to this test's uncollected map with the reason it is not an entity", missing)
	}
	for _, table := range effectTables {
		if !present[table.table] {
			t.Errorf("effectTables lists %q, which the schema does not define", table.table)
		}
	}
	// An exemption for a table that no longer exists is a stale excuse nobody
	// would notice, so it expires with the table.
	for table := range uncollected {
		if !present[table] {
			t.Errorf("the uncollected map exempts %q, which the schema no longer defines", table)
		}
	}
}

// TestMultiEntityEffectsSurviveARestartUnchanged gates the persisted-effects
// branch of the third criterion. The other restart test strips the effects
// member first and therefore exercises the legacy reconstruction path; this one
// leaves the stored response alone, so the replay must come back through the
// effects the first call actually committed — several entities, in order,
// field for field, and nothing anywhere in the database changed.
func TestMultiEntityEffectsSurviveARestartUnchanged(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart-multi.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &testIDs{}, testClock{})
	if _, err := app.UnwrapMutation(service.RegisterActor(ctx, app.RegisterActorCommand{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Owner"}, IdempotencyKey: "restart-owner"})); err != nil {
		t.Fatal(err)
	}
	objective, err := app.UnwrapMutation(service.CreateObjective(ctx, app.CreateObjectiveCommand{ActorID: "human:owner", IdempotencyKey: "restart-objective", Key: "OBJ-RESTART", Title: "Survive a restart", DesiredOutcome: "Stored effects replay unchanged.", Phase: work.ObjectivePlanning}))
	if err != nil {
		t.Fatal(err)
	}
	command := app.CreateWorkItemCommand{
		ActorID: "human:owner", IdempotencyKey: "restart-item", Key: "TH-RESTART", ObjectiveID: objective.ID,
		Title: "Several entities in one write", Kind: "research", CommitmentState: work.ItemProposed,
		ExecutionStatus: work.StatusBacklog, Priority: work.PriorityMedium, EstimatedScope: work.ScopeSmall,
		ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAny, AttentionState: work.AttentionNone,
		RequiredCapabilities: []string{"alpha_capability", "beta_capability"},
		AcceptanceCriteria: []app.ProposedAcceptanceCriterion{
			{Text: "First criterion.", Required: true, Ordinal: 1},
			{Text: "Second criterion.", Required: true, Ordinal: 2},
		},
	}
	created, err := service.CreateWorkItem(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Effects) < 5 {
		t.Fatalf("create effects = %#v, want the item with its criteria and capabilities", created.Effects)
	}
	before := rowCounts(t, ctx, database)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	database = reopened
	// A failing id generator catches any insert that would allocate an id; the
	// row-count snapshot catches the rest, including an update that changes a
	// version without changing a count and an extra activity row.
	replayService := app.NewService(reopened.Store(), &testIDs{fail: true}, testClock{})
	replayed, err := replayService.CreateWorkItem(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Result.ID != created.Result.ID {
		t.Fatalf("replayed item = %q, want %q", replayed.Result.ID, created.Result.ID)
	}
	if !slices.Equal(replayed.Effects, created.Effects) {
		t.Fatalf("effects changed across restart:\nbefore: %#v\nafter:  %#v", created.Effects, replayed.Effects)
	}
	assertNothingWasWritten(t, before, rowCounts(t, ctx, database))
}

// rowCounts snapshots every collected table plus the two internal ledgers, and
// sums the versions as well as the rows: an update that raises a version without
// changing a count is still a write.
func rowCounts(t *testing.T, ctx context.Context, database *Database) map[string]string {
	t.Helper()
	snapshot := make(map[string]string, len(effectTables)+2)
	for _, table := range effectTables {
		var rows int
		var versions any
		query := fmt.Sprintf("SELECT count(*), COALESCE(sum(%s), 0) FROM %s", strings.TrimPrefix(table.version, "NEW."), table.table)
		if err := database.db.QueryRowContext(ctx, query).Scan(&rows, &versions); err != nil {
			t.Fatalf("snapshot %s: %v", table.table, err)
		}
		snapshot[table.table] = fmt.Sprintf("%d rows, versions summing to %v", rows, versions)
	}
	for _, table := range []string{"activity", "idempotency_records"} {
		var rows int
		if err := database.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		snapshot[table] = fmt.Sprintf("%d rows", rows)
	}
	return snapshot
}

func assertNothingWasWritten(t *testing.T, before, after map[string]string) {
	t.Helper()
	for _, table := range slices.Sorted(maps.Keys(before)) {
		if before[table] != after[table] {
			t.Errorf("replay wrote to %s: %s became %s", table, before[table], after[table])
		}
	}
}

// TestEffectTableExpressionsNameRealColumns closes the other half of the
// completeness gate. Deleting an entry from effectTables is caught; editing one
// was not. A wrong column name fails loudly the moment any mutation runs,
// because the trigger will not compile — but a valid-but-wrong expression, a
// literal version or a truncated composite id, is silent for every table no
// test happens to assert.
func TestEffectTableExpressionsNameRealColumns(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "expressions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range effectTables {
		t.Run(table.table, func(t *testing.T) {
			columns := make(map[string]bool)
			rows, err := database.db.QueryContext(ctx, fmt.Sprintf("SELECT name FROM pragma_table_info(%q)", table.table))
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Fatal(err)
				}
				columns[name] = true
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if len(columns) == 0 {
				t.Fatalf("table %q has no columns", table.table)
			}
			for _, expression := range []struct {
				name string
				sql  string
			}{{"id", table.id}, {"version", table.version}, {"kind", table.kindExpr}} {
				if expression.sql == "" {
					continue
				}
				referenced := newColumnReferences(expression.sql)
				if len(referenced) == 0 {
					t.Errorf("%s expression %q references no column, so it cannot report what actually changed", expression.name, expression.sql)
				}
				for _, column := range referenced {
					if !columns[column] {
						t.Errorf("%s expression %q references %q, which %s does not have", expression.name, expression.sql, column, table.table)
					}
				}
			}
		})
	}
}

// TestEffectTableDefinitionsAreTheReviewedList pins every collected table as a
// whole tuple. The structural check above proves an expression names a real
// column of its own table; it cannot prove it names the right one, and most
// rows are a bare NEW.id that nothing else would notice changing. Swapping
// external_action_revisions' version for its revision number, or collapsing
// progress entries onto their work item id, are both valid SQL against real
// columns and both silently wrong.
//
// The kind strings are part of every mutating tool's response and the composite
// identifiers are named in the contract, so this list is the reviewed statement
// of both and effectTables is a copy of it. Changing either half is then a
// visible, deliberate edit in two places rather than a refactor nobody reads.
func TestEffectTableDefinitionsAreTheReviewedList(t *testing.T) {
	const approvalKinds = "CASE WHEN NEW.external_action_id IS NOT NULL THEN 'action_approval' " +
		"WHEN NEW.work_item_id IS NOT NULL AND NEW.approved_for_actor_id IS NOT NULL THEN 'execution_approval' " +
		"ELSE 'approval' END"
	want := []effectTable{
		{table: "objectives", kind: "objective", id: "NEW.id", version: "NEW.version"},
		{table: "plans", kind: "plan", id: "NEW.id", version: "NEW.version"},
		{table: "work_items", kind: "work_item", id: "NEW.id", version: "NEW.version"},
		{table: "output_profiles", kind: "output_profile", id: "NEW.id", version: "NEW.state_version"},
		{table: "expected_outputs", kind: "expected_output", id: "NEW.id", version: "NEW.version"},
		{table: "context_records", kind: "context_record", id: "NEW.id", version: "NEW.version"},
		{table: "questions", kind: "question", id: "NEW.id", version: "NEW.version"},
		{table: "question_blocks", kind: "question_block", id: "NEW.question_id || ':' || NEW.work_item_id", version: "NEW.version"},
		{table: "decisions", kind: "decision", id: "NEW.id", version: "NEW.version"},
		{table: "approvals", id: "NEW.id", version: "NEW.version", kindExpr: approvalKinds},
		{table: "capabilities", kind: "capability", id: "NEW.slug", version: "NEW.version"},
		{table: "work_item_capabilities", kind: "work_item_capability", id: "NEW.work_item_id || ':' || NEW.capability_slug", version: "NEW.version"},
		{table: "acceptance_criteria", kind: "acceptance_criterion", id: "NEW.id", version: "NEW.version"},
		{table: "dependencies", kind: "dependency", id: "NEW.id", version: "NEW.version"},
		{table: "artifacts", kind: "artifact", id: "NEW.id", version: "NEW.version"},
		{table: "output_revisions", kind: "output_revision", id: "NEW.id", version: "NEW.state_version"},
		{table: "output_revision_artifacts", kind: "output_revision_artifact", id: "NEW.output_revision_id || ':' || NEW.artifact_id", version: "NEW.version"},
		{table: "output_requirements", kind: "output_requirement", id: "NEW.id", version: "NEW.version"},
		{table: "output_validations", kind: "validation_record", id: "NEW.id", version: "NEW.version"},
		{table: "actors", kind: "actor", id: "NEW.id", version: "NEW.version"},
		{table: "actor_capabilities", kind: "actor_capability", id: "NEW.actor_id || ':' || NEW.capability_slug", version: "NEW.version"},
		{table: "claims", kind: "claim", id: "NEW.id", version: "NEW.version"},
		{table: "progress_entries", kind: "progress_entry", id: "NEW.id", version: "NEW.version"},
		{table: "manual_blockers", kind: "manual_blocker", id: "NEW.id", version: "NEW.version"},
		{table: "external_actions", kind: "external_action", id: "NEW.id", version: "NEW.version"},
		{table: "external_action_revisions", kind: "external_action_revision", id: "NEW.external_action_id || ':' || NEW.revision", version: "NEW.version"},
		{table: "authority_grants", kind: "authority_grant", id: "NEW.id", version: "NEW.version"},
		{table: "external_action_executions", kind: "external_action_execution", id: "NEW.id", version: "NEW.version"},
	}
	if len(effectTables) != len(want) {
		t.Fatalf("effectTables has %d entries, the reviewed list has %d", len(effectTables), len(want))
	}
	for index, expected := range want {
		got := effectTables[index]
		if got != expected {
			t.Errorf("entry %d (%s) differs from the reviewed list:\n got:  %+v\n want: %+v", index, expected.table, got, expected)
		}
	}
}

// newColumnReferences returns the column names a trigger expression reads from
// the row being written.
func newColumnReferences(expression string) []string {
	var columns []string
	for _, prefix := range []string{"NEW.", "OLD."} {
		rest := expression
		for {
			index := strings.Index(rest, prefix)
			if index < 0 {
				break
			}
			rest = rest[index+len(prefix):]
			end := 0
			for end < len(rest) && (rest[end] == '_' || (rest[end] >= 'a' && rest[end] <= 'z') || (rest[end] >= 'A' && rest[end] <= 'Z') || (rest[end] >= '0' && rest[end] <= '9')) {
				end++
			}
			if end > 0 {
				columns = append(columns, rest[:end])
			}
		}
	}
	return columns
}
