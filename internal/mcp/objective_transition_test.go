package mcp

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestObjectiveTransitionReasonOverTheWire pins REP-10 at the MCP surface: the
// transition response, list_objectives and get_objective_context all show why
// the objective is in its phase, and no tool claims, leases or advances an
// objective on its own.
func TestObjectiveTransitionReasonOverTheWire(t *testing.T) {
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
		text := result.Content[0].(*protocol.TextContent).Text
		var payload map[string]any
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["error"] != nil {
			t.Fatalf("%s failed: %s", name, text)
		}
		return payload
	}
	call("register_actor", map[string]any{"actor_id": "human:owner", "kind": "human", "display_name": "Owner", "idempotency_key": "register"})
	created := call("create_objective", map[string]any{
		"actor_id": "human:owner", "idempotency_key": "objective", "key": "OBJ-WHY",
		"title": "Explains its phase", "desired_outcome": "The reason is readable.", "phase": "idea",
	})["result"].(map[string]any)
	transitioned := call("transition_objective", map[string]any{
		"objective_id": created["id"], "actor_id": "human:owner", "idempotency_key": "discover",
		"target_phase": "discovery", "reason": "Worth a first look.", "expected_version": created["version"],
	})["result"].(map[string]any)
	check := func(source string, transition any) {
		t.Helper()
		fields, _ := transition.(map[string]any)
		at, _ := fields["at"].(string)
		if fields["from"] != "idea" || fields["to"] != "discovery" || fields["reason"] != "Worth a first look." || fields["actor_id"] != "human:owner" || !strings.HasSuffix(at, "Z") {
			t.Fatalf("%s last_phase_transition = %#v", source, transition)
		}
	}
	check("transition_objective", transitioned["last_phase_transition"])

	listed := call("list_objectives", map[string]any{})["result"].([]any)
	if len(listed) != 1 {
		t.Fatalf("list_objectives = %#v", listed)
	}
	check("list_objectives", listed[0].(map[string]any)["last_phase_transition"])

	context := call("get_objective_context", map[string]any{"objective_id": "OBJ-WHY"})["result"].(map[string]any)
	check("get_objective_context", context["objective"].(map[string]any)["last_phase_transition"])

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var objectiveTools []string
	for _, tool := range tools.Tools {
		if strings.Contains(tool.Name, "objective") {
			objectiveTools = append(objectiveTools, tool.Name)
		}
	}
	slices.Sort(objectiveTools)
	if strings.Join(objectiveTools, ",") != "create_objective,get_objective_context,list_objectives,patch_objective,transition_objective" {
		t.Fatalf("objective tools = %v; a claim, lease or automatic advance for objectives would appear here", objectiveTools)
	}
}
