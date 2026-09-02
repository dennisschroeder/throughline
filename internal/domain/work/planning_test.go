package work

import (
	"testing"
	"time"
)

func TestNewContextRecordValidatesKindLifecycle(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	record, err := NewContextRecord(ContextRecord{
		ID:          "context-1",
		ObjectiveID: "objective-1",
		Kind:        ContextAssumption,
		Title:       "Interview notes are representative",
		Body:        "Validate against a second source set.",
		Status:      ContextUntested,
		Confidence:  "medium",
		CreatedBy:   "agent:planner",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != 1 || !record.CreatedAt.Equal(now) || !record.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected audit fields: %#v", record)
	}

	_, err = NewContextRecord(ContextRecord{
		ID:          "context-2",
		ObjectiveID: "objective-1",
		Kind:        ContextFinding,
		Title:       "Invalid finding state",
		Status:      ContextValidated,
		CreatedBy:   "agent:planner",
	}, now)
	if err == nil {
		t.Fatal("expected finding with assumption status to be rejected")
	}
}

func TestTransitionContextRecordFollowsKindLifecycle(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	assumption, err := NewContextRecord(ContextRecord{
		ID: "context-1", ObjectiveID: "objective-1", Kind: ContextAssumption,
		Title: "Three sources are representative", Status: ContextUntested, CreatedBy: "agent:planner",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	validating, err := TransitionContextRecord(assumption, ContextValidating, "agent:researcher", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	invalidated, err := TransitionContextRecord(validating, ContextInvalidated, "agent:researcher", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if invalidated.Status != ContextInvalidated || invalidated.Version != 3 || invalidated.UpdatedBy != "agent:researcher" {
		t.Fatalf("unexpected transitioned assumption: %#v", invalidated)
	}
	if _, err := TransitionContextRecord(assumption, ContextAccepted, "agent:researcher", now.Add(time.Hour)); err == nil {
		t.Fatal("expected cross-lifecycle context transition to be rejected")
	}
}

func TestReviewPlanCommitsOnlyProposedPlan(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	plan, err := NewPlan("plan-1", "objective-1", "Research and skill plan", "", 1, PlanProposed, now)
	if err != nil {
		t.Fatal(err)
	}
	plan.ProposedBy = "agent:planner"
	plan.ProposedAt = now

	approved, err := ReviewPlan(plan, PlanApproved, "human:reviewer", "Scope and outputs are clear.", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if approved.CommitmentState != PlanApproved || approved.Version != 2 || approved.ResolvedBy != "human:reviewer" {
		t.Fatalf("unexpected approved plan: %#v", approved)
	}
	if _, err := ReviewPlan(approved, PlanRejected, "human:reviewer", "changed mind", now.Add(2*time.Hour)); err == nil {
		t.Fatal("expected resolved plan to reject a second review")
	}
	if _, err := NewPlanApproval("", approved); err == nil {
		t.Fatal("expected plan approval without an id to be rejected")
	}
	approved.ProposedBy = ""
	if _, err := NewPlanApproval("approval-1", approved); err == nil {
		t.Fatal("expected plan approval without proposal audit to be rejected")
	}
}

func TestObjectivePhaseTransitionPausesAndResumesPriorPhase(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	objective, err := NewObjective("objective-1", "OBJ-1", "Design a skill", "", "Reviewed skill package", ObjectivePlanning, now)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := TransitionObjective(objective, ObjectivePaused, "Awaiting sponsor review.", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if paused.PriorPhase != ObjectivePlanning || paused.Version != 2 {
		t.Fatalf("unexpected paused objective: %#v", paused)
	}
	resumed, err := TransitionObjective(paused, ObjectivePlanning, "Sponsor review completed.", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Phase != ObjectivePlanning || resumed.PriorPhase != "" || resumed.Version != 3 {
		t.Fatalf("unexpected resumed objective: %#v", resumed)
	}
	if _, err := TransitionObjective(objective, ObjectiveCompleted, "skip", now.Add(time.Hour)); err == nil {
		t.Fatal("expected invalid phase skip to be rejected")
	}
}

func TestContextSuccessMetricAcceptedStatuses(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		status  ContextStatus
		wantErr bool
		errMsg  string
	}{
		{ContextUntested, false, ""},
		{ContextValidating, false, ""},
		{ContextValidated, false, ""},
		{ContextInvalidated, false, ""},
		{ContextSuperseded, false, ""},
		{ContextWaived, false, ""},
		{ContextProposed, true, "proposed status should be rejected"},
		{ContextAccepted, true, "accepted status should be rejected"},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			_, err := NewContextRecord(ContextRecord{
				ID:          "metric-1",
				ObjectiveID: "objective-1",
				Kind:        ContextSuccessMetric,
				Title:       "Response time < 100ms",
				Status:      tt.status,
				CreatedBy:   "agent:planner",
			}, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewContextRecord returned err=%v, want err=%v (%s)", err != nil, tt.wantErr, tt.errMsg)
			}
		})
	}
}

func TestContextSuccessMetricValidTransitions(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		from    ContextStatus
		to      ContextStatus
		allowed bool
	}{
		{ContextUntested, ContextValidating, true},
		{ContextValidating, ContextValidated, true},
		{ContextValidating, ContextInvalidated, true},
		{ContextUntested, ContextWaived, true},
		{ContextValidating, ContextWaived, true},
		{ContextUntested, ContextValidated, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			metric, err := NewContextRecord(ContextRecord{
				ID:          "metric-1",
				ObjectiveID: "objective-1",
				Kind:        ContextSuccessMetric,
				Title:       "Success metric",
				Status:      tt.from,
				CreatedBy:   "agent:planner",
			}, now)
			if err != nil {
				t.Fatalf("NewContextRecord failed: %v", err)
			}
			_, err = TransitionContextRecord(metric, tt.to, "agent:tester", now.Add(time.Hour))
			if (err != nil) != !tt.allowed {
				t.Fatalf("transition %s->%s: got err=%v, want allowed=%v", tt.from, tt.to, err != nil, tt.allowed)
			}
		})
	}
}

func TestContextRequirementUnchanged(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	req, err := NewContextRecord(ContextRecord{
		ID:          "req-1",
		ObjectiveID: "objective-1",
		Kind:        ContextRequirement,
		Title:       "Must support concurrent users",
		Status:      ContextProposed,
		CreatedBy:   "agent:planner",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := TransitionContextRecord(req, ContextAccepted, "agent:reviewer", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	waived, err := TransitionContextRecord(accepted, ContextWaived, "agent:reviewer", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if waived.Status != ContextWaived {
		t.Fatalf("requirement should still support proposed->accepted->waived lifecycle, got %s", waived.Status)
	}
}
