package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestReviewRequirementsOverTheWire pins REP-09's MCP surface: declaring
// reviews on create_item and patch_item, recording a work-item review through
// record_validation, and reading each requirement's state from get_item.
func TestReviewRequirementsOverTheWire(t *testing.T) {
	ctx, session := newSession(t)
	call := func(name string, arguments map[string]any) (map[string]any, string) {
		t.Helper()
		if _, ok := arguments["workspace_id"]; !ok {
			arguments["workspace_id"] = testWorkspaceID
		}
		result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		text := result.Content[0].(*protocol.TextContent).Text
		var payload map[string]any
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatal(err)
		}
		return payload, text
	}
	must := func(name string, arguments map[string]any) map[string]any {
		t.Helper()
		payload, text := call(name, arguments)
		if payload["error"] != nil {
			t.Fatalf("%s failed: %s", name, text)
		}
		return payload["result"].(map[string]any)
	}
	must("register_actor", map[string]any{"actor_id": "human:reviewer", "kind": "human", "display_name": "Reviewer", "idempotency_key": "register"})
	objective := must("create_objective", map[string]any{
		"actor_id": "human:reviewer", "idempotency_key": "objective", "key": "OBJ-REVIEW",
		"title": "Reviews gate done", "desired_outcome": "Done waits for declared review.", "phase": "planning",
	})
	item := must("create_item", map[string]any{
		"actor_id": "human:reviewer", "idempotency_key": "item", "key": "R-1", "objective_id": objective["id"],
		"title": "Reviewed work", "kind": "task",
		"review_requirements": []any{map[string]any{"criterion_ref": "code-review", "validator_kind": "human_review"}},
	})
	if requirements, _ := item["review_requirements"].([]any); len(requirements) != 1 || requirements[0].(map[string]any)["criterion_ref"] != "code-review" {
		t.Fatalf("created item review requirements = %#v", item["review_requirements"])
	}
	if payload, text := call("create_item", map[string]any{
		"actor_id": "human:reviewer", "idempotency_key": "bad-kind", "key": "R-2", "objective_id": objective["id"],
		"title": "Wrong kind", "kind": "task",
		"review_requirements": []any{map[string]any{"criterion_ref": "reuse", "validator_kind": "successor_use"}},
	}); payload["error"] == nil || !strings.Contains(text, "not a review kind") {
		t.Fatalf("create_item with successor_use as a review kind = %s, want it refused", text)
	}

	evidence := func() []any {
		t.Helper()
		detail := must("get_item", map[string]any{"id": item["id"], "include": []any{"review_evidence"}})
		values, _ := detail["review_evidence"].([]any)
		return values
	}
	if states := evidence(); len(states) != 1 || states[0].(map[string]any)["state"] != "missing" {
		t.Fatalf("review evidence before any review = %#v", states)
	}
	recorded := must("record_validation", map[string]any{
		"actor_id": "human:reviewer", "idempotency_key": "review", "work_item_id": item["id"],
		"criterion_ref": "code-review", "validator_kind": "human_review", "verdict": "passed",
		"details": map[string]any{"rationale": "Read every change."}, "degraded": true,
	})
	if recorded["work_item_id"] != item["id"] || recorded["degraded"] != true || recorded["output_revision_id"] != "" {
		t.Fatalf("recorded work item review = %#v", recorded)
	}
	if states := evidence(); len(states) != 1 || states[0].(map[string]any)["state"] != "satisfied" || states[0].(map[string]any)["validation_record_id"] != recorded["id"] || states[0].(map[string]any)["degraded"] != true {
		t.Fatalf("review evidence after the review = %#v", states)
	}
	if trimmed := must("get_item", map[string]any{"id": item["id"], "include": []any{"dependencies"}}); trimmed["review_evidence"] != nil {
		t.Fatalf("get_item without the review_evidence section = %#v", trimmed["review_evidence"])
	}
	if payload, text := call("record_validation", map[string]any{
		"actor_id": "human:reviewer", "idempotency_key": "both", "work_item_id": item["id"], "output_revision_id": "revision",
		"criterion_ref": "code-review", "validator_kind": "probe", "verdict": "passed",
	}); payload["error"] == nil || !strings.Contains(text, "exactly one") {
		t.Fatalf("record_validation naming both subjects = %s, want it refused", text)
	}

	current := must("get_item", map[string]any{"id": item["id"]})["work_item"].(map[string]any)
	cleared := must("patch_item", map[string]any{
		"id": item["id"], "actor_id": "human:reviewer", "idempotency_key": "clear", "expected_version": current["version"],
		"review_requirements": []any{},
	})
	if cleared["review_requirements"] != nil {
		t.Fatalf("patched item after clearing review requirements = %#v", cleared["review_requirements"])
	}
	if states := evidence(); len(states) != 0 {
		t.Fatalf("review evidence after clearing the requirements = %#v", states)
	}
}
