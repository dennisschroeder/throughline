package dashboard

import (
	"testing"

	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// TestAcceptanceCriterionDetailViewCarriesTheSupersessionLink is REP-04's
// first criterion checked at the surface a person actually reads. The MCP
// tool response and the dashboard drawer are two independent mappings of the
// same two fields; pinning one does not pin the other.
func TestAcceptanceCriterionDetailViewCarriesTheSupersessionLink(t *testing.T) {
	criterion := work.AcceptanceCriterion{
		ID: "replacement", WorkItemID: "item", Text: "The corrected condition.", Required: true,
		Status: work.AcceptancePending, SupersedesID: "predecessor", SupersessionReason: "The original named the wrong artefact.",
	}
	view := acceptanceCriterionDetailView(criterion)
	if view.SupersedesID != "predecessor" {
		t.Fatalf("view.SupersedesID = %q, want %q", view.SupersedesID, "predecessor")
	}
	if view.SupersessionReason != "The original named the wrong artefact." {
		t.Fatalf("view.SupersessionReason = %q, want the recorded reason", view.SupersessionReason)
	}
}
