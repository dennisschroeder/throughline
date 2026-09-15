package work

import (
	"encoding/json"
	"strings"
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
	objective, err := NewObjective("objective-1", "OBJ-1", "Design a skill", "", "Reviewed skill package", ObjectivePlanning, PriorityMedium, now)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := TransitionObjective(objective, ObjectivePaused, "Awaiting sponsor review.", "human:sponsor", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if paused.PriorPhase != ObjectivePlanning || paused.Version != 2 {
		t.Fatalf("unexpected paused objective: %#v", paused)
	}
	resumed, err := TransitionObjective(paused, ObjectivePlanning, "Sponsor review completed.", "human:sponsor", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Phase != ObjectivePlanning || resumed.PriorPhase != "" || resumed.Version != 3 {
		t.Fatalf("unexpected resumed objective: %#v", resumed)
	}
	if _, err := TransitionObjective(objective, ObjectiveCompleted, "skip", "human:sponsor", now.Add(time.Hour)); err == nil {
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

// TestContextNonGoalAndAffectedFollowTheProposalLifecycle is REP-06's second
// criterion at the domain layer: both new kinds share the exact
// proposed -> accepted -> waived lifecycle requirement, constraint and risk
// already use, not a parallel one that could drift from it.
func TestContextNonGoalAndAffectedFollowTheProposalLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	for _, kind := range []ContextKind{ContextNonGoal, ContextAffected} {
		record, err := NewContextRecord(ContextRecord{
			ID: "context-" + string(kind), ObjectiveID: "objective-1", Kind: kind,
			Title: "Scoped explicitly", Status: ContextProposed, CreatedBy: "agent:planner",
		}, now)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		accepted, err := TransitionContextRecord(record, ContextAccepted, "agent:reviewer", now.Add(time.Hour))
		if err != nil {
			t.Fatalf("%s: proposed->accepted: %v", kind, err)
		}
		waived, err := TransitionContextRecord(accepted, ContextWaived, "agent:reviewer", now.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("%s: accepted->waived: %v", kind, err)
		}
		if waived.Status != ContextWaived {
			t.Fatalf("%s: status = %q, want waived", kind, waived.Status)
		}
		// Neither kind may skip straight to accepted or waived without having
		// been proposed and then accepted first, the same guard every kind on
		// this lifecycle enforces.
		if _, err := TransitionContextRecord(record, ContextWaived, "agent:reviewer", now.Add(time.Hour)); err == nil {
			t.Fatalf("%s: proposed->waived succeeded, want the accepted step required", kind)
		}
	}
}

func TestQuestionFrontierLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	base := Question{ID: "q", ObjectiveID: "objective", WorkItemID: " item-own ", Text: "Retention and export", CreatedBy: "human:owner",
		BlocksWorkItems: []string{"item-b", " item-own", "", "item-b"}}

	question, err := NewQuestion(base, now)
	if err != nil {
		t.Fatal(err)
	}
	if question.Status != QuestionOpen || question.AttentionState != AttentionNone {
		t.Fatalf("defaults = %s/%s, want open/none", question.Status, question.AttentionState)
	}
	if got := strings.Join(question.BlocksWorkItems, ","); got != "item-own,item-b" {
		t.Fatalf("blocked items = %q, want the question's own item first, then each other item once", got)
	}
	for _, invalid := range []Question{
		func() Question { q := base; q.Status = QuestionAnswered; return q }(),
		func() Question { q := base; q.AttentionState = "flagged"; return q }(),
	} {
		if _, err := NewQuestion(invalid, now); err == nil {
			t.Fatalf("NewQuestion accepted %s/%s", invalid.Status, invalid.AttentionState)
		}
	}

	unsharp := base
	unsharp.Status = QuestionUnsharp
	unsharp, err = NewQuestion(unsharp, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AnswerQuestion(unsharp, "Too early.", "human:owner", now); err == nil {
		t.Fatal("an unsharp question was answered")
	}
	if _, err := SharpenQuestion(unsharp, "  "); err == nil {
		t.Fatal("an unsharp question was sharpened without its phrasing")
	}
	if _, err := SharpenQuestion(question, "Already open"); err == nil {
		t.Fatal("an open question was sharpened")
	}
	sharpened, err := SharpenQuestion(unsharp, " Does export extend retention? ")
	if err != nil {
		t.Fatal(err)
	}
	if sharpened.Status != QuestionOpen || sharpened.Text != "Does export extend retention?" || sharpened.Version != unsharp.Version+1 {
		t.Fatalf("sharpened = %#v", sharpened)
	}
	if waived, err := WaiveQuestion(unsharp, "Out of scope.", "human:owner", now); err != nil || waived.Status != QuestionWaived {
		t.Fatalf("waiving an unsharp question = %#v, %v", waived, err)
	}

	linked, err := LinkQuestionBlocker(question, " item-c ")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(linked.BlocksWorkItems, ","); got != "item-own,item-b,item-c" || linked.Version != question.Version+1 {
		t.Fatalf("linked = %q v%d", got, linked.Version)
	}
	if got := strings.Join(question.BlocksWorkItems, ","); got != "item-own,item-b" {
		t.Fatalf("linking mutated the original question's links: %q", got)
	}
	if _, err := LinkQuestionBlocker(linked, "item-c"); err == nil {
		t.Fatal("the same item was linked twice")
	}
	answered, err := AnswerQuestion(question, "Yes.", "human:owner", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LinkQuestionBlocker(answered, "item-d"); err == nil {
		t.Fatal("an answered question was linked as a blocker")
	}
	if QuestionAnswered.Unresolved() || QuestionWaived.Unresolved() || !QuestionUnsharp.Unresolved() || !QuestionOpen.Unresolved() {
		t.Fatal("Unresolved does not match the blocking states")
	}
}

// TestQuestionDecodesRecordsStoredBeforeAttentionState covers idempotency
// responses written before REP-08, which carry RequiresHumanAttention and no
// AttentionState; replaying one must not yield an invalid empty state.
func TestQuestionDecodesRecordsStoredBeforeAttentionState(t *testing.T) {
	for _, testCase := range []struct {
		stored string
		want   AttentionState
	}{
		{`{"ID":"q","Status":"open","RequiresHumanAttention":true}`, AttentionNeedsHumanDecision},
		{`{"ID":"q","Status":"open","RequiresHumanAttention":false}`, AttentionNone},
		{`{"ID":"q","Status":"open"}`, AttentionNone},
		{`{"ID":"q","Status":"open","AttentionState":"needs_human_review","RequiresHumanAttention":true}`, AttentionNeedsHumanReview},
	} {
		var question Question
		if err := json.Unmarshal([]byte(testCase.stored), &question); err != nil {
			t.Fatal(err)
		}
		if question.AttentionState != testCase.want || question.ID != "q" || question.Status != QuestionOpen {
			t.Fatalf("%s decoded to %#v, want attention %s", testCase.stored, question, testCase.want)
		}
	}
	encoded, err := json.Marshal(Question{ID: "q", Status: QuestionUnsharp, AttentionState: AttentionNeedsClarification, BlocksWorkItems: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Question
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.AttentionState != AttentionNeedsClarification || roundTrip.Status != QuestionUnsharp || len(roundTrip.BlocksWorkItems) != 1 {
		t.Fatalf("round trip = %#v", roundTrip)
	}
}

func TestTransitionObjectiveRecordsItsEdgeReasonAndActor(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.FixedZone("test", 3600))
	objective := Objective{ID: "o", Key: "OBJ", Title: "T", Phase: ObjectiveIdea, Priority: PriorityMedium, Version: 1}
	if _, err := TransitionObjective(objective, ObjectiveDiscovery, "Worth exploring.", " ", now); err == nil {
		t.Fatal("a transition without an actor was accepted")
	}
	transitioned, err := TransitionObjective(objective, ObjectiveDiscovery, " Worth exploring. ", " human:owner ", now)
	if err != nil {
		t.Fatal(err)
	}
	want := PhaseTransition{From: ObjectiveIdea, To: ObjectiveDiscovery, Reason: "Worth exploring.", ActorID: "human:owner", At: now.UTC()}
	if transitioned.LastPhaseTransition == nil || *transitioned.LastPhaseTransition != want || transitioned.UpdatedBy != "human:owner" {
		t.Fatalf("transition = %#v, updated by %q; want %#v", transitioned.LastPhaseTransition, transitioned.UpdatedBy, want)
	}
	if objective.LastPhaseTransition != nil {
		t.Fatal("the original objective was modified")
	}
}
