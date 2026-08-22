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
