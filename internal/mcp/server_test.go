package mcp

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/workgraph/internal/app"
	workgraphsqlite "github.com/dennisschroeder/workgraph/internal/sqlite"
)

func TestToolsExposeStableErrorsAndReadAnnotations(t *testing.T) {
	ctx := context.Background()
	database, err := workgraphsqlite.Open(ctx, filepath.Join(t.TempDir(), "workgraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := protocol.NewInMemoryTransports()
	server := NewServer(app.NewService(database.Store(), app.UUIDv7Generator{}, app.SystemClock{}))
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := protocol.NewClient(&protocol.Implementation{Name: "mcp-test", Version: "v1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, tool := range tools.Tools {
		if tool.Name == "get_changes" {
			found = true
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("get_changes annotations = %#v", tool.Annotations)
			}
		}
	}
	if !found {
		t.Fatal("get_changes was not advertised")
	}

	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "get_item", Arguments: map[string]any{"id": "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("missing item did not return a tool error")
	}
	text := result.Content[0].(*protocol.TextContent).Text
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "not_found" {
		t.Fatalf("error code = %q", payload.Error.Code)
	}
}

func TestCreateObjectiveReplaysAndVersionConflictIncludesCurrent(t *testing.T) {
	ctx, session := newSession(t)
	call := func(name string, arguments map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	call("register_actor", map[string]any{"actor_id": "agent:researcher", "kind": "agent", "display_name": "Researcher", "idempotency_key": "register"})
	input := map[string]any{"actor_id": "agent:researcher", "idempotency_key": "objective", "key": "RES-1", "title": "Research local archival options", "desired_outcome": "A reusable research dossier", "phase": "discovery"}
	first := call("create_objective", input)
	second := call("create_objective", input)
	firstResult := first["result"].(map[string]any)
	secondResult := second["result"].(map[string]any)
	if firstResult["ID"] != secondResult["ID"] {
		t.Fatalf("idempotent replay IDs = %v and %v", firstResult["ID"], secondResult["ID"])
	}
	conflict := call("transition_objective", map[string]any{"objective_id": firstResult["ID"], "actor_id": "agent:researcher", "target_phase": "planning", "expected_version": 99, "idempotency_key": "conflict"})
	errorPayload := conflict["error"].(map[string]any)
	if errorPayload["code"] != "version_conflict" {
		t.Fatalf("error code = %v", errorPayload["code"])
	}
	current, ok := errorPayload["current"].(map[string]any)
	if !ok || current["id"] != firstResult["ID"] || current["version"] != float64(1) {
		t.Fatalf("current = %#v", errorPayload["current"])
	}
}

func newSession(t *testing.T) (context.Context, *protocol.ClientSession) {
	t.Helper()
	ctx := context.Background()
	database, err := workgraphsqlite.Open(ctx, filepath.Join(t.TempDir(), "workgraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := protocol.NewInMemoryTransports()
	server := NewServer(app.NewService(database.Store(), app.UUIDv7Generator{}, app.SystemClock{}))
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := protocol.NewClient(&protocol.Implementation{Name: "mcp-test", Version: "v1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return ctx, session
}

func TestStdioTwoClientNonCodeWorkflowSmoke(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	binary := filepath.Join(t.TempDir(), "workgraph")
	build := exec.Command("go", "build", "-o", binary, "./cmd/workgraph")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build server: %v\n%s", err, output)
	}
	if output, err := exec.Command(binary, "init", root).CombinedOutput(); err != nil {
		t.Fatalf("init workspace: %v\n%s", err, output)
	}
	open := func(name string) *protocol.ClientSession {
		t.Helper()
		client := protocol.NewClient(&protocol.Implementation{Name: name, Version: "v1"}, nil)
		session, err := client.Connect(ctx, &protocol.CommandTransport{Command: exec.Command(binary, "mcp", root)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	clientA, clientB := open("researcher"), open("reviewer")
	call := func(session *protocol.ClientSession, name string, arguments map[string]any) (map[string]any, bool) {
		t.Helper()
		response, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(response.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
			t.Fatal(err)
		}
		return payload, response.IsError
	}
	for _, actor := range []struct{ id, kind, key string }{{"agent:researcher", "agent", "register-a"}, {"human:reviewer", "agent", "register-b"}} {
		if _, failed := call(clientA, "register_actor", map[string]any{"actor_id": actor.id, "kind": actor.kind, "display_name": actor.id, "idempotency_key": actor.key}); failed {
			t.Fatalf("register %s failed", actor.id)
		}
	}
	baseline, failed := call(clientA, "get_changes", map[string]any{})
	if failed {
		t.Fatal("baseline changes failed")
	}
	cursor := baseline["workspace"].(map[string]any)["change_cursor"]
	created, failed := call(clientA, "create_objective", map[string]any{"actor_id": "agent:researcher", "idempotency_key": "objective", "key": "RES-1", "title": "Archive research workflow", "desired_outcome": "A validated research dossier", "phase": "discovery"})
	if failed {
		t.Fatal("create objective failed")
	}
	objective := created["result"].(map[string]any)
	plan, failed := call(clientA, "propose_plan", map[string]any{"objective_id": objective["ID"], "actor_id": "agent:researcher", "title": "Research local archives", "revision": 1, "items": []any{map[string]any{"ClientRef": "dossier", "Key": "RES-1", "Title": "Produce archive dossier", "Kind": "research", "Priority": "high", "EstimatedScope": "small", "ExecutionPolicy": "autonomous_with_report", "RequiredActorKind": "agent", "ExpectedOutputs": []any{map[string]any{"Name": "Dossier", "ProfileName": "structured_document", "ProfileVersion": 1, "Required": true, "Ordinal": 1}}}}})
	if failed {
		t.Fatal("propose plan failed")
	}
	planResult := plan["result"].(map[string]any)
	planDetail := planResult["Plan"].(map[string]any)
	if _, failed := call(clientB, "review_plan", map[string]any{"plan_id": planDetail["ID"], "actor_id": "human:reviewer", "decision": "approved", "reason": "Research scope approved", "expected_version": 1}); failed {
		t.Fatal("review plan failed")
	}
	if transitioned, failed := call(clientA, "transition_objective", map[string]any{"objective_id": objective["ID"], "actor_id": "agent:researcher", "target_phase": "planning", "reason": "Discovery completed", "expected_version": 1, "idempotency_key": "planning"}); failed {
		t.Fatalf("objective planning transition failed: %#v", transitioned)
	}
	if transitioned, failed := call(clientA, "transition_objective", map[string]any{"objective_id": objective["ID"], "actor_id": "agent:researcher", "target_phase": "execution", "reason": "Plan approved", "expected_version": 2, "idempotency_key": "execute"}); failed {
		t.Fatalf("objective transition failed: %#v", transitioned)
	}
	contextPayload, failed := call(clientA, "get_objective_context", map[string]any{"objective_id": objective["ID"]})
	if failed {
		t.Fatal("objective context failed")
	}
	items := contextPayload["result"].(map[string]any)["actor_relevant_work"].([]any)
	itemContext := items[0].(map[string]any)
	item := itemContext["WorkItem"].(map[string]any)
	expectedID := itemContext["ExpectedOutputs"].([]any)[0].(map[string]any)["ExpectedOutput"].(map[string]any)["ID"]
	ready, failed := call(clientA, "transition_item", map[string]any{"id": item["ID"], "actor_id": "agent:researcher", "target_status": "ready", "expected_version": item["Version"], "idempotency_key": "ready"})
	if failed {
		t.Fatalf("ready transition failed: %#v", ready)
	}
	readyItem := ready["result"].(map[string]any)
	claim, failed := call(clientA, "claim_item", map[string]any{"id": item["ID"], "actor_id": "agent:researcher", "expected_version": readyItem["Version"], "idempotency_key": "claim", "lease_seconds": 300, "transition_to_in_progress": true})
	if failed {
		t.Fatalf("claim failed: %#v", claim)
	}
	claimedItem := claim["result"].(map[string]any)["WorkItem"].(map[string]any)
	conflict, failed := call(clientB, "claim_item", map[string]any{"id": item["ID"], "actor_id": "human:reviewer", "expected_version": claimedItem["Version"], "idempotency_key": "claim-conflict", "lease_seconds": 300, "transition_to_in_progress": true})
	if !failed || conflict["error"].(map[string]any)["code"] != "claim_conflict" {
		t.Fatalf("claim conflict = %#v", conflict)
	}
	progress, failed := call(clientA, "append_progress", map[string]any{"id": item["ID"], "actor_id": "agent:researcher", "expected_version": claimedItem["Version"], "idempotency_key": "progress", "summary": "Sources catalogued"})
	if failed {
		t.Fatal("progress failed")
	}
	progressItem := progress["result"].(map[string]any)["WorkItem"].(map[string]any)
	revision, failed := call(clientA, "create_output_revision", map[string]any{"expected_output_id": expectedID, "actor_id": "agent:researcher", "artifacts": []any{map[string]any{"kind": "document", "uri": "file:///archive-dossier.md", "title": "Archive dossier", "metadata": map[string]any{}, "role": "primary"}}})
	if failed {
		t.Fatal("create revision failed")
	}
	if _, failed := call(clientB, "record_validation", map[string]any{"output_revision_id": revision["result"].(map[string]any)["ID"], "actor_id": "human:reviewer", "criterion_ref": "structure", "validator_kind": "structure", "verdict": "passed", "details": map[string]any{}}); failed {
		t.Fatal("validate output failed")
	}
	if outputs, failed := call(clientB, "list_outputs", map[string]any{"profile_name": "structured_document", "version_constraint": "=1"}); failed || len(outputs["result"].([]any)) == 0 {
		t.Fatalf("accepted output discovery = %#v", outputs)
	}
	evidence, failed := call(clientA, "attach_artifact", map[string]any{"work_item_id": item["ID"], "actor_id": "agent:researcher", "expected_version": progressItem["Version"], "idempotency_key": "evidence", "kind": "receipt", "uri": "file:///archive-consent.txt"})
	if failed {
		t.Fatalf("attach evidence failed: %#v", evidence)
	}
	evidenceResult := evidence["result"].(map[string]any)
	actionSubject := map[string]any{"action_type": "archive.request", "target": map[string]any{"collection": "city-records"}, "arguments": []any{}, "scope": map[string]any{"project": "archives"}, "permissions": []any{"network.read"}, "credential_requirements": []any{}, "constraints": map[string]any{}}
	action, failed := call(clientA, "propose_external_action", map[string]any{"work_item_id": item["ID"], "actor_id": "agent:researcher", "expected_version": evidenceResult["WorkItem"].(map[string]any)["Version"], "idempotency_key": "archive-action", "title": "Request archive access", "authorization_subject": actionSubject})
	if failed {
		t.Fatalf("propose action failed: %#v", action)
	}
	actionResult := action["result"].(map[string]any)
	actionDetail := actionResult["Action"].(map[string]any)
	revisionDetail := actionResult["Revision"].(map[string]any)
	approval, failed := call(clientB, "request_action_approval", map[string]any{"action_id": actionDetail["ID"], "actor_id": "human:reviewer", "approved_for_actor_id": "agent:researcher", "expected_action_version": actionDetail["Version"], "idempotency_key": "approve-request", "constraints": map[string]any{}, "request": "Approve archive access request"})
	if failed {
		t.Fatalf("request approval failed: %#v", approval)
	}
	resolved, failed := call(clientB, "resolve_action_approval", map[string]any{"approval_id": approval["result"].(map[string]any)["ID"], "actor_id": "human:reviewer", "expected_action_version": actionDetail["Version"], "idempotency_key": "approve", "decision": "approved", "rationale": "Access is scoped"})
	if failed {
		t.Fatalf("resolve approval failed: %#v", resolved)
	}
	resolvedResult := resolved["result"].(map[string]any)
	started, failed := call(clientA, "record_external_action_execution", map[string]any{"action_id": actionDetail["ID"], "actor_id": "agent:researcher", "expected_action_version": resolvedResult["Action"].(map[string]any)["Version"], "idempotency_key": "start", "subject_hash": revisionDetail["AuthorizationSubjectHash"], "authority_grant_id": resolvedResult["Grant"].(map[string]any)["ID"], "state": "started"})
	if failed {
		t.Fatalf("record execution start failed: %#v", started)
	}
	startedResult := started["result"].(map[string]any)
	if completed, failed := call(clientA, "record_external_action_execution", map[string]any{"execution_id": startedResult["Execution"].(map[string]any)["ID"], "actor_id": "agent:researcher", "expected_action_version": startedResult["Action"].(map[string]any)["Version"], "idempotency_key": "complete", "state": "succeeded", "result": map[string]any{"receipt": "recorded"}, "evidence_artifact_id": evidenceResult["Artifact"].(map[string]any)["ID"]}); failed {
		t.Fatalf("record execution result failed: %#v", completed)
	}
	deltas, failed := call(clientB, "get_changes", map[string]any{"since": cursor})
	if failed || len(deltas["result"].(map[string]any)["changes"].([]any)) == 0 {
		t.Fatalf("cursor deltas = %#v", deltas)
	}
}
