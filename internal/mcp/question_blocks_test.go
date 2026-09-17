package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestQuestionBlockingAndAttentionOverTheWire pins REP-08's MCP surface:
// ask_question's blocks_item_ids, the requires_human_attention alias,
// sharpen_question, link_question_blocker, the blocking questions get_item
// reports, board_overview's questions_needing_human_attention, and the
// attention target kinds that are now refused.
func TestQuestionBlockingAndAttentionOverTheWire(t *testing.T) {
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
	must("register_actor", map[string]any{"actor_id": "agent:planner", "kind": "agent", "display_name": "Planner", "idempotency_key": "register"})
	objective := must("create_objective", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "objective", "key": "OBJ-QUESTIONS",
		"title": "Questions hold work", "desired_outcome": "Blocked work is visible.", "phase": "planning",
	})
	item := func(key string) string {
		created := must("create_item", map[string]any{
			"actor_id": "agent:planner", "idempotency_key": "item-" + key, "key": key, "objective_id": objective["id"],
			"title": "Item " + key, "kind": "task",
		})
		return created["id"].(string)
	}
	first, second := item("Q-1"), item("Q-2")

	asked := must("ask_question", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "ask", "objective_id": "OBJ-QUESTIONS",
		"question": "How retention interacts with export", "status": "unsharp",
		"blocks_item_ids": []any{first}, "requires_human_attention": true,
	})
	if asked["status"] != "unsharp" || asked["attention_state"] != "needs_human_decision" {
		t.Fatalf("asked question = %#v, want unsharp and needs_human_decision from the legacy flag", asked)
	}
	if blocks, _ := asked["blocks_work_items"].([]any); len(blocks) != 1 || blocks[0] != first {
		t.Fatalf("asked question blocks = %#v, want [%s]", asked["blocks_work_items"], first)
	}
	// A resuming session must see the unsharp question: hiding it would make
	// an objective whose unphrased areas still hold work look settled.
	objectiveContext := must("get_objective_context", map[string]any{"objective_id": "OBJ-QUESTIONS", "include": []any{"open_questions"}})
	if questions, _ := objectiveContext["questions"].([]any); len(questions) != 1 || questions[0].(map[string]any)["status"] != "unsharp" {
		t.Fatalf("objective context questions = %#v, want the unsharp question", objectiveContext["questions"])
	}
	sharpened := must("sharpen_question", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "sharpen", "question_id": asked["id"],
		"expected_version": asked["version"], "question": "Does export extend retention?",
	})
	if sharpened["status"] != "open" || sharpened["text"] != "Does export extend retention?" {
		t.Fatalf("sharpened question = %#v", sharpened)
	}
	linked := must("link_question_blocker", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "link", "question_id": asked["id"],
		"expected_version": sharpened["version"], "work_item_id": second,
	})
	if blocks, _ := linked["blocks_work_items"].([]any); len(blocks) != 2 {
		t.Fatalf("linked question blocks = %#v, want both items", linked["blocks_work_items"])
	}

	detail := must("get_item", map[string]any{"id": second, "include": []any{"questions"}})
	if questions, _ := detail["blocking_questions"].([]any); len(questions) != 1 || questions[0].(map[string]any)["id"] != asked["id"] {
		t.Fatalf("get_item blocking questions = %#v", detail["blocking_questions"])
	}

	if trimmed := must("get_item", map[string]any{"id": second, "include": []any{"dependencies"}}); trimmed["blocking_questions"] != nil {
		t.Fatalf("get_item without the questions section = %#v, want blocking_questions omitted", trimmed["blocking_questions"])
	}

	// A second objective with its own flagged question, and a flagged question
	// here that is then answered: neither may appear in this objective's list.
	must("create_objective", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "other-objective", "key": "OBJ-OTHER",
		"title": "Elsewhere", "desired_outcome": "Stays apart.", "phase": "planning",
	})
	must("ask_question", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "ask-other", "objective_id": "OBJ-OTHER",
		"question": "Unrelated?", "attention_state": "needs_human_review",
	})
	must("ask_question", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "ask-plain", "objective_id": "OBJ-QUESTIONS",
		"question": "Unflagged and unresolved?",
	})
	resolved := must("ask_question", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "ask-resolved", "objective_id": "OBJ-QUESTIONS",
		"question": "Settled soon?", "attention_state": "needs_human_review",
	})
	must("answer_question", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "answer-resolved", "question_id": resolved["id"],
		"expected_version": resolved["version"], "answer": "Yes.",
	})
	overview := must("board_overview", map[string]any{"objective_id": "OBJ-QUESTIONS", "include_attention": true})
	if flagged, _ := overview["questions_needing_human_attention"].([]any); len(flagged) != 1 || flagged[0].(map[string]any)["id"] != asked["id"] {
		t.Fatalf("board_overview questions needing attention = %#v, want only this objective's unresolved flagged question", overview["questions_needing_human_attention"])
	}
	if quiet := must("board_overview", map[string]any{"objective_id": "OBJ-QUESTIONS"}); quiet["questions_needing_human_attention"] != nil {
		t.Fatalf("board_overview without include_attention = %#v, want no questions", quiet["questions_needing_human_attention"])
	}
	if payload, text := call("ask_question", map[string]any{
		"actor_id": "agent:planner", "idempotency_key": "ask-conflict", "objective_id": "OBJ-QUESTIONS",
		"question": "Conflicting?", "requires_human_attention": true, "attention_state": "needs_human_review",
	}); payload["error"] == nil || !strings.Contains(text, "conflicts") {
		t.Fatalf("ask_question with a conflicting legacy flag = %s, want it refused", text)
	}

	for _, kind := range []string{"decision", "review"} {
		payload, text := call("request_attention", map[string]any{
			"actor_id": "agent:planner", "idempotency_key": "attention-" + kind, "target_kind": kind,
			"target_id": asked["id"], "attention_state": "needs_human_review",
		})
		if payload["error"] == nil || !strings.Contains(text, "not supported") {
			t.Fatalf("request_attention on %s = %s, want it refused", kind, text)
		}
	}
}
