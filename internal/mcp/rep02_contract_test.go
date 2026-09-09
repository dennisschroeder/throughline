package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/domain/work"
)

// rep02ToolInventory reads the advertised tool surface from a live server rather
// than from a list maintained beside it. A second hand-written inventory would
// agree with a wrong one by construction and gate nothing.
func rep02ToolInventory(t *testing.T) (mutations, readOnly []string) {
	t.Helper()
	ctx, session := newSession(t)
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			readOnly = append(readOnly, tool.Name)
			continue
		}
		mutations = append(mutations, tool.Name)
	}
	return mutations, readOnly
}

// rep02OutputSchema returns the schema the server actually advertises for a
// tool, decoded from the wire rather than recomputed by calling outputSchema.
func rep02OutputSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	ctx, session := newSession(t)
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != name {
			continue
		}
		encoded, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatal(err)
		}
		return schema
	}
	t.Fatalf("tool %q is not advertised", name)
	return nil
}

func TestREP02MutationOutputSchemasRequireEffects(t *testing.T) {
	mutations, _ := rep02ToolInventory(t)
	if got := len(mutations); got != 39 {
		t.Fatalf("server advertises %d mutating tools, want 39: %v", got, mutations)
	}
	for _, name := range mutations {
		t.Run(name, func(t *testing.T) {
			schema := rep02OutputSchema(t, name)
			properties := rep02SchemaMap(t, schema, "properties")
			required := rep02SchemaStrings(t, schema, "required")
			if !rep02Contains(required, "effects") {
				t.Fatalf("output schema does not require effects: %#v", required)
			}

			effects := rep02SchemaMap(t, properties, "effects")
			if effects["type"] != "array" {
				t.Fatalf("effects type = %#v, want array", effects["type"])
			}
			item := rep02SchemaMap(t, effects, "items")
			itemProperties := rep02SchemaMap(t, item, "properties")
			itemRequired := rep02SchemaStrings(t, item, "required")
			for _, field := range []string{"kind", "id", "version"} {
				if !rep02Contains(itemRequired, field) {
					t.Errorf("effect item does not require %q: %#v", field, itemRequired)
				}
			}
			kind := rep02SchemaMap(t, itemProperties, "kind")
			if kind["type"] != "string" {
				t.Errorf("effect kind schema = %#v, want string", kind)
			}
			id := rep02SchemaMap(t, itemProperties, "id")
			if id["type"] != "string" {
				t.Errorf("effect id schema = %#v, want string", id)
			}
			version := rep02SchemaMap(t, itemProperties, "version")
			if version["type"] != "integer" {
				t.Errorf("effect version schema = %#v, want integer", version)
			}
			if minimum, ok := rep02Number(version["minimum"]); !ok || minimum < 1 {
				t.Errorf("effect version minimum = %#v, want integer >= 1", version["minimum"])
			}
		})
	}
}

func TestREP02ReadOnlyOutputSchemasDoNotExposeEffects(t *testing.T) {
	_, readOnly := rep02ToolInventory(t)
	if got := len(readOnly); got != 11 {
		t.Fatalf("server advertises %d read-only tools, want 11: %v", got, readOnly)
	}
	for _, name := range readOnly {
		t.Run(name, func(t *testing.T) {
			schema := rep02OutputSchema(t, name)
			required := rep02SchemaStrings(t, schema, "required")
			if rep02Contains(required, "effects") {
				t.Fatalf("read-only output schema requires effects: %#v", required)
			}
			properties := rep02SchemaMap(t, schema, "properties")
			if _, present := properties["effects"]; present {
				t.Fatalf("read-only output schema exposes effects: %#v", properties["effects"])
			}
		})
	}
}

// TestREP02ReadOnlyResponsesOmitEffects checks the payload a client receives,
// not only the schema: the validated map and the returned map used to be built
// separately, which let a read-only response carry a null effects member the
// schema forbids.
func TestREP02ReadOnlyResponsesOmitEffects(t *testing.T) {
	ctx, session := newSession(t)
	for name, arguments := range map[string]map[string]any{
		"board_overview":     {"workspace_id": testWorkspaceID},
		"list_items":         {"workspace_id": testWorkspaceID},
		"list_ready_items":   {"workspace_id": testWorkspaceID, "actor_id": "agent:reader"},
		"get_changes":        {"workspace_id": testWorkspaceID},
		"get_semantic_model": {"section": "manifest"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("%s failed: %s", name, result.Content[0].(*protocol.TextContent).Text)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal([]byte(result.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
				t.Fatal(err)
			}
			if raw, present := payload["effects"]; present {
				t.Fatalf("read-only response carries effects: %s", raw)
			}
			if _, present := result.StructuredContent.(map[string]any)["effects"]; present {
				t.Fatalf("read-only structured content carries effects: %#v", result.StructuredContent)
			}
		})
	}
}

