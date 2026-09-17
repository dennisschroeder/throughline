package work

import (
	"strings"
	"testing"
)

func criterionFor(id, itemID string, ordinal int) AcceptanceCriterion {
	return AcceptanceCriterion{ID: id, WorkItemID: itemID, Ordinal: ordinal, Text: "A condition", Required: true, Status: AcceptancePending, Version: 1}
}

// TestSupersedeAcceptanceCriterionRefusesWhatItCannotMean pins the guards. None
// of them had a test: the application layer happens to check some of the same
// things first, so every one could be deleted without anything noticing.
func TestSupersedeAcceptanceCriterionRefusesWhatItCannotMean(t *testing.T) {
	predecessor := criterionFor("id-old", "item-1", 1)
	replacement := criterionFor("id-new", "item-1", 1)
	for _, testCase := range []struct {
		name        string
		predecessor AcceptanceCriterion
		replacement AcceptanceCriterion
		actor       string
		rationale   string
		wantMessage string
	}{
		{"an actor is required", predecessor, replacement, "  ", "Because.", "requires an actor"},
		{"a reason is required", predecessor, replacement, "human:owner", "  ", "requires a reason"},
		{
			name: "a criterion cannot be superseded twice",
			predecessor: func() AcceptanceCriterion {
				already := predecessor
				already.Status = AcceptanceSuperseded
				return already
			}(),
			replacement: replacement, actor: "human:owner", rationale: "Because.",
			wantMessage: "already superseded",
		},
		{
			name:        "a criterion cannot be superseded from another work item",
			predecessor: predecessor, replacement: criterionFor("id-new", "item-2", 1),
			actor: "human:owner", rationale: "Because.",
			wantMessage: "within its own work item",
		},
		{
			name: "a criterion cannot supersede itself", predecessor: predecessor, replacement: predecessor,
			actor: "human:owner", rationale: "Because.", wantMessage: "cannot supersede itself",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := SupersedeAcceptanceCriterion(testCase.predecessor, testCase.replacement, testCase.actor, testCase.rationale)
			if err == nil {
				t.Fatal("supersession succeeded")
			}
			if !strings.Contains(err.Error(), testCase.wantMessage) {
				t.Fatalf("error = %q, want it to mention %q", err, testCase.wantMessage)
			}
		})
	}
}

// TestSupersedeAcceptanceCriterionKeepsAnEarlierVerdict covers the case that
// makes the predecessor worth keeping at all: a criterion already judged, whose
// verdict must stay readable once the condition it judged is replaced.
func TestSupersedeAcceptanceCriterionKeepsAnEarlierVerdict(t *testing.T) {
	judged := criterionFor("id-old", "item-1", 1)
	judged.Status = AcceptanceSatisfied
	judged.ResolvedBy = "human:reviewer"
	judged.ResolutionRationale = "Met, against the condition as written then."
	superseded, replacement, err := SupersedeAcceptanceCriterion(judged, criterionFor("id-new", "item-1", 1), "human:owner", "The condition was the wrong one.")
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Status != AcceptanceSuperseded || superseded.Version != judged.Version+1 {
		t.Fatalf("superseded = %#v", superseded)
	}
	if superseded.ResolvedBy != "human:reviewer" || superseded.ResolutionRationale != "Met, against the condition as written then." {
		t.Fatalf("superseding erased the earlier verdict: %#v", superseded)
	}
	if superseded.Text != judged.Text || superseded.Ordinal != judged.Ordinal || superseded.Required != judged.Required {
		t.Fatalf("superseding rewrote the predecessor: %#v", superseded)
	}
	if replacement.SupersedesID != judged.ID || replacement.SupersessionReason != "The condition was the wrong one." {
		t.Fatalf("replacement = %#v", replacement)
	}
	if replacement.Status != AcceptancePending {
		t.Fatalf("replacement status = %q, want pending", replacement.Status)
	}
}

func TestSupersededCriteriaAreNotActive(t *testing.T) {
	for status, want := range map[AcceptanceCriterionStatus]bool{
		AcceptancePending:    true,
		AcceptanceSatisfied:  true,
		AcceptanceWaived:     true,
		AcceptanceSuperseded: false,
	} {
		if got := status.Active(); got != want {
			t.Errorf("%q active = %v, want %v", status, got, want)
		}
	}
}
