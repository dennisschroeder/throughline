package authority

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestExternalActionRevisionAndMetadataRules(t *testing.T) {
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.FixedZone("test", 2*60*60))
	action, revision, err := NewExternalAction(ExternalAction{
		ID: " action-1 ", WorkItemID: " item-1 ", Title: " Install browser MCP ", Rationale: " Needed for research. ", Required: true,
	}, subject("2.1.0"), " agent:planner ", now)
	if err != nil {
		t.Fatal(err)
	}
	if action.ID != "action-1" || action.ActionType != "tool.install" || action.CurrentRevision != 1 || action.State != ActionProposed || action.Version != 1 {
		t.Fatalf("unexpected action: %#v", action)
	}
	if !bytes.Equal(revision.AuthorizationSubject, mustCanonicalSubject(t, subject("2.1.0"))) || revision.AuthorizationSubjectHash == "" || revision.ProposedAt.Location() != time.UTC {
		t.Fatalf("unexpected revision: %#v", revision)
	}

	metadata, err := UpdateExternalActionMetadata(action, " Install browser MCP for research ", " Revised rationale. ", " human:owner ", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.CurrentRevision != 1 || metadata.ActionType != action.ActionType || metadata.Version != 2 || metadata.Title != "Install browser MCP for research" {
		t.Fatalf("metadata update changed more than metadata: %#v", metadata)
	}

	if _, _, err := ReviseExternalAction(metadata, revision, subject("2.1.0"), "agent:planner", now); err == nil {
		t.Fatal("expected unchanged subject to reject a new revision")
	}
	revised, next, err := ReviseExternalAction(metadata, revision, subject("2.2.0"), "agent:planner", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if revised.CurrentRevision != 2 || revised.State != ActionProposed || revised.Version != 3 || next.Revision != 2 || next.AuthorizationSubjectHash == revision.AuthorizationSubjectHash {
		t.Fatalf("unexpected revised action: %#v, %#v", revised, next)
	}

	executing, err := TransitionExternalAction(revised, ActionAuthorized, now)
	if err != nil {
		t.Fatal(err)
	}
	executing, err = TransitionExternalAction(executing, ActionExecuting, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReviseExternalAction(executing, next, subject("2.3.0"), "agent:planner", now); err == nil {
		t.Fatal("expected executing action to reject revision")
	}
}

func TestExternalActionStateTransitions(t *testing.T) {
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	action := ExternalAction{ID: "action-1", WorkItemID: "item-1", ActionType: "tool.install", Title: "Install", CurrentRevision: 1, State: ActionProposed, Version: 1}
	for _, target := range []ExternalActionState{ActionAuthorized, ActionExecuting, ActionSucceeded} {
		var err error
		action, err = TransitionExternalAction(action, target, now)
		if err != nil {
			t.Fatalf("transition to %s: %v", target, err)
		}
	}
	if _, err := TransitionExternalAction(action, ActionFailed, now); err == nil {
		t.Fatal("expected terminal action to reject another transition")
	}
	if _, err := TransitionExternalAction(ExternalAction{State: ActionAuthorized}, ActionRejected, now); err == nil {
		t.Fatal("expected authorized action to reject direct rejection")
	}
}

func TestAuthorityGrantAuthorizationAndOneWayRevocation(t *testing.T) {
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	action, revision, err := NewExternalAction(ExternalAction{ID: "action-1", WorkItemID: "item-1", Title: "Install"}, subject("2.1.0"), "agent:planner", now)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := now.Add(time.Hour)
	grant, err := NewAuthorityGrant(AuthorityGrant{
		ID: "grant-1", PrincipalActorID: "agent:installer", SourceApprovalID: "approval-1", GrantedBy: "human:owner",
		Constraints: json.RawMessage(`{"global_install":false}`), ExpiresAt: &expiresAt,
	}, revision, now)
	if err != nil {
		t.Fatal(err)
	}
	if grant.ExternalActionID != action.ID || grant.ActionRevision != 1 || grant.AuthorizationSubjectHash != revision.AuthorizationSubjectHash || !grant.ActiveAt(now) {
		t.Fatalf("unexpected grant: %#v", grant)
	}
	action, err = TransitionExternalAction(action, ActionAuthorized, now)
	if err != nil {
		t.Fatal(err)
	}

	assertDenied(t, CheckAuthorization(action, revision, nil, "agent:installer", revision.AuthorizationSubjectHash, now), DenialApprovalRequired)
	assertDenied(t, CheckAuthorization(action, revision, &grant, "agent:other", revision.AuthorizationSubjectHash, now), DenialPrincipalMismatch)
	assertDenied(t, CheckAuthorization(action, revision, &grant, "agent:installer", "wrong", now), DenialSubjectMismatch)
	assertDenied(t, CheckAuthorization(ExternalAction{ID: action.ID, CurrentRevision: 2}, revision, &grant, "agent:installer", revision.AuthorizationSubjectHash, now), DenialApprovalStale)

	decision := CheckAuthorization(action, revision, &grant, "agent:installer", revision.AuthorizationSubjectHash, now)
	if !decision.Authorized || decision.GrantID != grant.ID || decision.Denial != nil {
		t.Fatalf("expected authorization, got %#v", decision)
	}

	revoked, err := RevokeAuthorityGrant(grant, "human:owner", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if revoked.ActiveAt(now.Add(2 * time.Minute)) {
		t.Fatal("revoked grant remained active")
	}
	if _, err := RevokeAuthorityGrant(revoked, "human:owner", now.Add(2*time.Minute)); err == nil {
		t.Fatal("expected revocation to be one-way")
	}
	assertDenied(t, CheckAuthorization(action, revision, &revoked, "agent:installer", revision.AuthorizationSubjectHash, now), DenialGrantRevoked)
	assertDenied(t, CheckAuthorization(action, revision, &grant, "agent:installer", revision.AuthorizationSubjectHash, expiresAt), DenialGrantExpired)
}

func TestAuthorityGrantRequiresExactSubjectConstraints(t *testing.T) {
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	_, revision, err := NewExternalAction(ExternalAction{ID: "action-1", WorkItemID: "item-1", Title: "Install"}, subject("2.1.0"), "agent:planner", now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewAuthorityGrant(AuthorityGrant{
		ID: "grant-1", PrincipalActorID: "agent:installer", SourceApprovalID: "approval-1", GrantedBy: "human:owner",
		Constraints: json.RawMessage(`{"global_install":true}`),
	}, revision, now)
	if err == nil {
		t.Fatal("expected mismatched constraints to reject the grant")
	}
}

func TestPayloadRevisionInvalidatesPriorGrant(t *testing.T) {
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	action, first, err := NewExternalAction(ExternalAction{ID: "action-1", WorkItemID: "item-1", Title: "Install"}, subject("2.1.0"), "agent:planner", now)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := NewAuthorityGrant(AuthorityGrant{
		ID: "grant-1", PrincipalActorID: "agent:installer", SourceApprovalID: "approval-1", GrantedBy: "human:owner",
		Constraints: json.RawMessage(`{"global_install":false}`),
	}, first, now)
	if err != nil {
		t.Fatal(err)
	}
	action, err = TransitionExternalAction(action, ActionAuthorized, now)
	if err != nil {
		t.Fatal(err)
	}
	action, second, err := ReviseExternalAction(action, first, subject("2.2.0"), "agent:planner", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if decision := CheckAuthorization(action, second, &grant, "agent:installer", second.AuthorizationSubjectHash, now.Add(time.Minute)); decision.Authorized {
		t.Fatalf("prior grant authorized revised payload: %#v", decision)
	}
}

func TestActionApprovalBindsAndResolvesExactRevision(t *testing.T) {
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	_, revision, err := NewExternalAction(ExternalAction{ID: "action-1", WorkItemID: "item-1", Title: "Install"}, subject("2.1.0"), "agent:planner", now)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := NewActionApproval(ActionApproval{
		ID: "approval-1", ApprovedForActorID: "agent:installer", Request: "Install the approved connector.",
		RequestedBy: "agent:planner", Constraints: json.RawMessage(`{"global_install":false}`),
	}, revision, now)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != ApprovalRequested || approval.AuthorizationSubjectHash != revision.AuthorizationSubjectHash {
		t.Fatalf("unexpected approval: %#v", approval)
	}
	resolved, err := ResolveActionApproval(approval, ApprovalApproved, "human:owner", "Approved for the named actor.", now)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != ApprovalApproved || resolved.ResolvedAt == nil {
		t.Fatalf("unexpected resolved approval: %#v", resolved)
	}
}

func TestExternalActionExecutionValidation(t *testing.T) {
	now := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	action, revision, err := NewExternalAction(ExternalAction{ID: "action-1", WorkItemID: "item-1", Title: "Install"}, subject("2.1.0"), "agent:planner", now)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := NewExternalActionExecution("execution-1", action, revision, "agent:installer", "grant-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if execution.State != ExecutionStarted || execution.FinishedAt != (time.Time{}) {
		t.Fatalf("unexpected execution: %#v", execution)
	}
	if _, err := CompleteExternalActionExecution(execution, ExecutionSucceeded, json.RawMessage(`{"installed_version":"2.1.0"}`), nil, now); err == nil {
		t.Fatal("expected success without evidence to reject")
	}
	completed, err := CompleteExternalActionExecution(execution, ExecutionSucceeded, json.RawMessage(`{"installed_version":"2.1.0"}`), []string{" artifact-1 "}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != ExecutionSucceeded || string(completed.Result) != `{"installed_version":"2.1.0"}` || len(completed.EvidenceIDs) != 1 || completed.EvidenceIDs[0] != "artifact-1" {
		t.Fatalf("unexpected completed execution: %#v", completed)
	}
	if _, err := CompleteExternalActionExecution(completed, ExecutionFailed, json.RawMessage(`{"reason":"late"}`), nil, now); err == nil {
		t.Fatal("expected terminal execution to reject a second completion")
	}
}

func subject(version string) []byte {
	return []byte(`{
        "action_type": "tool.install",
        "target": {"package": "browser-mcp", "version": "` + version + `"},
        "arguments": [],
        "scope": {"environment": "project"},
        "permissions": ["network.read", "config.write:project"],
        "credential_requirements": [],
        "constraints": {"global_install": false}
    }`)
}

func mustCanonicalSubject(t *testing.T, raw []byte) []byte {
	t.Helper()
	canonical, err := CanonicalizeSubject(raw)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func assertDenied(t *testing.T, decision AuthorizationDecision, reason AuthorizationDenialReason) {
	t.Helper()
	if decision.Authorized || decision.Denial == nil || decision.Denial.Reason != reason {
		t.Fatalf("expected denial %q, got %#v", reason, decision)
	}
}
