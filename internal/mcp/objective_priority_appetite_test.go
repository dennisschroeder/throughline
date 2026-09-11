package mcp

import (
	"encoding/json"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCreateObjectivePersistsAppetite pins the gap review found: appetite was
// decoded from create_objective's input and advertised in its schema, but the
// command constructed from it never carried the field, so any appetite given
// at creation was silently discarded.
func TestCreateObjectivePersistsAppetite(t *testing.T) {
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
		"actor_id": "agent:planner", "idempotency_key": "appetite-objective", "key": "OBJ-APPETITE",
		"title": "Carries an appetite from creation", "desired_outcome": "The appetite is not dropped.", "phase": "planning",
		"appetite": map[string]any{"value": 500, "unit": "tokens", "basis": "estimated"},
	})
	result := created["result"].(map[string]any)
	appetite, ok := result["appetite"].(map[string]any)
	if !ok {
		t.Fatalf("create_objective result carries no appetite: %#v", result)
	}
	if appetite["value"] != float64(500) || appetite["unit"] != "tokens" || appetite["basis"] != "estimated" {
		t.Fatalf("appetite at creation = %#v, want value 500, unit tokens, basis estimated", appetite)
	}
}

// TestPatchObjectiveAppliesPriority pins the other gap review found
// unguarded: patch_objective's priority-application path, at both the
// service layer and the MCP wire-mapping layer, had no test proving a
// patched priority is actually applied rather than silently ignored.
func TestPatchObjectiveAppliesPriority(t *testing.T) {
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
		"actor_id": "agent:planner", "idempotency_key": "priority-objective", "key": "OBJ-PATCH-PRIORITY",
		"title": "Starts at medium priority", "desired_outcome": "The patched priority takes effect.", "phase": "planning",
	})
	result := created["result"].(map[string]any)
	if result["priority"] != "medium" {
		t.Fatalf("priority at creation = %v, want the default medium", result["priority"])
	}

	patched := call("patch_objective", map[string]any{
		"objective_id": result["id"], "actor_id": "agent:planner", "idempotency_key": "priority-patch",
		"expected_version": result["version"], "priority": "urgent",
	})
	patchedResult := patched["result"].(map[string]any)
	if patchedResult["priority"] != "urgent" {
		t.Fatalf("priority after patch = %v, want urgent", patchedResult["priority"])
	}
}
