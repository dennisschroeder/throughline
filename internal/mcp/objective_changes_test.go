package mcp

import (
	"encoding/json"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestObjectiveChangesIncludeDiscoveryRecords pins the reproduction behind
// REP-07 at the wire: a session resuming on an objective in discovery asks
// get_changes for that objective and must see the questions, decisions and
// context it recorded there, each carrying its objective binding.
func TestObjectiveChangesIncludeDiscoveryRecords(t *testing.T) {
	ctx, session := newSession(t)
	call := func(name string, arguments map[string]any) map[string]any {
		t.Helper()
		if _, ok := arguments["workspace_id"]; !ok {
			arguments["workspace_id"] = testWorkspaceID
		}
		result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["error"] != nil {
			t.Fatalf("%s failed: %#v", name, payload["error"])
		}
		return payload
	}
	call("register_actor", map[string]any{"actor_id": "agent:planner", "kind": "agent", "display_name": "Planner", "idempotency_key": "register"})
	created := call("create_objective", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "discovery-objective", "key": "OBJ-DISCOVERY",
		"title": "Still in discovery", "desired_outcome": "Its history is resumable.", "phase": "discovery",
	})
	objectiveID := created["result"].(map[string]any)["id"].(string)
	call("record_context", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "discovery-context", "objective_id": objectiveID,
		"kind": "requirement", "status": "proposed", "title": "Resumption needs the feed",
	})
	call("ask_question", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "discovery-question", "objective_id": objectiveID, "question": "Who resumes?",
	})
	call("record_decision", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "discovery-decision", "objective_id": objectiveID,
		"title": "Bind activity", "decision": "Every objective-scoped event carries its objective.",
	})

	changes := call("get_changes", map[string]any{"objective_id": "OBJ-DISCOVERY", "since": "0"})
	entries, _ := changes["result"].(map[string]any)["changes"].([]any)
	seen := map[string]bool{}
	for _, raw := range entries {
		entry := raw.(map[string]any)
		if entry["objective_id"] != objectiveID {
			t.Fatalf("change %v carries objective_id %v, want %s", entry["event_type"], entry["objective_id"], objectiveID)
		}
		seen[entry["event_type"].(string)] = true
	}
	for _, want := range []string{"objective.created", "context_record.recorded", "question.asked", "decision.recorded"} {
		if !seen[want] {
			t.Fatalf("get_changes for the objective omits %s: %v", want, seen)
		}
	}
}
