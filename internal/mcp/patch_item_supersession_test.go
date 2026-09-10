package mcp

import (
	"encoding/json"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestPatchItemMapsSupersedesIDAndSupersessionReasonWithoutSwapping guards the
// patch_item request mapping in server.go: supersedes_id and
// supersession_reason are both plain strings, so a field swap in the mapping
// compiles and, without this test, passes the whole suite. This is the only
// entry point an agent actually uses to supersede a criterion.
func TestPatchItemMapsSupersedesIDAndSupersessionReasonWithoutSwapping(t *testing.T) {
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

	call("register_actor", map[string]any{"actor_id": "agent:supersede", "kind": "agent", "display_name": "Supersede", "idempotency_key": "supersede-actor"})
	objective := call("create_objective", map[string]any{
		"actor_id": "agent:supersede", "idempotency_key": "supersede-objective", "key": "OBJ-SUPERSEDE",
		"title": "Correct a wrong condition", "desired_outcome": "History survives the correction.", "phase": "planning",
	})["result"].(map[string]any)["id"].(string)
	item := call("create_item", map[string]any{
		"actor_id": "agent:supersede", "idempotency_key": "supersede-item", "key": "TH-SUPERSEDE",
		"objective_id": objective, "title": "Carries the criterion", "kind": "research",
		"acceptance_criteria": []map[string]any{
			{"text": "The wrong condition.", "required": true, "ordinal": 1},
		},
	})["result"].(map[string]any)
	itemID := item["id"].(string)

	criteria := call("get_item", map[string]any{"id": itemID, "include": []string{"acceptance_criteria"}})["result"].(map[string]any)["acceptance_criteria"].([]any)
	predecessor := criteria[0].(map[string]any)
	predecessorID := predecessor["id"].(string)

	const reason = "The original named the wrong artefact."
	patched := call("patch_item", map[string]any{
		"id": itemID, "actor_id": "agent:supersede", "idempotency_key": "supersede-patch",
		"expected_version": item["version"],
		"acceptance_criteria_to_add": []map[string]any{
			{"text": "The correct condition.", "required": true, "ordinal": predecessor["ordinal"], "supersedes_id": predecessorID, "supersession_reason": reason},
		},
	})
	_ = patched

	after := call("get_item", map[string]any{"id": itemID, "include": []string{"acceptance_criteria"}})["result"].(map[string]any)["acceptance_criteria"].([]any)
	var replacement map[string]any
	for _, raw := range after {
		row := raw.(map[string]any)
		if row["id"] != predecessorID {
			replacement = row
		}
	}
	if replacement == nil {
		t.Fatalf("no replacement criterion found among %#v", after)
	}
	// A swapped mapping would put the reason where the ID belongs and vice
	// versa; this is only distinguishable because the two fields hold
	// differently-shaped values (a UUID versus a sentence).
	if replacement["supersedes_id"] != predecessorID {
		t.Fatalf("replacement supersedes_id = %v, want the predecessor id %q", replacement["supersedes_id"], predecessorID)
	}
	if replacement["supersession_reason"] != reason {
		t.Fatalf("replacement supersession_reason = %v, want %q", replacement["supersession_reason"], reason)
	}
}
