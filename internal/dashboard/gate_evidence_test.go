package dashboard

import (
	"testing"

	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

// TestGateEvidenceOmitsSupersededCriteria keeps a replaced condition out of the
// evidence a reviewer judges at the gate. Listing it would ask someone to weigh
// a condition nobody stands behind any more.
func TestGateEvidenceOmitsSupersededCriteria(t *testing.T) {
	criterion := func(id string, status work.AcceptanceCriterionStatus) work.AcceptanceCriterion {
		return work.AcceptanceCriterion{ID: id, WorkItemID: "item", Text: "condition " + id, Required: true, Status: status}
	}
	item := &ports.WorkItemContext{
		WorkItem: work.WorkItem{ID: "item", Key: "TH-1"},
		AcceptanceCriteria: []work.AcceptanceCriterion{
			criterion("replaced", work.AcceptanceSuperseded),
			criterion("replacement", work.AcceptancePending),
			criterion("untouched", work.AcceptanceSatisfied),
		},
	}
	rows := gateCriterionRows(item)
	if len(rows) != 2 {
		t.Fatalf("gate evidence rows = %d, want the two active conditions: %#v", len(rows), rows)
	}
	for _, row := range rows {
		if row.Status == string(work.AcceptanceSuperseded) || row.Text == "condition replaced" {
			t.Fatalf("gate evidence carries a superseded condition: %#v", row)
		}
	}
}
