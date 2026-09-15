package dashboard

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// TestDashboardExposesReviewEvidence is REP-09's second criterion on the
// human surface: an item that declared a review through its plan shows, in
// the drawer and on its card in review, which declared review done waits on
// and in what state.
func TestDashboardExposesReviewEvidence(t *testing.T) {
	h := newTestHarness(t)
	const actorID = "agent:dashboard-worker"
	h.call("register_actor", map[string]any{"actor_id": actorID, "kind": "agent", "display_name": "Dashboard Worker", "idempotency_key": "register"})
	objective := h.call("create_objective", map[string]any{
		"actor_id": actorID, "idempotency_key": "objective", "key": "OBJ-REVIEWED", "title": "Reviewed",
		"desired_outcome": "Outcome", "phase": "planning",
	})["result"].(map[string]any)
	plan := h.call("propose_plan", map[string]any{
		"objective_id": objective["id"], "actor_id": actorID, "idempotency_key": "plan", "title": "Plan", "revision": 1,
		"items": []any{map[string]any{
			"client_ref": "item-1", "key": "REV-1", "title": "Reviewed item", "kind": "research",
			"priority": "medium", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent",
			"review_requirements": []any{map[string]any{"criterion_ref": "code-review", "validator_kind": "human_review"}},
		}},
	})["result"].(map[string]any)
	itemID := plan["items"].([]any)[0].(map[string]any)["work_item"].(map[string]any)["id"].(string)

	h.login("human:reviewer")
	var detail itemDetail
	if resp := h.getJSON("/dashboard/api/v1/item?id="+itemID, &detail); resp.StatusCode != http.StatusOK {
		t.Fatalf("item detail status = %d", resp.StatusCode)
	}
	if len(detail.ReviewEvidence) != 1 || detail.ReviewEvidence[0].CriterionRef != "code-review" || detail.ReviewEvidence[0].ValidatorKind != "human_review" || detail.ReviewEvidence[0].State != "missing" {
		t.Fatalf("drawer review evidence = %+v", detail.ReviewEvidence)
	}

	// A recorded review reaches the drawer with the state the gate derived and
	// the record that decided it, not only the missing default.
	recorded := h.call("record_validation", map[string]any{
		"actor_id": actorID, "idempotency_key": "review", "work_item_id": itemID, "criterion_ref": "code-review",
		"validator_kind": "human_review", "verdict": "failed", "details": map[string]any{"rationale": "Two findings open."}, "degraded": true,
	})["result"].(map[string]any)
	detail = itemDetail{}
	if resp := h.getJSON("/dashboard/api/v1/item?id="+itemID, &detail); resp.StatusCode != http.StatusOK {
		t.Fatalf("item detail status = %d", resp.StatusCode)
	}
	if len(detail.ReviewEvidence) != 1 || detail.ReviewEvidence[0].State != "failed" || detail.ReviewEvidence[0].ValidationRecordID != recorded["id"] || !detail.ReviewEvidence[0].Degraded {
		t.Fatalf("drawer review evidence after a failed degraded review = %+v", detail.ReviewEvidence)
	}
}

func TestCardInReviewNamesTheUnsatisfiedReview(t *testing.T) {
	objective := work.Objective{ID: "objective", Phase: work.ObjectiveExecution}
	item := ports.WorkItemContext{
		WorkItem: work.WorkItem{ID: "item", Key: "TH-1", CommitmentState: work.ItemAccepted, ExecutionStatus: work.StatusReview},
		ReviewEvidence: []work.ReviewEvidence{
			{Requirement: work.ReviewRequirement{CriterionRef: "suite", ValidatorKind: "probe"}, State: work.ReviewEvidenceSatisfied},
			{Requirement: work.ReviewRequirement{CriterionRef: "code-review", ValidatorKind: "human_review"}, State: work.ReviewEvidenceStale},
		},
	}
	card := buildCard(item, map[string]Gate{}, map[string]bool{}, objective, "review", time.Now())
	if card.Blocker == nil || card.Blocker.Code != "review_evidence" || !strings.Contains(card.Blocker.Label, "code-review stale") {
		t.Fatalf("card in review with a stale review = %#v", card.Blocker)
	}

	// Only an item in review is waiting on its reviews to reach done; the same
	// evidence on an item still being worked is not what holds it.
	item.WorkItem.ExecutionStatus = work.StatusInProgress
	if card := buildCard(item, map[string]Gate{}, map[string]bool{"item": true}, objective, "in_progress", time.Now()); card.Blocker != nil {
		t.Fatalf("card in progress with a stale review = %#v, want no blocker", card.Blocker)
	}
	item.WorkItem.ExecutionStatus = work.StatusReview

	item.ReviewEvidence[1].State = work.ReviewEvidenceSatisfied
	if card := buildCard(item, map[string]Gate{}, map[string]bool{}, objective, "review", time.Now()); card.Blocker != nil && card.Blocker.Code == "review_evidence" {
		t.Fatalf("card with every review satisfied = %#v", card.Blocker)
	}
}
