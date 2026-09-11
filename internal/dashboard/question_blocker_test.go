package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// TestCardNamesAQuestionBlockerApartFromDependencies is REP-08's second
// criterion on the board: an item held by a question and an item waiting on
// a prerequisite are resolved in different ways, so the card must say which.
func TestCardNamesAQuestionBlockerApartFromDependencies(t *testing.T) {
	objective := work.Objective{ID: "objective", Phase: work.ObjectiveExecution}
	item := func(questions ...work.Question) ports.WorkItemContext {
		return ports.WorkItemContext{
			WorkItem:          work.WorkItem{ID: "item", Key: "TH-1", CommitmentState: work.ItemAccepted, ExecutionStatus: work.StatusReady},
			BlockingQuestions: questions,
		}
	}
	notReady := map[string]bool{}

	held := buildCard(item(
		work.Question{ID: "q1", Text: "Which region hosts the data?", Status: work.QuestionOpen},
		work.Question{ID: "q2", Text: "Who owns sign-off?", Status: work.QuestionUnsharp},
	), map[string]Gate{}, notReady, objective, "ready", time.Now())
	if held.Blocker == nil || held.Blocker.Code != "blocked_question" {
		t.Fatalf("card held by questions = %#v, want blocker code blocked_question", held.Blocker)
	}
	if !strings.Contains(held.Blocker.Label, "Which region hosts the data?") || !strings.Contains(held.Blocker.Label, "+1 more") {
		t.Fatalf("question blocker label = %q, want the first question and the count of the rest", held.Blocker.Label)
	}

	waiting := buildCard(item(), map[string]Gate{}, notReady, objective, "ready", time.Now())
	if waiting.Blocker == nil || waiting.Blocker.Code != "blocked_dependency" {
		t.Fatalf("card without questions = %#v, want blocker code blocked_dependency", waiting.Blocker)
	}

	done := item(work.Question{ID: "q1", Text: "Late question", Status: work.QuestionOpen})
	done.WorkItem.ExecutionStatus = work.StatusDone
	if card := buildCard(done, map[string]Gate{}, notReady, objective, "done", time.Now()); card.Blocker != nil {
		t.Fatalf("done card = %#v, want no blocker", card.Blocker)
	}
}

// TestQuestionGatePreservesTheStoredAttentionState replaces a detail that
// reported every flagged question as "flagged for human attention",
// discarding which of the four states was asked for.
func TestQuestionGatePreservesTheStoredAttentionState(t *testing.T) {
	objCtx := ports.ObjectiveContext{
		Objective: work.Objective{ID: "objective", Title: "Objective"},
		Questions: []work.Question{{ID: "q1", Text: "Which region?", Status: work.QuestionUnsharp, AttentionState: work.AttentionNeedsHumanReview, Version: 1}},
	}
	gate := Gate{ID: "q1", Kind: "question", Title: "Which region?", TargetID: "q1", ExpectedVersion: 1, RequestedAt: time.Now().UTC().Format(time.RFC3339)}
	ask, evidence, facts, _ := questionGateSections(objCtx, gate, nil, time.Now())
	var attention string
	for _, fact := range facts {
		if fact.Label == "attention state" {
			attention = fact.Value
		}
	}
	if attention != string(work.AttentionNeedsHumanReview) {
		t.Fatalf("attention state fact = %q, want needs_human_review", attention)
	}
	if evidence.Meta != string(work.QuestionUnsharp) || !strings.Contains(ask, "sharpen_question") {
		t.Fatalf("unsharp question gate: meta %q, ask %q", evidence.Meta, ask)
	}
}
