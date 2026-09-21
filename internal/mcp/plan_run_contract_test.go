package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAHarnessCanFindAndContinueTheRightRunOverMCP walks the contract a session
// that did not create the runs actually depends on. It knows only the objective
// key, and has to end up holding one run's work — without anything resolving a
// latest run on its behalf.
func TestAHarnessCanFindAndContinueTheRightRunOverMCP(t *testing.T) {
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
		return payload["result"].(map[string]any)
	}

	call("register_actor", map[string]any{"actor_id": "agent:runner", "kind": "agent", "display_name": "Runner", "idempotency_key": "register-runner"})
	call("register_actor", map[string]any{"actor_id": "human:owner", "kind": "human", "display_name": "Owner", "idempotency_key": "register-owner"})
	objective := call("create_objective", map[string]any{
		"actor_id": "agent:runner", "idempotency_key": "objective", "key": "OBJ-RUNS-MCP",
		"title": "Repeatable work", "desired_outcome": "A later session continues the right run.",
		"phase": "planning", "mode": "ongoing", "max_concurrent_runs": 2,
	})
	if objective["mode"] != "ongoing" || objective["max_concurrent_runs"].(float64) != 2 {
		t.Fatalf("objective mode and capacity did not survive the MCP contract: %#v", objective)
	}
	plan := call("propose_plan", map[string]any{
		"objective_id": "OBJ-RUNS-MCP", "actor_id": "agent:runner", "idempotency_key": "plan",
		"title": "Two steps", "revision": 1,
		"steps": []any{
			map[string]any{"client_ref": "one", "key": "MCP-ONE", "title": "First", "kind": "research", "priority": "high", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent"},
			map[string]any{"client_ref": "two", "key": "MCP-TWO", "title": "Second", "kind": "research", "optional": true, "priority": "medium", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent", "depends_on": []any{"one"}},
		},
	})
	planID := plan["plan"].(map[string]any)["id"].(string)
	if steps := plan["steps"].([]any); len(steps) != 2 {
		t.Fatalf("propose_plan persisted %d steps, want 2", len(steps))
	}
	call("review_plan", map[string]any{"plan_id": planID, "actor_id": "human:owner", "idempotency_key": "review", "decision": "approved", "reason": "Complete.", "expected_version": 1})
	call("transition_objective", map[string]any{"objective_id": "OBJ-RUNS-MCP", "actor_id": "human:owner", "idempotency_key": "execute", "target_phase": "execution", "reason": "Run it.", "expected_version": 1})

	var runIDs []string
	for _, key := range []string{"march", "april"} {
		created := call("create_plan_run", map[string]any{
			"objective_id": "OBJ-RUNS-MCP", "plan_id": planID, "actor_id": "agent:runner",
			"idempotency_key": "run-" + key, "run_key": key,
		})
		run := created["run"].(map[string]any)
		if run["status"] != "active" {
			t.Fatalf("created run = %#v", run)
		}
		if items := created["work_items"].([]any); len(items) != 2 {
			t.Fatalf("run %s materialized %d work items, want 2", key, len(items))
		}
		runIDs = append(runIDs, run["id"].(string))
	}

	// A session that knows only the objective key finds both runs, newest
	// first, each carrying its revision and how much of it is left.
	listed := call("list_plan_runs", map[string]any{"objective_id": "OBJ-RUNS-MCP"})
	runs := listed["runs"].([]any)
	if listed["total"].(float64) != 2 || len(runs) != 2 || listed["has_more"].(bool) {
		t.Fatalf("list_plan_runs = %#v", listed)
	}
	for _, entry := range runs {
		summary := entry.(map[string]any)
		if summary["plan_revision"].(float64) != 1 || summary["work_items"].(float64) != 2 || summary["done"].(float64) != 0 {
			t.Fatalf("run summary = %#v", summary)
		}
	}

	// Paging is bounded and says when there is more.
	page := call("list_plan_runs", map[string]any{"objective_id": "OBJ-RUNS-MCP", "limit": 1})
	if len(page["runs"].([]any)) != 1 || !page["has_more"].(bool) || page["next_cursor"].(string) == "" {
		t.Fatalf("first page = %#v", page)
	}
	rest := call("list_plan_runs", map[string]any{"objective_id": "OBJ-RUNS-MCP", "cursor": page["next_cursor"], "limit": 1})
	if len(rest["runs"].([]any)) != 1 || rest["has_more"].(bool) {
		t.Fatalf("second page = %#v", rest)
	}

	// Having chosen a run, the session lists that run's work — and gets only
	// that run's work, though both runs answer to the same plan revision.
	chosen := runIDs[0]
	scoped := call("list_items", map[string]any{"objective_id": "OBJ-RUNS-MCP", "plan_run_id": chosen})
	scopedItems := scoped["items"].([]any)
	if len(scopedItems) != 2 {
		t.Fatalf("run-scoped listing = %d items, want 2", len(scopedItems))
	}
	for _, entry := range scopedItems {
		item := entry.(map[string]any)["work_item"].(map[string]any)
		if item["plan_run_id"] != chosen {
			t.Fatalf("run-scoped listing returned work from %v, want %s", item["plan_run_id"], chosen)
		}
		if item["origin"] != "plan_step" || item["origin_plan_step_id"] == "" {
			t.Fatalf("a materialized item lost its provenance over MCP: %#v", item)
		}
	}
	byPlan := call("list_items", map[string]any{"objective_id": "OBJ-RUNS-MCP", "plan_id": planID})
	if len(byPlan["items"].([]any)) != 4 {
		t.Fatalf("plan-scoped listing = %d items, want all four — which is why plan_id cannot scope a run", len(byPlan["items"].([]any)))
	}

	// The objective's own continuation view already carries the runs, so
	// orienting does not need a second question.
	objectiveContext := call("get_objective_context", map[string]any{"objective_id": "OBJ-RUNS-MCP", "actor_id": "agent:runner"})
	contextRuns, ok := objectiveContext["plan_runs"].([]any)
	if !ok || len(contextRuns) != 2 {
		t.Fatalf("objective context plan_runs = %#v", objectiveContext["plan_runs"])
	}

	// Filtering by status is the read that answers "which run is still live".
	call("close_plan_run", map[string]any{
		"plan_run_id": chosen, "actor_id": "human:owner", "idempotency_key": "cancel-chosen",
		"expected_version": 1, "target_status": "cancelled", "reason": "Superseded by the other run.",
	})
	active := call("list_plan_runs", map[string]any{"objective_id": "OBJ-RUNS-MCP", "status": []any{"active"}})
	if active["total"].(float64) != 1 {
		t.Fatalf("active listing after closing one run = %#v", active)
	}
	if active["runs"].([]any)[0].(map[string]any)["run"].(map[string]any)["id"] != runIDs[1] {
		t.Fatalf("the active listing named the wrong run: %#v", active["runs"])
	}
}

// TestListPlanRunsRejectsAnUnknownStatus keeps the filter honest: a status the
// model does not have must be refused rather than silently matching nothing,
// which would read as "no runs" to the caller.
func TestListPlanRunsRejectsAnUnknownStatus(t *testing.T) {
	ctx, session := newSession(t)
	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "list_plan_runs", Arguments: map[string]any{
		"workspace_id": testWorkspaceID, "status": []any{"finished"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("an unknown plan run status was accepted as a filter")
	}
}

// TestTheToolSurfaceOffersNoSchedulingOrImplicitSelection asserts the
// objective's non-goals against the contract itself. An absence is what a
// later change removes without noticing, so the surface is inspected rather
// than trusted: no tool may take a cadence, and none may offer to pick a run.
func TestTheToolSurfaceOffersNoSchedulingOrImplicitSelection(t *testing.T) {
	ctx, session := newSession(t)
	tools, err := session.ListTools(ctx, &protocol.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("the server advertises no tools")
	}
	// Words that would mean Throughline had taken on triggering or cadence.
	// These match anywhere in a field name, because no legitimate field here
	// contains them.
	cadence := []string{"schedule", "cron", "cadence", "interval", "recurrence", "frequency"}
	// Whole field names that would mean it chose which run a caller meant.
	// These are matched exactly: "max_concurrent_runs" is a capacity cap, not
	// a selection, and contains one of them as a substring.
	selection := map[string]bool{
		"latest_run": true, "current_run": true, "previous_run": true, "last_successful_run": true,
		"latest_revision": true, "current_revision": true, "use_default": true, "use_latest": true,
		"next_run_at": true, "due_at": true,
	}
	for _, tool := range tools.Tools {
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatal(err)
		}
		properties, _ := schema["properties"].(map[string]any)
		for name := range properties {
			lowered := strings.ToLower(name)
			for _, banned := range cadence {
				if strings.Contains(lowered, banned) {
					t.Errorf("tool %s takes %q, which would make Throughline own triggering or cadence", tool.Name, name)
				}
			}
			if selection[lowered] {
				t.Errorf("tool %s takes %q, which would make Throughline choose a run or revision for the caller", tool.Name, name)
			}
		}
	}

	// create_plan_run requires the revision explicitly: there is no way to ask
	// for "the current one".
	schema := map[string]any{}
	for _, tool := range tools.Tools {
		if tool.Name != "create_plan_run" {
			continue
		}
		encoded, _ := json.Marshal(tool.InputSchema)
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatal(err)
		}
	}
	required, _ := schema["required"].([]any)
	names := map[string]bool{}
	for _, value := range required {
		names[value.(string)] = true
	}
	if !names["plan_id"] || !names["run_key"] || !names["objective_id"] {
		t.Fatalf("create_plan_run does not require the caller to name what it means: %#v", required)
	}
}
