package mcp

import (
	"encoding/json"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// addressingHarness builds one workspace holding an objective with work, so a
// key-addressed call can be checked for what it selects rather than only for
// whether it succeeded. Asserting success is what let six tools quietly lose key
// addressing while the suite stayed green.
type addressingHarness struct {
	t          *testing.T
	call       func(string, map[string]any) map[string]any
	raw        func(string, map[string]any) *protocol.CallToolResult
	objectiveA string
	objectiveB string
}

func newAddressingHarness(t *testing.T) *addressingHarness {
	t.Helper()
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
	raw := func(name string, arguments map[string]any) *protocol.CallToolResult {
		t.Helper()
		if _, ok := arguments["workspace_id"]; !ok {
			arguments["workspace_id"] = testWorkspaceID
		}
		result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	call("register_actor", map[string]any{"actor_id": "agent:addr", "kind": "agent", "display_name": "Addressing", "idempotency_key": "addr-actor"})
	a := call("create_objective", map[string]any{
		"actor_id": "agent:addr", "idempotency_key": "addr-a", "key": "OBJ-ADDR-A",
		"title": "The addressed objective", "desired_outcome": "Reached by key", "phase": "discovery",
	})["result"].(map[string]any)["id"].(string)
	b := call("create_objective", map[string]any{
		"actor_id": "agent:addr", "idempotency_key": "addr-b", "key": "OBJ-ADDR-B",
		"title": "The other objective", "desired_outcome": "Never reached by mistake", "phase": "discovery",
	})["result"].(map[string]any)["id"].(string)
	return &addressingHarness{t: t, call: call, raw: raw, objectiveA: a, objectiveB: b}
}

// TestEveryToolTakingAnObjectiveIdAcceptsAKey walks the whole surface. Six tools
// could drop their resolver without a single test failing, because nothing
// checked that a key-addressed call reaches the objective the key names.
func TestEveryToolTakingAnObjectiveIdAcceptsAKey(t *testing.T) {
	h := newAddressingHarness(t)

	// Reads: a key must select the same thing the identifier selects.
	h.call("create_item", map[string]any{
		"actor_id": "agent:addr", "idempotency_key": "addr-seed-item", "key": "TH-ADDR-1",
		"objective_id": h.objectiveA, "title": "Work under A", "kind": "research",
	})
	for _, probe := range []struct {
		tool      string
		arguments map[string]any
		count     func(map[string]any) int
	}{
		{"list_items", map[string]any{}, func(r map[string]any) int { return len(r["items"].([]any)) }},
		{"get_changes", map[string]any{"since": "0"}, func(r map[string]any) int {
			changes, _ := r["changes"].([]any)
			return len(changes)
		}},
	} {
		t.Run(probe.tool, func(t *testing.T) {
			byKey := probe.arguments
			byKey["objective_id"] = "OBJ-ADDR-A"
			keyed := probe.count(h.call(probe.tool, cloneArgs(byKey))["result"].(map[string]any))
			byID := cloneArgs(probe.arguments)
			byID["objective_id"] = h.objectiveA
			identified := probe.count(h.call(probe.tool, byID)["result"].(map[string]any))
			if keyed == 0 || keyed != identified {
				t.Fatalf("%s by key returned %d, by id %d — a key must select what the id selects", probe.tool, keyed, identified)
			}
			other := cloneArgs(probe.arguments)
			other["objective_id"] = "OBJ-ADDR-B"
			if got := probe.count(h.call(probe.tool, other)["result"].(map[string]any)); got == keyed && keyed != 0 {
				t.Fatalf("%s did not distinguish the two objectives", probe.tool)
			}
		})
	}

	// list_outputs is a filter with nothing to count here, so pin the negative:
	// an unresolvable reference must fail rather than answer "nothing found".
	t.Run("list_outputs", func(t *testing.T) {
		if listed := h.call("list_outputs", map[string]any{"objective_id": "OBJ-ADDR-A"}); listed["error"] != nil {
			t.Fatalf("list_outputs by key = %#v", listed["error"])
		}
		assertNotFound(t, h.raw("list_outputs", map[string]any{"objective_id": "OBJ-NOPE"}))
	})

	// Mutating tools: each must land on the objective the key names.
	t.Run("record_context", func(t *testing.T) {
		recorded := h.call("record_context", map[string]any{
			"actor_id": "agent:addr", "idempotency_key": "addr-context", "objective_id": "OBJ-ADDR-A",
			"kind": "requirement", "title": "Addressed by key", "status": "accepted",
		})
		if recorded["result"].(map[string]any)["objective_id"] != h.objectiveA {
			t.Fatalf("record_context landed on %#v", recorded["result"])
		}
	})
	t.Run("record_decision", func(t *testing.T) {
		recorded := h.call("record_decision", map[string]any{
			"actor_id": "agent:addr", "idempotency_key": "addr-decision", "objective_id": "OBJ-ADDR-A",
			"title": "Addressed by key", "decision": "The key reached the right objective.",
		})
		if recorded["result"].(map[string]any)["objective_id"] != h.objectiveA {
			t.Fatalf("record_decision landed on %#v", recorded["result"])
		}
	})
	t.Run("propose_plan", func(t *testing.T) {
		proposed := h.call("propose_plan", map[string]any{
			"actor_id": "agent:addr", "idempotency_key": "addr-plan", "objective_id": "OBJ-ADDR-A",
			"title": "Addressed by key", "items": []any{map[string]any{
				"client_ref": "only", "key": "TH-ADDR-PLAN", "title": "The plan's only item", "kind": "research",
				"priority": "medium", "estimated_scope": "small", "execution_policy": "agent_may_propose",
				"required_actor_kind": "any",
			}},
		})
		plan := proposed["result"].(map[string]any)["plan"].(map[string]any)
		if plan["objective_id"] != h.objectiveA {
			t.Fatalf("propose_plan landed on %#v", plan)
		}
	})
	t.Run("patch_objective", func(t *testing.T) {
		patched := h.call("patch_objective", map[string]any{
			"actor_id": "agent:addr", "idempotency_key": "addr-patch", "objective_id": "OBJ-ADDR-B",
			"expected_version": h.version("OBJ-ADDR-B"), "title": "Renamed through its key",
		})
		result := patched["result"].(map[string]any)
		if result["id"] != h.objectiveB || result["title"] != "Renamed through its key" {
			t.Fatalf("patch_objective landed on %#v", result)
		}
	})
	t.Run("transition_objective", func(t *testing.T) {
		moved := h.call("transition_objective", map[string]any{
			"actor_id": "agent:addr", "idempotency_key": "addr-transition", "objective_id": "OBJ-ADDR-B",
			"target_phase": "planning", "expected_version": h.version("OBJ-ADDR-B"),
			"reason": "Addressed by key, so the phase move must land on the same objective.",
		})
		result := moved["result"].(map[string]any)
		if result["id"] != h.objectiveB || result["phase"] != "planning" {
			t.Fatalf("transition_objective landed on %#v", result)
		}
	})
}

// TestVersionConflictAddressedByKeyStillCarriesCurrent pins the one error that
// exists to tell a caller which version to retry with. The lookups behind it are
// all by identifier, so the caller who has only the key — the caller keys exist
// for — was the one who got the conflict without the version.
func TestVersionConflictAddressedByKeyStillCarriesCurrent(t *testing.T) {
	h := newAddressingHarness(t)
	for _, tool := range []string{"patch_objective", "transition_objective"} {
		t.Run(tool, func(t *testing.T) {
			arguments := map[string]any{
				"actor_id": "agent:addr", "idempotency_key": "conflict-" + tool,
				"objective_id": "OBJ-ADDR-A", "expected_version": 99,
			}
			if tool == "patch_objective" {
				arguments["title"] = "Stale write"
			} else {
				arguments["target_phase"] = "planning"
				arguments["reason"] = "A stale write that must still say which version is current."
			}
			result := h.raw(tool, arguments)
			if !result.IsError {
				t.Fatalf("%s with a stale version succeeded", tool)
			}
			var payload struct {
				Error struct {
					Code    string         `json:"code"`
					Current map[string]any `json:"current"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(result.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error.Code != "version_conflict" {
				t.Fatalf("%s error code = %q", tool, payload.Error.Code)
			}
			if payload.Error.Current == nil {
				t.Fatalf("%s addressed by key lost its current block, so the caller cannot learn the version to retry with", tool)
			}
			if payload.Error.Current["id"] != h.objectiveA || payload.Error.Current["key"] != "OBJ-ADDR-A" {
				t.Fatalf("%s current block = %#v", tool, payload.Error.Current)
			}
		})
	}
}

// TestUnresolvableObjectiveIsNotFoundEverywhere pins the documented promise that
// a reference resolving to no objective fails, including where the field is only
// a filter: answering "no work here" for something that does not exist is a
// wrong answer, not an empty one.
func TestUnresolvableObjectiveIsNotFoundEverywhere(t *testing.T) {
	h := newAddressingHarness(t)
	for _, probe := range []struct {
		tool      string
		arguments map[string]any
	}{
		{"board_overview", map[string]any{}},
		{"list_items", map[string]any{}},
		{"get_changes", map[string]any{"since": "0"}},
		{"list_outputs", map[string]any{}},
		{"get_objective_context", map[string]any{"actor_id": "agent:addr"}},
	} {
		t.Run(probe.tool, func(t *testing.T) {
			arguments := cloneArgs(probe.arguments)
			arguments["objective_id"] = "OBJ-NOPE"
			assertNotFound(t, h.raw(probe.tool, arguments))
		})
	}
}

func assertNotFound(t *testing.T, result *protocol.CallToolResult) {
	t.Helper()
	text := result.Content[0].(*protocol.TextContent).Text
	if !result.IsError {
		t.Fatalf("an unresolvable objective_id answered successfully: %s", text)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "not_found" {
		t.Fatalf("unresolvable objective_id error code = %q, want not_found: %s", payload.Error.Code, text)
	}
}

// version reads an objective's current version the way a client would, so a
// subtest does not depend on what the ones before it happened to leave behind.
func (h *addressingHarness) version(reference string) int {
	h.t.Helper()
	context := h.call("get_objective_context", map[string]any{"objective_id": reference, "actor_id": "agent:addr"})
	objective := context["result"].(map[string]any)["objective"].(map[string]any)
	return int(objective["version"].(float64))
}

func cloneArgs(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
