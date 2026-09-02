package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/config"
	"github.com/dennisschroeder/throughline/internal/domain/authority"
	"github.com/dennisschroeder/throughline/internal/registry"
	"github.com/dennisschroeder/throughline/internal/router"
)

func TestToolsExposeStableErrorsAndReadAnnotations(t *testing.T) {
	ctx, session := newSession(t)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var foundSemantic, foundChanges bool
	for _, tool := range tools.Tools {
		if tool.Name == "get_semantic_model" {
			foundSemantic = true
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("get_semantic_model annotations = %#v", tool.Annotations)
			}
		}
		if tool.Name == "get_changes" {
			foundChanges = true
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("get_changes annotations = %#v", tool.Annotations)
			}
		}
	}
	if !foundSemantic || !foundChanges {
		t.Fatal("read-only semantic/change tool was not advertised")
	}
	semantic, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "get_semantic_model", Arguments: map[string]any{"section": "entities", "ids": []any{"work_item", "unknown"}}})
	if err != nil {
		t.Fatal(err)
	}
	if semantic.IsError {
		t.Fatalf("semantic model call failed: %s", semantic.Content[0].(*protocol.TextContent).Text)
	}
	var semanticPayload struct {
		Result struct {
			Section       string   `json:"section"`
			ContentDigest string   `json:"content_digest"`
			Data          []any    `json:"data"`
			NotFound      []string `json:"not_found_ids"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(semantic.Content[0].(*protocol.TextContent).Text), &semanticPayload); err != nil {
		t.Fatal(err)
	}
	if semanticPayload.Result.Section != "entities" || semanticPayload.Result.ContentDigest == "" || len(semanticPayload.Result.Data) != 1 || len(semanticPayload.Result.NotFound) != 1 {
		t.Fatalf("semantic payload = %#v", semanticPayload)
	}

	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "get_item", Arguments: map[string]any{"workspace_id": testWorkspaceID, "id": "missing"}})
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

func TestEveryMutationAdvertisesActorAndIdempotencyKey(t *testing.T) {
	ctx, session := newSession(t)
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			continue
		}
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"actor_id", "idempotency_key"} {
			if !strings.Contains(string(encoded), `"`+field+`"`) {
				t.Fatalf("%s schema omits %s", tool.Name, field)
			}
		}
	}
}

func TestCreateObjectiveReplaysAndVersionConflictIncludesCurrent(t *testing.T) {
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
		return payload
	}
	call("register_actor", map[string]any{"actor_id": "agent:researcher", "kind": "agent", "display_name": "Researcher", "idempotency_key": "register"})
	input := map[string]any{"actor_id": "agent:researcher", "idempotency_key": "objective", "key": "RES-1", "title": "Research local archival options", "desired_outcome": "A reusable research dossier", "phase": "discovery"}
	first := call("create_objective", input)
	if first["error"] != nil {
		t.Fatalf("create objective = %#v", first)
	}
	firstCursor := first["workspace"].(map[string]any)["change_cursor"]
	if patched := call("patch_objective", map[string]any{"objective_id": first["result"].(map[string]any)["id"], "actor_id": "agent:researcher", "idempotency_key": "patch", "expected_version": 1, "title": "Updated objective"}); patched["error"] != nil {
		t.Fatalf("patch objective = %#v", patched)
	}
	second := call("create_objective", input)
	firstResult := first["result"].(map[string]any)
	secondResult := second["result"].(map[string]any)
	if firstResult["id"] != secondResult["id"] {
		t.Fatalf("idempotent replay IDs = %v and %v", firstResult["id"], secondResult["id"])
	}
	if second["workspace"].(map[string]any)["change_cursor"] != firstCursor {
		t.Fatalf("replayed change cursor = %v, want %v", second["workspace"].(map[string]any)["change_cursor"], firstCursor)
	}
	conflict := call("transition_objective", map[string]any{"objective_id": firstResult["id"], "actor_id": "agent:researcher", "target_phase": "planning", "expected_version": 99, "idempotency_key": "conflict"})
	errorPayload := conflict["error"].(map[string]any)
	if errorPayload["code"] != "version_conflict" {
		t.Fatalf("error code = %v", errorPayload["code"])
	}
	current, ok := errorPayload["current"].(map[string]any)
	if !ok || current["id"] != firstResult["id"] || current["version"] != float64(2) {
		t.Fatalf("current = %#v", errorPayload["current"])
	}
}

// TestTransitionContextEnforcesVersionAndKindLifecycle proves transition_context reaches
// Service.TransitionContext: a stale expected_version is rejected rather than silently
// applied, and the kind-specific lifecycle (untested -> validating -> validated) is enforced
// by the domain, not re-implemented in the MCP layer — a direct untested -> validated jump
// is rejected too.
func TestTransitionContextEnforcesVersionAndKindLifecycle(t *testing.T) {
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
		return payload
	}
	mustOK := func(name string, arguments map[string]any) map[string]any {
		t.Helper()
		payload := call(name, arguments)
		if payload["error"] != nil {
			t.Fatalf("%s = %#v", name, payload)
		}
		return payload["result"].(map[string]any)
	}

	mustOK("register_actor", map[string]any{"actor_id": "agent:metrics", "kind": "agent", "display_name": "Metrics", "idempotency_key": "transition-context-actor"})
	objective := mustOK("create_objective", map[string]any{"actor_id": "agent:metrics", "idempotency_key": "transition-context-objective", "key": "TC-1", "title": "Transition context coverage", "desired_outcome": "Prove the lifecycle is domain-enforced", "phase": "discovery"})

	metric := mustOK("record_context", map[string]any{"objective_id": objective["id"], "actor_id": "agent:metrics", "idempotency_key": "transition-context-metric", "kind": "success_metric", "title": "p95 latency under 200ms", "status": "untested"})

	validating := mustOK("transition_context", map[string]any{"context_record_id": metric["id"], "actor_id": "agent:metrics", "idempotency_key": "transition-context-validating", "target_status": "validating", "expected_version": metric["version"]})
	if validating["status"] != "validating" {
		t.Fatalf("status = %v, want validating", validating["status"])
	}
	if validating["version"] != float64(2) {
		t.Fatalf("version = %v, want 2", validating["version"])
	}

	stale := call("transition_context", map[string]any{"context_record_id": metric["id"], "actor_id": "agent:metrics", "idempotency_key": "transition-context-stale", "target_status": "validated", "expected_version": 1})
	staleError, ok := stale["error"].(map[string]any)
	if !ok || staleError["code"] != "version_conflict" {
		t.Fatalf("stale transition error = %#v", stale)
	}

	skipped := mustOK("record_context", map[string]any{"objective_id": objective["id"], "actor_id": "agent:metrics", "idempotency_key": "transition-context-metric-2", "kind": "success_metric", "title": "error budget respected", "status": "untested"})
	skip := call("transition_context", map[string]any{"context_record_id": skipped["id"], "actor_id": "agent:metrics", "idempotency_key": "transition-context-skip", "target_status": "validated", "expected_version": skipped["version"]})
	skipError, ok := skip["error"].(map[string]any)
	if !ok || skipError["code"] != "validation_failed" || !strings.Contains(skipError["message"].(string), "cannot transition") {
		t.Fatalf("untested->validated error = %#v", skip)
	}
}

func TestProposePlanUsesStrictSnakeCaseNestedInput(t *testing.T) {
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
		return payload
	}
	if payload := call("register_actor", map[string]any{"actor_id": "agent:planner", "kind": "agent", "display_name": "Planner", "idempotency_key": "actor"}); payload["error"] != nil {
		t.Fatalf("register actor = %#v", payload)
	}
	objective := call("create_objective", map[string]any{"actor_id": "agent:planner", "idempotency_key": "objective", "key": "OBJ-1", "title": "Objective", "desired_outcome": "Outcome", "phase": "planning"})["result"].(map[string]any)
	payload := call("propose_plan", map[string]any{"objective_id": objective["id"], "actor_id": "agent:planner", "idempotency_key": "plan", "title": "Plan", "revision": 1, "items": []any{map[string]any{"client_ref": "one", "key": "TH-1", "title": "One", "kind": "research", "priority": "medium", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent", "unexpected": true}}})
	if payload["error"] == nil {
		t.Fatalf("nested unknown field accepted: %#v", payload)
	}
	if payload["error"].(map[string]any)["code"] != "validation_failed" {
		t.Fatalf("error = %#v", payload)
	}
}

func TestWorkspaceScopedToolsFailClosedOnMissingAndUnknownWorkspaceID(t *testing.T) {
	ctx, session := newSession(t)

	missing, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "board_overview", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !missing.IsError {
		t.Fatal("board_overview without workspace_id was accepted")
	}
	var missingPayload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(missing.Content[0].(*protocol.TextContent).Text), &missingPayload); err != nil {
		t.Fatal(err)
	}
	if missingPayload.Error.Code != "workspace_required" {
		t.Fatalf("missing workspace_id error code = %q, want workspace_required", missingPayload.Error.Code)
	}

	unknown, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "board_overview", Arguments: map[string]any{"workspace_id": "no-such-workspace"}})
	if err != nil {
		t.Fatal(err)
	}
	if !unknown.IsError {
		t.Fatal("board_overview with an unknown workspace_id was accepted")
	}
	var unknownPayload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(unknown.Content[0].(*protocol.TextContent).Text), &unknownPayload); err != nil {
		t.Fatal(err)
	}
	if unknownPayload.Error.Code != "workspace_not_found" {
		t.Fatalf("unknown workspace_id error code = %q, want workspace_not_found", unknownPayload.Error.Code)
	}
}

func TestGetSemanticModelNeedsNoWorkspaceID(t *testing.T) {
	ctx, session := newSession(t)
	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "get_semantic_model", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("get_semantic_model without workspace_id failed: %s", result.Content[0].(*protocol.TextContent).Text)
	}
}

// TestConcurrentRequestsRouteDistinctWorkspacesIndependently proves the central per-request
// wrapper, not a connection or session, decides which workspace a call reaches: two
// workspaces served by one Router behind one HTTP endpoint never see each other's writes,
// even when their requests interleave concurrently.
func TestConcurrentRequestsRouteDistinctWorkspacesIndependently(t *testing.T) {
	ctx := context.Background()
	fakeRegistry := newTestRegistry()
	workspaceA, _, err := config.Initialize(t.TempDir(), "", "workspace-a")
	if err != nil {
		t.Fatal(err)
	}
	workspaceB, _, err := config.Initialize(t.TempDir(), "", "workspace-b")
	if err != nil {
		t.Fatal(err)
	}
	fakeRegistry.register(t, workspaceA)
	fakeRegistry.register(t, workspaceB)
	workspaceRouter := router.New(fakeRegistry, router.NewProviderManager(router.SQLiteProvider{}), app.UUIDv7Generator{}, app.SystemClock{}, 0)
	t.Cleanup(func() { _ = workspaceRouter.Close() })

	httpServer := httptest.NewServer(Handler(workspaceRouter))
	t.Cleanup(httpServer.Close)

	open := func() *protocol.ClientSession {
		t.Helper()
		client := protocol.NewClient(&protocol.Implementation{Name: "concurrent-test", Version: "v1"}, nil)
		session, err := client.Connect(ctx, &protocol.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}

	const attemptsPerWorkspace = 8
	var wg sync.WaitGroup
	errs := make(chan error, attemptsPerWorkspace*2)
	createInWorkspace := func(workspaceID, key string) {
		defer wg.Done()
		session := open()
		result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "create_objective", Arguments: map[string]any{
			"workspace_id": workspaceID, "actor_id": "agent:concurrent", "idempotency_key": key,
			"key": key, "title": "Concurrent objective", "desired_outcome": "Isolation check", "phase": "discovery",
		}})
		if err != nil {
			errs <- err
			return
		}
		if result.IsError {
			errs <- fmt.Errorf("%s: %s", key, result.Content[0].(*protocol.TextContent).Text)
		}
	}
	for i := 0; i < attemptsPerWorkspace; i++ {
		wg.Add(2)
		go createInWorkspace(workspaceA.Config.WorkspaceID, fmt.Sprintf("OBJ-A-%d", i))
		go createInWorkspace(workspaceB.Config.WorkspaceID, fmt.Sprintf("OBJ-B-%d", i))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	// Every idempotency key was unique per workspace; a leak across workspaces would have
	// surfaced above as a spurious idempotency collision or version conflict. Confirm both
	// workspaces are still independently queryable after the concurrent storm.
	verify := open()
	for _, workspaceID := range []string{workspaceA.Config.WorkspaceID, workspaceB.Config.WorkspaceID} {
		result, err := verify.CallTool(ctx, &protocol.CallToolParams{Name: "list_items", Arguments: map[string]any{"workspace_id": workspaceID}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("list_items for %s failed after concurrent writes: %s", workspaceID, result.Content[0].(*protocol.TextContent).Text)
		}
	}
}

// TestParallelGraphNodesClaimConflictRereadAndReplayIndependently exercises the accepted
// "distinct logical actor identity for every parallel graph node" contract
// (agent:<harness>:<run_id>:<node_key>) through the full HTTP+router+MCP stack: two nodes
// of the same run race a claim, the loser gets a claim_conflict envelope carrying enough
// current state to reread and decide whether to retry, and idempotent replay/mismatch
// detection both work per-actor. Claim lease expiry itself is exercised at the domain layer
// (internal/domain/work/coordination_test.go); this test proves routing/HTTP does not
// interfere with the identity, claim, or idempotency semantics that layer guarantees.
func TestParallelGraphNodesClaimConflictRereadAndReplayIndependently(t *testing.T) {
	ctx, session := newSession(t)
	runID := "01996f20-9a10-7000-8000-000000000000" // a fixed UUIDv7-shaped run_id
	nodeA := "agent:codex:" + runID + ":dossier"
	nodeB := "agent:codex:" + runID + ":review"

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
		return payload
	}

	for _, actor := range []string{nodeA, nodeB} {
		if payload := call("register_actor", map[string]any{"actor_id": actor, "kind": "agent", "display_name": actor, "idempotency_key": "register-" + actor}); payload["error"] != nil {
			t.Fatalf("register %s = %#v", actor, payload)
		}
	}
	if payload := call("register_actor", map[string]any{"actor_id": "human:reviewer", "kind": "human", "display_name": "Reviewer", "idempotency_key": "register-reviewer-" + runID}); payload["error"] != nil {
		t.Fatalf("register reviewer = %#v", payload)
	}
	objective := call("create_objective", map[string]any{
		"actor_id": nodeA, "idempotency_key": "objective-" + runID, "key": "OBJ-PARALLEL-" + runID[:8],
		"title": "Parallel graph run", "desired_outcome": "Distinct node identities are enforced.", "phase": "discovery",
	})["result"].(map[string]any)
	plan := call("propose_plan", map[string]any{
		"objective_id": objective["id"], "actor_id": nodeA, "idempotency_key": "plan-" + runID, "title": "Parallel plan", "revision": 1,
		"items": []any{map[string]any{
			"client_ref": "shared", "key": "TH-PARALLEL-" + runID[:8], "title": "Shared work", "kind": "research",
			"priority": "medium", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent",
		}},
	})["result"].(map[string]any)
	planID := plan["plan"].(map[string]any)["id"]
	call("review_plan", map[string]any{"plan_id": planID, "actor_id": "human:reviewer", "idempotency_key": "review-" + runID, "decision": "approved", "reason": "Approved.", "expected_version": 1})
	call("transition_objective", map[string]any{"objective_id": objective["id"], "actor_id": "human:reviewer", "idempotency_key": "planning-" + runID, "target_phase": "planning", "reason": "Plan.", "expected_version": 1})
	call("transition_objective", map[string]any{"objective_id": objective["id"], "actor_id": "human:reviewer", "idempotency_key": "execution-" + runID, "target_phase": "execution", "reason": "Execute.", "expected_version": 2})

	version := func(id any) any {
		t.Helper()
		return call("get_item", map[string]any{"id": id})["result"].(map[string]any)["work_item"].(map[string]any)["version"]
	}

	items := call("list_items", map[string]any{"objective_id": objective["id"]})["result"].(map[string]any)["items"].([]any)
	item := items[0].(map[string]any)["work_item"].(map[string]any)
	call("transition_item", map[string]any{
		"id": item["id"], "actor_id": nodeA, "target_status": "ready", "expected_version": version(item["id"]),
		"idempotency_key": "ready-" + runID,
	})

	// Node A wins the claim.
	readyVersion := version(item["id"])
	claimA := call("claim_item", map[string]any{
		"id": item["id"], "actor_id": nodeA, "expected_version": readyVersion, "idempotency_key": "claim-" + runID,
		"lease_seconds": 300,
	})
	if claimA["error"] != nil {
		t.Fatalf("node A claim = %#v", claimA)
	}
	claimedVersion := claimA["result"].(map[string]any)["work_item"].(map[string]any)["version"]

	// Node B, a distinct actor identity for the same run, loses the race and must be told
	// enough to reread rather than guess.
	claimB := call("claim_item", map[string]any{
		"id": item["id"], "actor_id": nodeB, "expected_version": claimedVersion, "idempotency_key": "claim-" + runID,
		"lease_seconds": 300,
	})
	errorPayload, _ := claimB["error"].(map[string]any)
	if errorPayload["code"] != "claim_conflict" {
		t.Fatalf("node B claim = %#v, want claim_conflict", claimB)
	}
	current, ok := errorPayload["current"].(map[string]any)
	if !ok || current["id"] != item["id"] {
		t.Fatalf("claim_conflict current = %#v, want enough state to reread %s", errorPayload["current"], item["id"])
	}

	// Node A's own identical retry (same actor, same idempotency key) replays rather than
	// re-executing or conflicting with itself.
	replay := call("claim_item", map[string]any{
		"id": item["id"], "actor_id": nodeA, "expected_version": readyVersion, "idempotency_key": "claim-" + runID,
		"lease_seconds": 300,
	})
	if !reflect.DeepEqual(replay, claimA) {
		t.Fatalf("node A replay = %#v, want identical to the original claim %#v", replay, claimA)
	}

	// Idempotency is scoped per (actor_id, key): node B reusing node A's exact key text is
	// evaluated as node B's own fresh request, not as a replay of node A's successful claim
	// (which would be a cross-actor isolation bug) — it independently hits its own
	// claim_conflict rather than returning node A's result.
	if reflect.DeepEqual(claimB, claimA) {
		t.Fatalf("node B's request with node A's key text returned node A's own claim result: %#v", claimB)
	}

	// Node A reusing its own key with a materially different request is rejected outright,
	// not silently accepted as a replay.
	mismatch := call("claim_item", map[string]any{
		"id": item["id"], "actor_id": nodeA, "expected_version": readyVersion, "idempotency_key": "claim-" + runID,
		"lease_seconds": 600,
	})
	mismatchError, _ := mismatch["error"].(map[string]any)
	if mismatchError["code"] != "idempotency_key_reused_with_different_request" {
		t.Fatalf("same-actor changed retry = %#v, want idempotency_key_reused_with_different_request", mismatch)
	}
}

// TestDaemonRemainsHealthyAfterAnAbruptClientDisconnect proves one client abandoning a
// request mid-flight (its context cancelled, no clean MCP session shutdown) never wedges
// the daemon for anyone else: a fresh session opened immediately afterward against the same
// Router/HTTP server still resolves workspace_id and completes a call normally.
func TestDaemonRemainsHealthyAfterAnAbruptClientDisconnect(t *testing.T) {
	fakeRegistry := newTestRegistry()
	workspace, _, err := config.Initialize(t.TempDir(), "", "disconnect-workspace")
	if err != nil {
		t.Fatal(err)
	}
	fakeRegistry.register(t, workspace)
	workspaceRouter := router.New(fakeRegistry, router.NewProviderManager(router.SQLiteProvider{}), app.UUIDv7Generator{}, app.SystemClock{}, 0)
	t.Cleanup(func() { _ = workspaceRouter.Close() })

	httpServer := httptest.NewServer(Handler(workspaceRouter))
	t.Cleanup(httpServer.Close)

	open := func() *protocol.ClientSession {
		t.Helper()
		client := protocol.NewClient(&protocol.Implementation{Name: "disconnect-test", Version: "v1"}, nil)
		// DisableStandaloneSSE: this test's abandoned session must leave behind exactly one
		// thing — a request whose context was already cancelled — not also a lingering
		// server-initiated event stream, which is a distinct concern from a mid-request
		// disconnect.
		transport := &protocol.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp", DisableStandaloneSSE: true}
		session, err := client.Connect(context.Background(), transport, nil)
		if err != nil {
			t.Fatal(err)
		}
		return session
	}

	abandoned := open()
	abandonedCtx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: simulates a client that vanished before the response arrived
	_, _ = abandoned.CallTool(abandonedCtx, &protocol.CallToolParams{Name: "board_overview", Arguments: map[string]any{"workspace_id": workspace.Config.WorkspaceID}})
	// Deliberately no session.Close(): the abandoned session's underlying connection is left
	// exactly as an abruptly killed process would leave it.

	fresh := open()
	t.Cleanup(func() { _ = fresh.Close() })
	freshCtx, freshCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer freshCancel()
	result, err := fresh.CallTool(freshCtx, &protocol.CallToolParams{Name: "board_overview", Arguments: map[string]any{"workspace_id": workspace.Config.WorkspaceID}})
	if err != nil {
		t.Fatalf("a fresh session after an abandoned peer connection did not complete within 5s: %v", err)
	}
	if result.IsError {
		t.Fatalf("board_overview after an abandoned peer connection failed: %s", result.Content[0].(*protocol.TextContent).Text)
	}
}

func TestRuntimeValidationRejectsUnknownNestedArtifactField(t *testing.T) {
	ctx, session := newSession(t)
	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "create_output_revision", Arguments: map[string]any{
		"workspace_id": testWorkspaceID, "expected_output_id": "missing", "actor_id": "agent:writer", "idempotency_key": "revision", "artifacts": []any{
			map[string]any{"kind": "document", "uri": "file:///tmp/result.md", "unexpected": true},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("unknown nested artifact field was accepted")
	}
}

func TestTerminalExecutionRequiresNonEmptyEvidenceArtifactID(t *testing.T) {
	err := validateExecutionBranch(json.RawMessage(`{
"execution_id":"execution-1","actor_id":"agent:writer","expected_action_version":1,
"idempotency_key":"terminal","state":"failed","result":{"receipt":"none"},"evidence_artifact_id":""
}`), authority.ExecutionFailed)
	if err == nil || !strings.Contains(err.Error(), "non-empty evidence_artifact_id") {
		t.Fatalf("terminal execution validation error = %v", err)
	}
}

func TestRequestAttentionRejectsUnsupportedTargetKind(t *testing.T) {
	ctx, session := newSession(t)
	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "request_attention", Arguments: map[string]any{
		"workspace_id": testWorkspaceID, "target_kind": "unsupported", "target_id": "target-1", "actor_id": "human:reviewer", "idempotency_key": "attention", "attention_state": "needs_human_review",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("unsupported attention target kind was accepted")
	}
}

func TestRequestAttentionSupportsObjectiveScopedQuestion(t *testing.T) {
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
		if result.IsError {
			t.Fatalf("%s failed: %#v", name, payload)
		}
		return payload
	}
	call("register_actor", map[string]any{"actor_id": "agent:researcher", "kind": "agent", "display_name": "Researcher", "idempotency_key": "actor"})
	objective := call("create_objective", map[string]any{"actor_id": "agent:researcher", "idempotency_key": "objective", "key": "OBJ-ATTENTION", "title": "Attention target", "desired_outcome": "A durable attention request", "phase": "discovery"})["result"].(map[string]any)
	question := call("ask_question", map[string]any{"objective_id": objective["id"], "actor_id": "agent:researcher", "idempotency_key": "question", "question": "Which source is authoritative?"})["result"].(map[string]any)
	attention := call("request_attention", map[string]any{"target_kind": "question", "target_id": question["id"], "actor_id": "agent:researcher", "idempotency_key": "attention", "expected_version": question["version"], "attention_state": "needs_human_decision"})["result"].(map[string]any)
	if attention["target_kind"] != "question" || attention["target_id"] != question["id"] || attention["question"] == nil || attention["work_item"] != nil {
		t.Fatalf("objective-scoped attention = %#v", attention)
	}
}

func TestRuntimeValidationRejectsUnknownAuthorizationSubjectField(t *testing.T) {
	ctx, session := newSession(t)
	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "propose_external_action", Arguments: map[string]any{
		"workspace_id": testWorkspaceID, "work_item_id": "missing", "actor_id": "agent:writer", "expected_version": 1, "idempotency_key": "action",
		"title": "Action", "authorization_subject": map[string]any{"action_type": "tool.install", "target": map[string]any{"package": "tool", "unexpected": true}, "arguments": []any{}, "scope": map[string]any{}, "permissions": []any{}, "credential_requirements": []any{}, "constraints": map[string]any{}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("unknown authorization subject field was accepted")
	}
}

func TestRuntimeValidationRejectsUnknownFlattenedActionArgumentField(t *testing.T) {
	ctx, session := newSession(t)
	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "propose_plan", Arguments: map[string]any{
		"workspace_id": testWorkspaceID, "objective_id": "missing", "actor_id": "agent:writer", "idempotency_key": "plan", "title": "Plan", "items": []any{map[string]any{
			"client_ref": "one", "key": "TH-1", "title": "One", "kind": "research", "external_actions": []any{map[string]any{
				"title": "External work", "action_type": "tool.install", "target": map[string]any{}, "arguments": []any{map[string]any{"unexpected": true}}, "scope": map[string]any{}, "permissions": []any{}, "credential_requirements": []any{}, "constraints": map[string]any{},
			}},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("unknown flattened action argument field was accepted")
	}
}

// testWorkspaceID is the stable workspace_id newSession registers for every test in this
// package. Tests inject it into tool arguments rather than relying on any default, matching
// the accepted decision that every workspace-scoped tool call requires workspace_id
// explicitly.
const testWorkspaceID = "test-workspace"

func newSession(t *testing.T) (context.Context, *protocol.ClientSession) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	workspace, _, err := config.Initialize(root, "", testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	fakeRegistry := newTestRegistry()
	fakeRegistry.register(t, workspace)
	workspaceRouter := router.New(fakeRegistry, router.NewProviderManager(router.SQLiteProvider{}), app.UUIDv7Generator{}, app.SystemClock{}, 0)
	t.Cleanup(func() { _ = workspaceRouter.Close() })

	serverTransport, clientTransport := protocol.NewInMemoryTransports()
	server := NewServer(workspaceRouter)
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

// testRegistry is a minimal in-memory router.Registry so mcp package tests do not need a
// real SQLite-backed registry.Registry file.
type testRegistry struct {
	targets map[string]registry.WorkspaceTarget
}

func newTestRegistry() *testRegistry {
	return &testRegistry{targets: map[string]registry.WorkspaceTarget{}}
}

func (r *testRegistry) register(t *testing.T, workspace config.Workspace) {
	t.Helper()
	r.targets[workspace.Config.WorkspaceID] = registry.WorkspaceTarget{
		WorkspaceID:     workspace.Config.WorkspaceID,
		ProviderKind:    registry.ProviderSQLite,
		ProviderLocator: workspace.Config.WorkspaceID,
		CanonicalRoot:   workspace.Root,
		Generation:      1,
		LifecycleState:  registry.LifecycleActive,
	}
}

func (r *testRegistry) Lookup(_ context.Context, workspaceID string) (registry.WorkspaceTarget, error) {
	target, ok := r.targets[workspaceID]
	if !ok {
		return registry.WorkspaceTarget{}, registry.ErrWorkspaceNotFound
	}
	return target, nil
}

func TestOmittedMutationsReplayAndRejectChangedRequests(t *testing.T) {
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
		if result.IsError {
			t.Fatalf("%s failed: %#v", name, payload)
		}
		return payload
	}
	copyArguments := func(arguments map[string]any) map[string]any {
		result := make(map[string]any, len(arguments))
		for key, value := range arguments {
			result[key] = value
		}
		return result
	}
	type replayCase struct {
		name      string
		arguments map[string]any
		changed   func(map[string]any)
	}
	run := func(testCase replayCase) map[string]any {
		t.Helper()
		first := call(testCase.name, testCase.arguments)
		replay := call(testCase.name, testCase.arguments)
		if !reflect.DeepEqual(replay, first) {
			t.Fatalf("%s replay = %#v, want %#v", testCase.name, replay, first)
		}
		changed := copyArguments(testCase.arguments)
		testCase.changed(changed)
		result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: testCase.name, Arguments: changed})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatalf("%s changed request was accepted", testCase.name)
		}
		var mismatch map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].(*protocol.TextContent).Text), &mismatch); err != nil {
			t.Fatal(err)
		}
		if got := mismatch["error"].(map[string]any)["code"]; got != "idempotency_key_reused_with_different_request" {
			t.Fatalf("%s mismatch code = %v: %#v", testCase.name, got, mismatch)
		}
		return first
	}
	version := func(id any) any {
		t.Helper()
		return call("get_item", map[string]any{"id": id})["result"].(map[string]any)["work_item"].(map[string]any)["version"]
	}

	call("register_actor", map[string]any{"actor_id": "agent:writer", "kind": "agent", "display_name": "Writer", "idempotency_key": "matrix-writer"})
	call("register_actor", map[string]any{"actor_id": "human:reviewer", "kind": "human", "display_name": "Reviewer", "idempotency_key": "matrix-reviewer"})
	objective := call("create_objective", map[string]any{"actor_id": "agent:writer", "idempotency_key": "matrix-objective", "key": "MATRIX-1", "title": "Replay matrix", "desired_outcome": "Exercise mutation retries", "phase": "discovery"})["result"].(map[string]any)
	plan := call("propose_plan", map[string]any{"objective_id": objective["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-plan", "title": "Matrix plan", "revision": 1, "items": []any{
		map[string]any{"client_ref": "lease", "key": "MATRIX-LEASE", "title": "Lease item", "kind": "research", "priority": "medium", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent"},
		map[string]any{"client_ref": "dependent", "key": "MATRIX-DEPENDENT", "title": "Dependent item", "kind": "research", "priority": "medium", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent"},
	}})["result"].(map[string]any)["plan"].(map[string]any)
	call("review_plan", map[string]any{"plan_id": plan["id"], "actor_id": "human:reviewer", "idempotency_key": "matrix-review", "decision": "approved", "reason": "Approved", "expected_version": 1})
	call("transition_objective", map[string]any{"objective_id": objective["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-planning", "target_phase": "planning", "reason": "Planned", "expected_version": 1})
	call("transition_objective", map[string]any{"objective_id": objective["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-execution", "target_phase": "execution", "reason": "Executing", "expected_version": 2})
	items := call("list_items", map[string]any{"objective_id": objective["id"]})["result"].(map[string]any)["items"].([]any)
	lease := items[0].(map[string]any)["work_item"].(map[string]any)
	dependent := items[1].(map[string]any)["work_item"].(map[string]any)

	created := run(replayCase{"create_item", map[string]any{"actor_id": "agent:writer", "idempotency_key": "matrix-create-item", "key": "MATRIX-DIRECT", "objective_id": objective["id"], "title": "Direct item", "kind": "research", "priority": "low", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent"}, func(arguments map[string]any) { arguments["title"] = "Changed direct item" }})["result"].(map[string]any)
	run(replayCase{"patch_item", map[string]any{"id": created["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-patch-item", "expected_version": created["version"], "title": "Patched direct item"}, func(arguments map[string]any) { arguments["title"] = "Changed patch" }})
	question := run(replayCase{"ask_question", map[string]any{"objective_id": objective["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-question", "question": "Which review path applies?"}, func(arguments map[string]any) { arguments["question"] = "Changed question" }})["result"].(map[string]any)
	attention := run(replayCase{"request_attention", map[string]any{"target_kind": "question", "target_id": question["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-attention", "expected_version": question["version"], "attention_state": "needs_human_decision"}, func(arguments map[string]any) { arguments["attention_state"] = "needs_human_review" }})["result"].(map[string]any)
	run(replayCase{"answer_question", map[string]any{"question_id": question["id"], "actor_id": "human:reviewer", "idempotency_key": "matrix-answer", "expected_version": attention["question"].(map[string]any)["version"], "answer": "Use the standard review path."}, func(arguments map[string]any) { arguments["answer"] = "Changed answer" }})
	contextRecord := run(replayCase{"record_context", map[string]any{"objective_id": objective["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-context", "kind": "requirement", "title": "Retry must be stable", "status": "proposed", "body": "Responses include the original cursor."}, func(arguments map[string]any) { arguments["title"] = "Changed context" }})["result"].(map[string]any)
	run(replayCase{"transition_context", map[string]any{"context_record_id": contextRecord["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-context-transition", "target_status": "accepted", "expected_version": contextRecord["version"]}, func(arguments map[string]any) { arguments["target_status"] = "waived" }})
	run(replayCase{"record_decision", map[string]any{"objective_id": objective["id"], "actor_id": "human:reviewer", "idempotency_key": "matrix-decision", "title": "Use durable records", "decision": "Store retries transactionally", "rationale": "Cursor stability matters"}, func(arguments map[string]any) { arguments["decision"] = "Changed decision" }})

	approval := run(replayCase{"request_approval", map[string]any{"target_kind": "work_item", "work_item_id": created["id"], "actor_id": "human:reviewer", "idempotency_key": "matrix-request-approval", "request": "Approve direct item", "expected_version": version(created["id"])}, func(arguments map[string]any) { arguments["request"] = "Changed approval request" }})["result"].(map[string]any)
	run(replayCase{"resolve_approval", map[string]any{"target_kind": "work_item", "approval_id": approval["id"], "actor_id": "human:reviewer", "idempotency_key": "matrix-resolve-approval", "decision": "approved", "rationale": "Looks good", "expected_version": approval["version"]}, func(arguments map[string]any) { arguments["rationale"] = "Changed rationale" }})

	blocker := run(replayCase{"block_item", map[string]any{"work_item_id": created["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-block", "expected_version": version(created["id"]), "reason": "Awaiting source"}, func(arguments map[string]any) { arguments["reason"] = "Changed blocker" }})["result"].(map[string]any)
	run(replayCase{"unblock_item", map[string]any{"blocker_id": blocker["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-unblock", "expected_version": version(created["id"]), "resolution": "Source received"}, func(arguments map[string]any) { arguments["resolution"] = "Changed resolution" }})

	linked := run(replayCase{"link_dependency", map[string]any{"work_item_id": dependent["id"], "depends_on_work_item_id": lease["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-link", "expected_version": version(dependent["id"]), "kind": "hard", "note": "Wait for lease item"}, func(arguments map[string]any) { arguments["note"] = "Changed dependency" }})
	_ = linked
	run(replayCase{"unlink_dependency", map[string]any{"work_item_id": dependent["id"], "depends_on_work_item_id": lease["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-unlink", "expected_version": version(dependent["id"]), "kind": "hard"}, func(arguments map[string]any) { arguments["kind"] = "soft" }})

	profile := run(replayCase{"propose_output_profile", map[string]any{"actor_id": "agent:writer", "idempotency_key": "matrix-profile", "name": "matrix_profile", "version": 1, "description": "Retry matrix", "structure": map[string]any{"required": []any{"summary"}}, "semantics": map[string]any{"purpose": "test"}, "validation": map[string]any{"required": []any{}}}, func(arguments map[string]any) { arguments["description"] = "Changed profile" }})["result"].(map[string]any)
	run(replayCase{"review_output_profile", map[string]any{"profile_id": profile["id"], "actor_id": "human:reviewer", "idempotency_key": "matrix-profile-review", "expected_version": profile["state_version"], "decision": "active", "reason": "Approved profile"}, func(arguments map[string]any) { arguments["reason"] = "Changed profile review" }})

	call("transition_item", map[string]any{"id": lease["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-ready", "target_status": "ready", "expected_version": version(lease["id"])})
	claim := call("claim_item", map[string]any{"id": lease["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-claim", "expected_version": version(lease["id"]), "lease_seconds": 300, "transition_to_in_progress": true})["result"].(map[string]any)
	claimItem := claim["work_item"].(map[string]any)
	claimDetail := claim["claim"].(map[string]any)
	run(replayCase{"renew_claim", map[string]any{"work_item_id": lease["id"], "claim_id": claimDetail["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-renew", "expected_version": claimItem["version"], "lease_seconds": 300}, func(arguments map[string]any) { arguments["lease_seconds"] = 301 }})
	run(replayCase{"release_item", map[string]any{"work_item_id": lease["id"], "claim_id": claimDetail["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-release", "expected_version": version(lease["id"]), "reason": "Done with lease", "return_to_ready": true}, func(arguments map[string]any) { arguments["reason"] = "Changed release" }})

	run(replayCase{"define_expected_output", map[string]any{"work_item_id": dependent["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-expected-output", "name": "Matrix document", "profile_name": "structured_document", "profile_version": 1, "expected_version": version(dependent["id"]), "required": true, "ordinal": 1, "contract": map[string]any{}}, func(arguments map[string]any) { arguments["name"] = "Changed output" }})
	run(replayCase{"add_output_requirement", map[string]any{"work_item_id": created["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-output-requirement", "expected_version": version(created["id"]), "required_profile_name": "structured_document", "version_constraint": "=1", "required": true, "note": "Need an accepted document"}, func(arguments map[string]any) { arguments["note"] = "Changed output requirement" }})

	actionSubject := map[string]any{"action_type": "archive.request", "target": map[string]any{"collection": "matrix"}, "arguments": []any{}, "scope": map[string]any{"project": "matrix"}, "permissions": []any{"network.read"}, "credential_requirements": []any{}, "constraints": map[string]any{}}
	action := call("propose_external_action", map[string]any{"work_item_id": dependent["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-action", "expected_version": version(dependent["id"]), "title": "Archive request", "authorization_subject": actionSubject})["result"].(map[string]any)["action"].(map[string]any)
	patchedAction := run(replayCase{"patch_external_action_metadata", map[string]any{"action_id": action["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-patch-action", "expected_action_version": action["version"], "metadata": map[string]any{"title": "Renamed archive request"}}, func(arguments map[string]any) {
		arguments["metadata"] = map[string]any{"title": "Changed action metadata"}
	}})["result"].(map[string]any)
	run(replayCase{"revise_external_action", map[string]any{"action_id": action["id"], "actor_id": "agent:writer", "idempotency_key": "matrix-revise-action", "expected_action_version": patchedAction["version"], "expected_work_item_version": version(dependent["id"]), "authorization_subject": map[string]any{"action_type": "archive.request", "target": map[string]any{"collection": "matrix-revised"}, "arguments": []any{}, "scope": map[string]any{"project": "matrix"}, "permissions": []any{"network.read"}, "credential_requirements": []any{}, "constraints": map[string]any{}}}, func(arguments map[string]any) { arguments["authorization_subject"] = actionSubject }})
}

// TestHTTPTwoClientNonCodeWorkflowSmoke exercises the full multi-tool coordination workflow
// over Streamable HTTP with two independent client sessions against one shared daemon
// process (here, one httptest server sharing one Router), proving concurrent sessions are
// routed independently rather than through connection-bound workspace state.
func TestHTTPTwoClientNonCodeWorkflowSmoke(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspace, _, err := config.Initialize(root, "", "smoke-workspace")
	if err != nil {
		t.Fatal(err)
	}
	fakeRegistry := newTestRegistry()
	fakeRegistry.register(t, workspace)
	workspaceRouter := router.New(fakeRegistry, router.NewProviderManager(router.SQLiteProvider{}), app.UUIDv7Generator{}, app.SystemClock{}, 0)
	t.Cleanup(func() { _ = workspaceRouter.Close() })

	httpServer := httptest.NewServer(Handler(workspaceRouter))
	t.Cleanup(httpServer.Close)

	open := func(name string) *protocol.ClientSession {
		t.Helper()
		client := protocol.NewClient(&protocol.Implementation{Name: name, Version: "v1"}, nil)
		session, err := client.Connect(ctx, &protocol.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	clientA, clientB := open("researcher"), open("reviewer")
	type mutationRecord struct {
		session   *protocol.ClientSession
		name      string
		arguments map[string]any
		response  map[string]any
	}
	var mutations []mutationRecord
	mutation := map[string]bool{"register_actor": true, "create_objective": true, "patch_objective": true, "propose_plan": true, "review_plan": true, "transition_objective": true, "transition_item": true, "claim_item": true, "append_progress": true, "create_output_revision": true, "record_validation": true, "attach_artifact": true, "propose_external_action": true, "request_action_approval": true, "resolve_action_approval": true, "record_external_action_execution": true}
	call := func(session *protocol.ClientSession, name string, arguments map[string]any) (map[string]any, bool) {
		t.Helper()
		if _, ok := arguments["workspace_id"]; !ok {
			arguments["workspace_id"] = workspace.Config.WorkspaceID
		}
		response, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(response.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
			t.Fatal(err)
		}
		if mutation[name] && !response.IsError {
			mutations = append(mutations, mutationRecord{session: session, name: name, arguments: arguments, response: payload})
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
	plan, failed := call(clientA, "propose_plan", map[string]any{"objective_id": objective["id"], "actor_id": "agent:researcher", "idempotency_key": "plan", "title": "Research local archives", "revision": 1, "items": []any{map[string]any{"client_ref": "dossier", "key": "RES-1", "title": "Produce archive dossier", "kind": "research", "priority": "high", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent", "expected_outputs": []any{map[string]any{"name": "Dossier", "profile": "structured_document", "profile_version": 1, "required": true, "ordinal": 1}}}, map[string]any{"client_ref": "consumer", "key": "RES-2", "title": "Use accepted archive dossier", "kind": "workflow_design", "priority": "medium", "estimated_scope": "small", "execution_policy": "autonomous_with_report", "required_actor_kind": "agent", "output_requirements": []any{map[string]any{"required_profile_name": "structured_document", "version_constraint": "=1", "required": true, "note": "Use an accepted dossier."}}}}})
	if failed {
		t.Fatalf("propose plan failed: %#v", plan)
	}
	planResult := plan["result"].(map[string]any)
	planDetail := planResult["plan"].(map[string]any)
	if _, failed := call(clientB, "review_plan", map[string]any{"plan_id": planDetail["id"], "actor_id": "human:reviewer", "idempotency_key": "review", "decision": "approved", "reason": "Research scope approved", "expected_version": 1}); failed {
		t.Fatal("review plan failed")
	}
	if transitioned, failed := call(clientA, "transition_objective", map[string]any{"objective_id": objective["id"], "actor_id": "agent:researcher", "target_phase": "planning", "reason": "Discovery completed", "expected_version": 1, "idempotency_key": "planning"}); failed {
		t.Fatalf("objective planning transition failed: %#v", transitioned)
	}
	if transitioned, failed := call(clientA, "transition_objective", map[string]any{"objective_id": objective["id"], "actor_id": "agent:researcher", "target_phase": "execution", "reason": "Plan approved", "expected_version": 2, "idempotency_key": "execute"}); failed {
		t.Fatalf("objective transition failed: %#v", transitioned)
	}
	contextPayload, failed := call(clientA, "list_items", map[string]any{"objective_id": objective["id"]})
	if failed {
		t.Fatalf("list items failed: %#v", contextPayload)
	}
	items := contextPayload["result"].(map[string]any)["items"].([]any)
	itemContext := items[0].(map[string]any)
	item := itemContext["work_item"].(map[string]any)
	expectedID := itemContext["expected_outputs"].([]any)[0].(map[string]any)["expected_output"].(map[string]any)["id"]
	consumer := items[1].(map[string]any)["work_item"].(map[string]any)
	if requirements := items[1].(map[string]any)["output_requirements"].([]any); len(requirements) != 1 {
		t.Fatalf("consumer output requirements = %#v", requirements)
	}
	consumerReady, failed := call(clientA, "transition_item", map[string]any{"id": consumer["id"], "actor_id": "agent:researcher", "target_status": "ready", "expected_version": consumer["version"], "idempotency_key": "consumer-ready"})
	if failed {
		t.Fatalf("consumer ready failed: %#v", consumerReady)
	}
	ready, failed := call(clientA, "transition_item", map[string]any{"id": item["id"], "actor_id": "agent:researcher", "target_status": "ready", "expected_version": item["version"], "idempotency_key": "ready"})
	if failed {
		t.Fatalf("ready transition failed: %#v", ready)
	}
	readyItem := ready["result"].(map[string]any)
	claim, failed := call(clientA, "claim_item", map[string]any{"id": item["id"], "actor_id": "agent:researcher", "expected_version": readyItem["version"], "idempotency_key": "claim", "lease_seconds": 300, "transition_to_in_progress": true})
	if failed {
		t.Fatalf("claim failed: %#v", claim)
	}
	claimedItem := claim["result"].(map[string]any)["work_item"].(map[string]any)
	conflict, failed := call(clientB, "claim_item", map[string]any{"id": item["id"], "actor_id": "human:reviewer", "expected_version": claimedItem["version"], "idempotency_key": "claim-conflict", "lease_seconds": 300, "transition_to_in_progress": true})
	if !failed || conflict["error"].(map[string]any)["code"] != "claim_conflict" {
		t.Fatalf("claim conflict = %#v", conflict)
	}
	_, failed = call(clientA, "append_progress", map[string]any{"id": item["id"], "actor_id": "agent:researcher", "expected_version": claimedItem["version"], "idempotency_key": "progress", "summary": "Sources catalogued"})
	if failed {
		t.Fatal("progress failed")
	}
	readyBefore, failed := call(clientA, "list_ready_items", map[string]any{"actor_id": "agent:researcher"})
	if failed {
		t.Fatalf("list blocked readiness failed: %#v", readyBefore)
	}
	if readyContains(readyBefore["result"], consumer["id"]) {
		t.Fatalf("output requirement did not block consumer readiness: %#v", readyBefore)
	}
	revision, failed := call(clientA, "create_output_revision", map[string]any{"expected_output_id": expectedID, "actor_id": "agent:researcher", "idempotency_key": "revision", "artifacts": []any{map[string]any{"kind": "document", "uri": "file:///archive-dossier.md", "title": "Archive dossier", "metadata": map[string]any{}, "role": "primary"}}})
	if failed {
		t.Fatal("create revision failed")
	}
	if _, failed := call(clientB, "record_validation", map[string]any{"output_revision_id": revision["result"].(map[string]any)["id"], "actor_id": "human:reviewer", "idempotency_key": "validation", "criterion_ref": "structure", "validator_kind": "structure", "verdict": "passed", "details": map[string]any{}}); failed {
		t.Fatal("validate output failed")
	}
	if outputs, failed := call(clientB, "list_outputs", map[string]any{"profile_name": "structured_document", "version_constraint": "=1"}); failed || len(outputs["result"].([]any)) == 0 {
		t.Fatalf("accepted output discovery = %#v", outputs)
	}
	readyAfter, failed := call(clientA, "list_ready_items", map[string]any{"actor_id": "agent:researcher"})
	if failed || !readyContains(readyAfter["result"], consumer["id"]) {
		t.Fatalf("accepted output did not satisfy reuse requirement: %#v", readyAfter)
	}
	current, failed := call(clientA, "get_item", map[string]any{"id": item["id"]})
	if failed {
		t.Fatal("get reusable item failed")
	}
	evidence, failed := call(clientA, "attach_artifact", map[string]any{"work_item_id": item["id"], "actor_id": "agent:researcher", "expected_version": current["result"].(map[string]any)["work_item"].(map[string]any)["version"], "idempotency_key": "evidence", "kind": "receipt", "uri": "file:///archive-consent.txt"})
	if failed {
		t.Fatalf("attach evidence failed: %#v", evidence)
	}
	evidenceResult := evidence["result"].(map[string]any)
	actionSubject := map[string]any{"action_type": "archive.request", "target": map[string]any{"collection": "city-records"}, "arguments": []any{}, "scope": map[string]any{"project": "archives"}, "permissions": []any{"network.read"}, "credential_requirements": []any{}, "constraints": map[string]any{}}
	action, failed := call(clientA, "propose_external_action", map[string]any{"work_item_id": item["id"], "actor_id": "agent:researcher", "expected_version": evidenceResult["work_item"].(map[string]any)["version"], "idempotency_key": "archive-action", "title": "Request archive access", "authorization_subject": actionSubject})
	if failed {
		t.Fatalf("propose action failed: %#v", action)
	}
	actionResult := action["result"].(map[string]any)
	actionDetail := actionResult["action"].(map[string]any)
	revisionDetail := actionResult["revision"].(map[string]any)
	approval, failed := call(clientB, "request_action_approval", map[string]any{"action_id": actionDetail["id"], "actor_id": "human:reviewer", "approved_for_actor_id": "agent:researcher", "expected_action_version": actionDetail["version"], "authorization_subject_hash": revisionDetail["authorization_subject_hash"], "idempotency_key": "approve-request", "constraints": map[string]any{}, "request": "Approve archive access request"})
	if failed {
		t.Fatalf("request approval failed: %#v", approval)
	}
	resolved, failed := call(clientB, "resolve_action_approval", map[string]any{"approval_id": approval["result"].(map[string]any)["id"], "actor_id": "human:reviewer", "expected_action_version": actionDetail["version"], "idempotency_key": "approve", "decision": "approved", "rationale": "Access is scoped"})
	if failed {
		t.Fatalf("resolve approval failed: %#v", resolved)
	}
	resolvedResult := resolved["result"].(map[string]any)
	started, failed := call(clientA, "record_external_action_execution", map[string]any{"action_id": actionDetail["id"], "actor_id": "agent:researcher", "expected_action_version": resolvedResult["action"].(map[string]any)["version"], "idempotency_key": "start", "subject_hash": revisionDetail["authorization_subject_hash"], "authority_grant_id": resolvedResult["grant"].(map[string]any)["id"], "state": "started"})
	if failed {
		t.Fatalf("record execution start failed: %#v", started)
	}
	startedResult := started["result"].(map[string]any)
	if completed, failed := call(clientA, "record_external_action_execution", map[string]any{"execution_id": startedResult["execution"].(map[string]any)["id"], "actor_id": "agent:researcher", "expected_action_version": startedResult["action"].(map[string]any)["version"], "idempotency_key": "complete", "state": "succeeded", "result": map[string]any{"receipt": "recorded"}, "evidence_artifact_id": evidenceResult["artifact"].(map[string]any)["id"]}); failed {
		t.Fatalf("record execution result failed: %#v", completed)
	}
	deltas, failed := call(clientB, "get_changes", map[string]any{"since": cursor})
	if failed || len(deltas["result"].(map[string]any)["changes"].([]any)) == 0 {
		t.Fatalf("cursor deltas = %#v", deltas)
	}
	for _, recorded := range mutations {
		replayed, failed := call(recorded.session, recorded.name, recorded.arguments)
		if failed || !reflect.DeepEqual(replayed, recorded.response) {
			t.Fatalf("%s replay = %#v, want %#v", recorded.name, replayed, recorded.response)
		}
		changed := make(map[string]any, len(recorded.arguments)+1)
		for key, value := range recorded.arguments {
			changed[key] = value
		}
		switch recorded.name {
		case "claim_item":
			changed["lease_seconds"] = 301
		case "record_validation":
			changed["criterion_ref"] = "changed"
		case "record_external_action_execution":
			if changed["state"] == "started" {
				changed["subject_hash"] = "changed"
			} else {
				changed["result"] = map[string]any{"receipt": "changed"}
			}
		case "create_output_revision":
			changed["content_digest"] = "sha256:changed"
		case "transition_item", "transition_objective", "review_plan":
			changed["reason"] = "changed retry"
		case "resolve_action_approval":
			changed["rationale"] = "changed retry"
		case "append_progress":
			changed["summary"] = "changed retry"
		case "request_action_approval":
			changed["request"] = "changed retry"
		case "attach_artifact", "propose_external_action", "propose_plan", "create_objective", "patch_objective":
			changed["title"] = "changed retry"
		case "register_actor":
			changed["display_name"] = "changed retry"
		}
		mismatch, failed := call(recorded.session, recorded.name, changed)
		if !failed || mismatch["error"].(map[string]any)["code"] != "idempotency_key_reused_with_different_request" {
			t.Fatalf("%s changed retry = %#v", recorded.name, mismatch)
		}
	}
}

func readyContains(raw any, workItemID any) bool {
	items, _ := raw.([]any)
	for _, candidate := range items {
		item, _ := candidate.(map[string]any)["work_item"].(map[string]any)
		if item["id"] == workItemID {
			return true
		}
	}
	return false
}
