package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

func TestCoordinationAndAuthorityVerticalSlice(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "coordination.db"))
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
		{Actor: work.Actor{ID: "agent:observer", Kind: work.ActorTypeAgent, DisplayName: "Other research agent"}, IdempotencyKey: "register-observer"},
	} {
		if _, err := service.RegisterActor(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	item := createReadyResearchItem(t, ctx, service)
	if _, err := service.AssignActorCapability(ctx, app.AssignActorCapabilityCommand{
		ActorID: "agent:researcher", Capability: "research", Description: "Can conduct source research.", GrantedBy: "human:owner", IdempotencyKey: "capability-research",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AssignActorCapability(ctx, app.AssignActorCapabilityCommand{
		ActorID: "agent:observer", Capability: "research", Description: "Can conduct source research.", GrantedBy: "human:owner", IdempotencyKey: "capability-observer",
	}); err != nil {
		t.Fatal(err)
	}
	ready, err := service.ListReadyWorkForActor(ctx, "agent:researcher")
	if err != nil {
		t.Fatal(err)
	}
	assertReadyKeys(t, ready, item.Key)

	claimCommand := app.ClaimWorkItemCommand{
		WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "claim-research",
		LeaseDuration: time.Hour, TransitionToInProgress: true,
	}
	claimResult, err := service.ClaimWorkItem(ctx, claimCommand)
	if err != nil {
		t.Fatal(err)
	}
	replayedClaim, err := service.ClaimWorkItem(ctx, claimCommand)
	if err != nil {
		t.Fatal(err)
	}
	if replayedClaim.Claim.ID != claimResult.Claim.ID || replayedClaim.WorkItem.Version != claimResult.WorkItem.Version {
		t.Fatalf("claim replay = %#v, original = %#v", replayedClaim, claimResult)
	}
	item = claimResult.WorkItem
	contextAfterClaim, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(contextAfterClaim.Claims) != 1 || contextAfterClaim.Claims[0].ActorID != "agent:researcher" {
		t.Fatalf("claims = %#v", contextAfterClaim.Claims)
	}
	ready, err = service.ListReadyWorkForActor(ctx, "agent:researcher")
	if err != nil {
		t.Fatal(err)
	}
	assertReadyKeys(t, ready)

	progress, err := service.AppendProgress(ctx, app.AppendProgressCommand{
		WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "progress-research",
		Summary: "Collected the source set.", Completed: []string{"identified primary sources"}, Remaining: []string{"publish the dossier"},
	})
	if err != nil {
		t.Fatal(err)
	}
	item = progress.WorkItem
	evidence, err := service.AttachArtifact(ctx, app.AttachArtifactCommand{
		WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "attach-evidence",
		Kind: "document", URI: "throughline://research/evidence-dossier", Title: "Evidence dossier", Metadata: json.RawMessage(`{"format":"markdown"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	item = evidence.WorkItem

	subject := json.RawMessage(`{"action_type":"knowledge.publish","target":{"collection":"research-library"},"arguments":[],"scope":{"workspace":"local"},"permissions":["knowledge.write"],"credential_requirements":[],"constraints":{"audience":"internal"}}`)
	actionResult, err := service.ProposeExternalAction(ctx, app.ProposeExternalActionCommand{
		WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "propose-publish",
		Required: true, Title: "Publish the research dossier", Rationale: "The work item explicitly requires this external publication record.", Subject: subject,
	})
	if err != nil {
		t.Fatal(err)
	}
	item.Version++
	approval, err := service.RequestExternalActionApproval(ctx, app.RequestExternalActionApprovalCommand{
		ActionID: actionResult.Action.ID, ActorID: "agent:researcher", ExpectedActionVersion: actionResult.Action.Version,
		ExpectedSubjectHash: actionResult.Revision.AuthorizationSubjectHash,
		IdempotencyKey:      "request-publish", ApprovedForActorID: "agent:researcher", Constraints: json.RawMessage(`{"audience":"internal"}`),
		ExpiresAt: ptrTime(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), Request: "Authorize this exact internal publication.",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := service.ResolveExternalActionApproval(ctx, app.ResolveExternalActionApprovalCommand{
		ApprovalID: approval.ID, ActorID: "human:owner", ExpectedActionVersion: actionResult.Action.Version,
		IdempotencyKey: "resolve-publish", Decision: authority.ApprovalApproved, Rationale: "The scope and audience are appropriate.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Grant == nil || resolution.Action.State != authority.ActionAuthorized {
		t.Fatalf("approval resolution = %#v", resolution)
	}
	decision, err := service.CheckActionAuthorization(ctx, app.CheckActionAuthorizationQuery{
		ActionID: resolution.Action.ID, ActorID: "agent:researcher", SubjectHash: actionResult.Revision.AuthorizationSubjectHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Authorized || decision.GrantID != resolution.Grant.ID {
		t.Fatalf("authorization decision = %#v", decision)
	}
	_, err = service.StartExternalActionExecution(ctx, app.StartExternalActionExecutionCommand{
		ActionID: resolution.Action.ID, ActorID: "agent:observer", ExpectedActionVersion: resolution.Action.Version,
		IdempotencyKey: "start-publish-observer", SubjectHash: actionResult.Revision.AuthorizationSubjectHash, AuthorityGrantID: resolution.Grant.ID,
	})
	var authorizationError app.AuthorizationError
	if !errors.As(err, &authorizationError) || authorizationError.Decision.Denial == nil || authorizationError.Decision.Denial.Reason != authority.DenialPrincipalMismatch {
		t.Fatalf("capable but unauthorized actor error = %v", err)
	}
	started, err := service.StartExternalActionExecution(ctx, app.StartExternalActionExecutionCommand{
		ActionID: resolution.Action.ID, ActorID: "agent:researcher", ExpectedActionVersion: resolution.Action.Version,
		IdempotencyKey: "start-publish", SubjectHash: actionResult.Revision.AuthorizationSubjectHash, AuthorityGrantID: resolution.Grant.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.CompleteExternalActionExecution(ctx, app.CompleteExternalActionExecutionCommand{
		ExecutionID: started.Execution.ID, ActorID: "agent:researcher", ExpectedActionVersion: started.Action.Version,
		IdempotencyKey: "complete-publish", State: authority.ExecutionSucceeded, Result: json.RawMessage(`{"status":"published"}`), EvidenceArtifactID: evidence.Artifact.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Action.State != authority.ActionSucceeded || completed.Execution.State != authority.ExecutionSucceeded {
		t.Fatalf("completed action = %#v", completed)
	}

	for _, target := range []work.ExecutionStatus{work.StatusReview, work.StatusDone} {
		item, err = service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{
			WorkItemID: item.ID, TargetStatus: target, ActorID: "agent:researcher", Reason: "The research dossier is complete.", ExpectedVersion: item.Version, IdempotencyKey: "advance-coordination-" + string(target),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := service.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Progress) != 1 || len(loaded.Artifacts) != 1 || len(loaded.ExternalActions) != 1 || len(loaded.ExternalActions[0].Grants) != 1 || len(loaded.ExternalActions[0].Executions) != 1 {
		t.Fatalf("structured coordination context = %#v", loaded)
	}
	if loaded.ExternalActions[0].Executions[0].State != authority.ExecutionSucceeded || loaded.WorkItem.ExecutionStatus != work.StatusDone {
		t.Fatalf("final coordination state = %#v", loaded)
	}
	for name, statement := range map[string]string{
		"action revision update":      "UPDATE external_action_revisions SET proposed_by = 'agent:researcher' WHERE external_action_id = '" + completed.Action.ID + "'",
		"action revision delete":      "DELETE FROM external_action_revisions WHERE external_action_id = '" + completed.Action.ID + "'",
		"execution terminal mutation": "UPDATE external_action_executions SET state = 'failed' WHERE id = '" + completed.Execution.ID + "'",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := database.db.ExecContext(ctx, statement); err == nil {
				t.Fatal("expected SQLite append-only or immutable guard")
			}
		})
	}
	if _, err := database.db.ExecContext(ctx, "UPDATE authority_grants SET revoked_by = 'human:owner', revoked_at = '2030-01-01T00:00:00Z' WHERE id = ?", resolution.Grant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, "UPDATE authority_grants SET revoked_at = NULL WHERE id = ?", resolution.Grant.ID); err == nil {
		t.Fatal("expected a revoked authority grant to remain revoked")
	}
	staleSubject := json.RawMessage(`{"action_type":"knowledge.publish","target":{"collection":"research-library","version":"v1"},"arguments":[],"scope":{"workspace":"local"},"permissions":["knowledge.write"],"credential_requirements":[],"constraints":{"audience":"internal"}}`)
	staleAction, err := service.ProposeExternalAction(ctx, app.ProposeExternalActionCommand{WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "propose-stale-grant", Required: false, Title: "Publish a revised dossier", Rationale: "Verifies revision-bound authority.", Subject: staleSubject})
	if err != nil {
		t.Fatal(err)
	}
	item.Version++
	staleApproval, err := service.RequestExternalActionApproval(ctx, app.RequestExternalActionApprovalCommand{ActionID: staleAction.Action.ID, ActorID: "agent:researcher", ExpectedActionVersion: staleAction.Action.Version, ExpectedSubjectHash: staleAction.Revision.AuthorizationSubjectHash, IdempotencyKey: "request-stale-grant", ApprovedForActorID: "agent:researcher", Constraints: json.RawMessage(`{"audience":"internal"}`), Request: "Authorize this exact dossier publication."})
	if err != nil {
		t.Fatal(err)
	}
	staleResolution, err := service.ResolveExternalActionApproval(ctx, app.ResolveExternalActionApprovalCommand{ApprovalID: staleApproval.ID, ActorID: "human:owner", ExpectedActionVersion: staleAction.Action.Version, IdempotencyKey: "resolve-stale-grant", Decision: authority.ApprovalApproved, Rationale: "The first payload is bounded."})
	if err != nil {
		t.Fatal(err)
	}
	staleAction, err = service.ReviseExternalAction(ctx, app.ReviseExternalActionCommand{ActionID: staleResolution.Action.ID, ActorID: "agent:researcher", ExpectedActionVersion: staleResolution.Action.Version, ExpectedWorkItemVersion: item.Version, IdempotencyKey: "revise-stale-grant", Subject: json.RawMessage(`{"action_type":"knowledge.publish","target":{"collection":"research-library","version":"v2"},"arguments":[],"scope":{"workspace":"local"},"permissions":["knowledge.write"],"credential_requirements":[],"constraints":{"audience":"internal"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	item.Version++
	staleDecision, err := service.CheckActionAuthorization(ctx, app.CheckActionAuthorizationQuery{ActionID: staleAction.Action.ID, ActorID: "agent:researcher", SubjectHash: staleAction.Revision.AuthorizationSubjectHash})
	if err != nil {
		t.Fatal(err)
	}
	if staleDecision.Authorized {
		t.Fatalf("persisted prior grant authorized revised action: %#v", staleDecision)
	}

	_, err = service.AppendProgress(ctx, app.AppendProgressCommand{
		WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "complete-publish",
		Summary: "A mismatched request must not replay.",
	})
	if !errors.Is(err, ports.ErrIdempotencyMismatch) {
		t.Fatalf("mismatched idempotency key error = %v", err)
	}
}

func createReadyResearchItem(t *testing.T, ctx context.Context, service *app.Service) work.WorkItem {
	t.Helper()
	objective, err := service.CreateObjective(ctx, app.CreateObjectiveCommand{
		ActorID: "human:owner", IdempotencyKey: "create-objective-coordination", Key: "OBJ-COORDINATION", Title: "Publish a research dossier", DesiredOutcome: "A reviewed, internally published dossier.", Phase: work.ObjectivePlanning,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.ProposePlan(ctx, app.ProposePlanCommand{
		ObjectiveID: objective.ID, ActorID: "agent:planner", IdempotencyKey: "propose-plan-coordination", Title: "Research and publish", Revision: 1,
		Items: []app.ProposedWorkItem{{
			ClientRef: "dossier", Key: "TH-COORDINATION", Title: "Research the dossier", Kind: "research", Priority: work.PriorityHigh,
			EstimatedScope: work.ScopeMedium, ExecutionPolicy: work.PolicyAgentMayPropose, RequiredActorKind: work.ActorAgent, RequiredCapabilities: []string{"research"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReviewPlan(ctx, app.ReviewPlanCommand{PlanID: plan.Plan.ID, ReviewerActorID: "human:owner", IdempotencyKey: "review-plan-coordination", Decision: work.PlanApproved, Reason: "The plan is ready.", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TransitionObjective(ctx, app.TransitionObjectiveCommand{ObjectiveID: objective.ID, TargetPhase: work.ObjectiveExecution, ActorID: "human:owner", IdempotencyKey: "transition-objective-coordination", Reason: "Start the approved plan.", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	item := plan.Items[0].WorkItem
	item, err = service.TransitionWorkItem(ctx, app.TransitionWorkItemCommand{WorkItemID: item.ID, TargetStatus: work.StatusReady, ActorID: "human:owner", Reason: "Ready for a research agent.", ExpectedVersion: item.Version + 1, IdempotencyKey: "ready-coordination"})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func ptrTime(value time.Time) *time.Time { return &value }

func TestConcurrentAgentsCannotBothClaimWorkItem(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "claim-race.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ids := &concurrentIDs{}
	serviceOne := app.NewService(first.Store(), ids, testClock{})
	serviceTwo := app.NewService(second.Store(), ids, testClock{})
	for _, command := range []app.RegisterActorCommand{
		{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Research owner"}, IdempotencyKey: "race-register-owner"},
		{Actor: work.Actor{ID: "agent:one", Kind: work.ActorTypeAgent, DisplayName: "First agent"}, IdempotencyKey: "race-register-one"},
		{Actor: work.Actor{ID: "agent:two", Kind: work.ActorTypeAgent, DisplayName: "Second agent"}, IdempotencyKey: "race-register-two"},
	} {
		if _, err := serviceOne.RegisterActor(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	item := createReadyResearchItem(t, ctx, serviceOne)
	for _, actorID := range []string{"agent:one", "agent:two"} {
		if _, err := serviceOne.AssignActorCapability(ctx, app.AssignActorCapabilityCommand{
			ActorID: actorID, Capability: "research", Description: "Can conduct source research.", GrantedBy: "human:owner", IdempotencyKey: "race-capability-" + actorID,
		}); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	errorsByActor := make(chan error, 2)
	for index, service := range []*app.Service{serviceOne, serviceTwo} {
		actorID := []string{"agent:one", "agent:two"}[index]
		go func(service *app.Service, actorID string) {
			<-start
			_, err := service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{
				WorkItemID: item.ID, ActorID: actorID, ExpectedVersion: item.Version, IdempotencyKey: "race-claim-" + actorID,
				LeaseDuration: time.Hour, TransitionToInProgress: true,
			})
			errorsByActor <- err
		}(service, actorID)
	}
	close(start)
	firstError := <-errorsByActor
	secondError := <-errorsByActor
	if (firstError == nil) == (secondError == nil) {
		t.Fatalf("concurrent claims returned %v and %v; want exactly one success", firstError, secondError)
	}
	loaded, err := serviceOne.GetWorkItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Claims) != 1 || loaded.WorkItem.ExecutionStatus != work.StatusInProgress {
		t.Fatalf("claim race state = %#v", loaded)
	}
}

func TestClaimUniquenessMapsToConflict(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "claim-conflict.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})
	for _, command := range []app.RegisterActorCommand{
		{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Research owner"}, IdempotencyKey: "conflict-register-owner"},
		{Actor: work.Actor{ID: "agent:one", Kind: work.ActorTypeAgent, DisplayName: "First agent"}, IdempotencyKey: "conflict-register-one"},
		{Actor: work.Actor{ID: "agent:two", Kind: work.ActorTypeAgent, DisplayName: "Second agent"}, IdempotencyKey: "conflict-register-two"},
	} {
		if _, err := service.RegisterActor(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	item := createReadyResearchItem(t, ctx, service)
	firstClaim, err := work.NewClaim("claim-one", item.ID, "agent:one", time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Store().WithinTransaction(ctx, func(repository ports.Repository) error {
		return repository.CreateClaim(ctx, firstClaim)
	}); err != nil {
		t.Fatal(err)
	}
	secondClaim, err := work.NewClaim("claim-two", item.ID, "agent:two", time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	err = database.Store().WithinTransaction(ctx, func(repository ports.Repository) error {
		return repository.CreateClaim(ctx, secondClaim)
	})
	if !errors.Is(err, ports.ErrClaimConflict) {
		t.Fatalf("claim conflict error = %v", err)
	}
}

func TestClaimRequiresNoOpenBlockerAndMatchingExecutionApproval(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "claim-gates.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service := app.NewService(database.Store(), &planningIDs{}, &planningClock{})
	for _, command := range []app.RegisterActorCommand{
		{Actor: work.Actor{ID: "human:owner", Kind: work.ActorTypeHuman, DisplayName: "Research owner"}, IdempotencyKey: "gates-register-owner"},
		{Actor: work.Actor{ID: "agent:researcher", Kind: work.ActorTypeAgent, DisplayName: "Researcher"}, IdempotencyKey: "gates-register-researcher"},
	} {
		if _, err := service.RegisterActor(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	item := createReadyResearchItem(t, ctx, service)
	if _, err := service.AssignActorCapability(ctx, app.AssignActorCapabilityCommand{ActorID: "agent:researcher", Capability: "research", Description: "Can conduct source research.", GrantedBy: "human:owner", IdempotencyKey: "gates-capability"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AskQuestion(ctx, app.AskQuestionCommand{ObjectiveID: item.ObjectiveID, WorkItemID: item.ID, ActorID: "agent:researcher", IdempotencyKey: "ask-question-coordination", Question: "Is the source scope complete?"}); err != nil {
		t.Fatal(err)
	}
	_, err = service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "claim-blocked", LeaseDuration: time.Hour, TransitionToInProgress: true})
	var gateError app.ClaimGateError
	if !errors.As(err, &gateError) || len(gateError.Requirements) == 0 || gateError.Requirements[0].Code != work.ClaimRequirementNoBlockers {
		t.Fatalf("blocked claim error = %#v", err)
	}

	if _, err := database.db.ExecContext(ctx, "UPDATE questions SET status = 'answered' WHERE work_item_id = ?", item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, "UPDATE work_items SET execution_policy = 'approval_required', version = version + 1 WHERE id = ?", item.ID); err != nil {
		t.Fatal(err)
	}
	item.Version++
	ready, err := service.ListReadyWorkForActor(ctx, "agent:researcher")
	if err != nil {
		t.Fatal(err)
	}
	assertReadyKeys(t, ready)
	if _, err := service.ApproveWorkItemExecution(ctx, app.ApproveWorkItemExecutionCommand{WorkItemID: item.ID, ActorID: "human:owner", ApprovedForID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "approve-gated-work", Request: "Approve the bounded research run.", Rationale: "The scope is ready for execution."}); err != nil {
		t.Fatal(err)
	}
	item.Version++
	ready, err = service.ListReadyWorkForActor(ctx, "agent:researcher")
	if err != nil {
		t.Fatal(err)
	}
	assertReadyKeys(t, ready, item.Key)
	if _, err := service.ClaimWorkItem(ctx, app.ClaimWorkItemCommand{WorkItemID: item.ID, ActorID: "agent:researcher", ExpectedVersion: item.Version, IdempotencyKey: "claim-approved", LeaseDuration: time.Hour, TransitionToInProgress: true}); err != nil {
		t.Fatal(err)
	}
}

type concurrentIDs struct {
	mu   sync.Mutex
	next int
}

func (ids *concurrentIDs) New() (string, error) {
	ids.mu.Lock()
	defer ids.mu.Unlock()
	ids.next++
	return time.Date(2026, 8, 22, 12, 0, ids.next, 0, time.UTC).Format("20060102T150405.000000000"), nil
}