func rep02SchemaMap(t *testing.T, schema map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := schema[key].(map[string]any)
	if !ok {
		t.Fatalf("schema %q = %#v, want object", key, schema[key])
	}
	return value
}

func rep02SchemaStrings(t *testing.T, schema map[string]any, key string) []string {
	t.Helper()
	switch values := schema[key].(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				t.Fatalf("schema %q contains %#v, want strings", key, value)
			}
			result = append(result, text)
		}
		return result
	default:
		t.Fatalf("schema %q = %#v, want string array", key, schema[key])
		return nil
	}
}

func rep02Contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func rep02Number(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}

// TestREP02MutatingResponsesCarryCommittedEffects checks the wire, not the
// schema: a mutation must name the entities it changed with the versions it
// committed, and a retry must return the same list without writing again.
func TestREP02MutatingResponsesCarryCommittedEffects(t *testing.T) {
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
	call("register_actor", map[string]any{"actor_id": "agent:effects", "kind": "agent", "display_name": "Effects", "idempotency_key": "register-effects"})

	input := map[string]any{"actor_id": "agent:effects", "idempotency_key": "effects-objective", "key": "EFF-1", "title": "Report committed effects", "desired_outcome": "Callers never infer collateral changes", "phase": "discovery"}
	created := call("create_objective", input)
	objectiveID := created["result"].(map[string]any)["id"].(string)
	effects, ok := created["effects"].([]any)
	if !ok {
		t.Fatalf("create_objective returned no effects: %#v", created)
	}
	if len(effects) != 1 {
		t.Fatalf("create_objective effects = %#v, want exactly the objective", effects)
	}
	effect := effects[0].(map[string]any)
	if effect["kind"] != "objective" || effect["id"] != objectiveID || effect["version"] != float64(1) {
		t.Fatalf("create_objective effect = %#v", effect)
	}

	replayed := call("create_objective", input)
	if !reflect.DeepEqual(replayed["effects"], created["effects"]) {
		t.Fatalf("replayed effects = %#v, want %#v", replayed["effects"], created["effects"])
	}

	// A mutation whose transaction changes more than the entity it returns must
	// name the collateral entity too.
	item := call("create_item", map[string]any{
		"actor_id": "agent:effects", "idempotency_key": "effects-item", "key": "EFF-ITEM",
		"objective_id": objectiveID, "title": "Carry an acceptance criterion", "kind": "research",
		"acceptance_criteria": []any{map[string]any{"text": "The collateral criterion is reported.", "required": true, "ordinal": 1}},
	})
	itemEffects := item["effects"].([]any)
	kinds := make(map[string]bool, len(itemEffects))
	for _, raw := range itemEffects {
		entry := raw.(map[string]any)
		kinds[entry["kind"].(string)] = true
		if entry["id"] == "" || entry["version"].(float64) < 1 {
			t.Fatalf("create_item effect = %#v", entry)
		}
	}
	for _, kind := range []string{"work_item", "acceptance_criterion"} {
		if !kinds[kind] {
			t.Fatalf("create_item effects %#v omit %s", itemEffects, kind)
		}
	}
}

// TestREP02OutputValidationRejectsAZeroEffectVersion gates the enforcement, not
// the advertisement. The effect schema promises "minimum": 1; asserting only
// that the promise appears in the schema leaves the validator free to ignore it,
// which is how the bound became decoration once already. It goes through
// validatedToolResult, the single function every successful tool response is
// built by, so removing the check anywhere on that path fails here.
func TestREP02OutputValidationRejectsAZeroEffectVersion(t *testing.T) {
	payload := func(version int) map[string]any {
		return map[string]any{
			"workspace": map[string]any{"id": testWorkspaceID, "change_cursor": "1"},
			"result":    snakeCaseValue(work.Objective{ID: "OBJ-1", Key: "OBJ-1", Title: "Gate the bound", Phase: work.ObjectiveIdea, Version: 1}),
			"effects":   snakeCaseValue([]app.Effect{{Kind: "objective", ID: "OBJ-1", Version: version}}),
		}
	}
	valid := validatedToolResult(context.Background(), "create_objective", false, payload(1))
	if valid.IsError {
		t.Fatalf("valid effect rejected: %s", valid.Content[0].(*protocol.TextContent).Text)
	}
	for name, version := range map[string]int{"zero": 0, "negative": -1} {
		t.Run(name, func(t *testing.T) {
			result := validatedToolResult(context.Background(), "create_objective", false, payload(version))
			if !result.IsError {
				t.Fatalf("effect version %d passed output validation: %s", version, result.Content[0].(*protocol.TextContent).Text)
			}
			if !strings.Contains(result.Content[0].(*protocol.TextContent).Text, "output_validation_failed") {
				t.Fatalf("unexpected error for version %d: %s", version, result.Content[0].(*protocol.TextContent).Text)
			}
		})
	}
}
