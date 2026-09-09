package mcp

import (
	"encoding/json"
	"fmt"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestOrientationSeesAnObjectiveWithNoWorkItems is REP-03's first criterion at
// the wire. board_overview is the call an agent is told to orient with, and its
// objectives field used to be built by iterating work items: it counted items
// per phase rather than objectives, and an objective with no items contributed
// nothing at all, so the one orientation call could not see it.
func TestOrientationSeesAnObjectiveWithNoWorkItems(t *testing.T) {
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
	call("register_actor", map[string]any{"actor_id": "agent:orient", "kind": "agent", "display_name": "Orient", "idempotency_key": "register-orient"})
	created := call("create_objective", map[string]any{
		"actor_id": "agent:orient", "idempotency_key": "orient-objective", "key": "OBJ-ORIENT",
		"title": "Created and immediately abandoned", "desired_outcome": "Visible before it has any work", "phase": "discovery",
	})
	objectiveID := created["result"].(map[string]any)["id"].(string)

	// A second objective carrying more than one work item. Counting objectives
	// and counting items are only distinguishable when some objective holds
	// several: with one item each, a half-fix that still counts items but adds
	// the empty ones passes every assertion.
	busy := call("create_objective", map[string]any{
		"actor_id": "agent:orient", "idempotency_key": "orient-busy", "key": "OBJ-BUSY",
		"title": "Holds three items", "desired_outcome": "Counted once", "phase": "planning",
	})
	busyID := busy["result"].(map[string]any)["id"].(string)
	for index, key := range []string{"TH-BUSY-1", "TH-BUSY-2", "TH-BUSY-3"} {
		call("create_item", map[string]any{
			"actor_id": "agent:orient", "idempotency_key": fmt.Sprintf("orient-item-%d", index),
			"key": key, "objective_id": busyID, "title": "Work " + key, "kind": "research",
		})
	}

	overview := call("board_overview", map[string]any{})
	objectives, ok := overview["result"].(map[string]any)["objectives"].(map[string]any)
	if !ok {
		t.Fatalf("board_overview result = %#v", overview["result"])
	}
	if objectives["discovery"] != float64(1) {
		t.Fatalf("board_overview objectives = %#v, want one objective in discovery", objectives)
	}
	if objectives["planning"] != float64(1) {
		t.Fatalf("board_overview objectives = %#v, want the three-item objective counted once, not three times", objectives)
	}

	listed := call("list_objectives", map[string]any{})
	rows, ok := listed["result"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("list_objectives result = %#v, want both objectives", listed["result"])
	}
	rowsByKey := map[string]map[string]any{}
	for _, raw := range rows {
		entry := raw.(map[string]any)
		rowsByKey[entry["key"].(string)] = entry
	}
	if counts, ok := rowsByKey["OBJ-BUSY"]["item_counts"].(map[string]any); !ok || counts["backlog"] != float64(3) {
		t.Fatalf("OBJ-BUSY item_counts = %#v, want three in backlog", rowsByKey["OBJ-BUSY"]["item_counts"])
	}
	row := rowsByKey["OBJ-ORIENT"]
	if row["id"] != objectiveID || row["key"] != "OBJ-ORIENT" || row["phase"] != "discovery" {
		t.Fatalf("list_objectives row = %#v", row)
	}
	if counts, ok := row["item_counts"].(map[string]any); !ok || len(counts) != 0 {
		t.Fatalf("item_counts = %#v, want an empty map rather than an absent one", row["item_counts"])
	}

	// The key is an address: a caller resuming from notes has only that.
	byKey := call("get_objective_context", map[string]any{"objective_id": "OBJ-ORIENT", "actor_id": "agent:orient"})
	context, ok := byKey["result"].(map[string]any)["objective"].(map[string]any)
	if !ok || context["id"] != objectiveID {
		t.Fatalf("get_objective_context by key = %#v", byKey["result"])
	}
	// The identifier must keep working: routing these tools through the resolver
	// put id addressing on a new code path of its own.
	byID := call("get_objective_context", map[string]any{"objective_id": objectiveID, "actor_id": "agent:orient"})
	if byID["result"].(map[string]any)["objective"].(map[string]any)["id"] != objectiveID {
		t.Fatalf("get_objective_context by id = %#v", byID["result"])
	}
	for _, reference := range []any{"OBJ-ORIENT", objectiveID} {
		scoped := call("board_overview", map[string]any{"objective_id": reference})
		phases := scoped["result"].(map[string]any)["objectives"].(map[string]any)
		if phases["discovery"] != float64(1) || phases["planning"] != nil {
			t.Fatalf("board_overview scoped by %v = %#v", reference, phases)
		}
	}

	// A filter that resolves nothing must say so rather than answer "no work".
	for _, name := range []string{"list_items", "get_changes", "list_outputs"} {
		arguments := map[string]any{"workspace_id": testWorkspaceID, "objective_id": "OBJ-BUSY"}
		if name == "get_changes" {
			arguments["since"] = "0"
		}
		scoped := call(name, arguments)
		if scoped["error"] != nil {
			t.Fatalf("%s by key = %#v", name, scoped["error"])
		}
	}
	unknown, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "list_items", Arguments: map[string]any{"workspace_id": testWorkspaceID, "objective_id": "OBJ-NO-SUCH-THING"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !unknown.IsError {
		t.Fatalf("list_items with an unresolvable objective_id answered successfully: %s", unknown.Content[0].(*protocol.TextContent).Text)
	}
}
