package dashboard

import (
	"testing"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// TestCardProgressCountsOnlyActiveCriteria is REP-04's second criterion on the
// human surface. Counting a superseded condition made correcting a criterion
// move the card away from done — describing the work more accurately is not
// the same as having done less of it.
func TestCardProgressCountsOnlyActiveCriteria(t *testing.T) {
	criterion := func(status work.AcceptanceCriterionStatus, required bool) work.AcceptanceCriterion {
		return work.AcceptanceCriterion{ID: string(status), WorkItemID: "item", Text: "A condition", Required: required, Status: status}
	}
	for _, testCase := range []struct {
		name          string
		criteria      []work.AcceptanceCriterion
		passed, total int
	}{
		{
			name: "a superseded criterion and its pending replacement count once",
			criteria: []work.AcceptanceCriterion{
				criterion(work.AcceptanceSuperseded, true),
				criterion(work.AcceptancePending, true),
				criterion(work.AcceptanceSatisfied, true),
			},
			passed: 1, total: 2,
		},
		{
			name: "a criterion satisfied before it was superseded stops counting as passed",
			criteria: []work.AcceptanceCriterion{
				func() work.AcceptanceCriterion {
					c := criterion(work.AcceptanceSuperseded, true)
					c.ResolvedBy = "human:reviewer"
					return c
				}(),
				criterion(work.AcceptancePending, true),
			},
			passed: 0, total: 1,
		},
		{
			name:     "optional criteria are still ignored",
			criteria: []work.AcceptanceCriterion{criterion(work.AcceptanceSatisfied, false), criterion(work.AcceptancePending, true)},
			passed:   0, total: 1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			item := ports.WorkItemContext{
				WorkItem:           work.WorkItem{ID: "item", Key: "TH-1", ExecutionStatus: work.StatusInProgress},
				AcceptanceCriteria: testCase.criteria,
			}
			card := buildCard(item, map[string]Gate{}, map[string]bool{}, work.Objective{ID: "objective"}, "in_progress", time.Now())
			if card.Progress == nil {
				t.Fatal("card carries no progress")
			}
			if card.Progress.Passed != testCase.passed || card.Progress.Total != testCase.total {
				t.Fatalf("progress = %d/%d, want %d/%d", card.Progress.Passed, card.Progress.Total, testCase.passed, testCase.total)
			}
		})
	}
}
