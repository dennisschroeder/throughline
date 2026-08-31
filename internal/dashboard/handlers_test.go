package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/config"
	throughlinemcp "github.com/dennisschroeder/throughline/internal/mcp"
	"github.com/dennisschroeder/throughline/internal/registry"
	"github.com/dennisschroeder/throughline/internal/router"
)

const testWorkspaceID = "dashboard-test-workspace"

// testHarness wires a real SQLite-backed workspace, a WorkspaceRouter, an MCP session
// through the same HandlerWithHub wiring cli.go uses in production, and the dashboard's own
// handlers pointed at the same Router — so an MCP tool call in this test exercises the exact
// write path a real agent client would, and the dashboard's GET routes read the same state a
// real browser session would see.
type testHarness struct {
	t       *testing.T
	ctx     context.Context
	mcp     *protocol.ClientSession
	hub     *Hub
	handler *Handlers
	server  *httptest.Server
	client  *http.Client
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	workspace, _, err := config.Initialize(root, "", testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	reg := &fakeRegistry{targets: map[string]registry.WorkspaceTarget{
		testWorkspaceID: {
			WorkspaceID:     testWorkspaceID,
			ProviderKind:    registry.ProviderSQLite,
			ProviderLocator: testWorkspaceID,
			CanonicalRoot:   workspace.Root,
			Generation:      1,
			LifecycleState:  registry.LifecycleActive,
		},
	}}
	workspaceRouter := router.New(reg, router.NewProviderManager(router.SQLiteProvider{}), app.UUIDv7Generator{}, app.SystemClock{}, 0)
	t.Cleanup(func() { _ = workspaceRouter.Close() })

	hub := NewHub()
	mcpServer := throughlinemcp.NewServerWithHub(workspaceRouter, hub)
	serverTransport, clientTransport := protocol.NewInMemoryTransports()
	if _, err := mcpServer.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := protocol.NewClient(&protocol.Implementation{Name: "dashboard-test", Version: "v1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	handlers := NewHandlers(Config{
		Router:       workspaceRouter,
		Hub:          hub,
		AllowedHosts: []string{"127.0.0.1"},
	})

	mux := http.NewServeMux()
	mux.Handle("/dashboard/token", handlers.MintLoginTokenHandler())
	mux.Handle("/dashboard/login", handlers.ExchangeLoginTokenHandler())
	mux.Handle("/dashboard/api/v1/objectives", handlers.ObjectivesHandler())
	mux.Handle("/dashboard/api/v1/loop", handlers.LoopHandler())
	mux.Handle("/dashboard/api/v1/changes", handlers.ChangesHandler())
	mux.Handle("/dashboard/api/v1/gate", handlers.GateDetailHandler())
	mux.Handle("/dashboard/api/v1/item", handlers.ItemDetailHandler())
	mux.Handle("/dashboard/", handlers.StaticHandler())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	jar, _ := cookiejar.New(nil)
	return &testHarness{t: t, ctx: ctx, mcp: session, hub: hub, handler: handlers, server: server, client: &http.Client{Jar: jar}}
}

func (h *testHarness) call(name string, arguments map[string]any) map[string]any {
	h.t.Helper()
	if _, ok := arguments["workspace_id"]; !ok {
		arguments["workspace_id"] = testWorkspaceID
	}
	result, err := h.mcp.CallTool(h.ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		h.t.Fatalf("call %s: %v", name, err)
	}
	text := result.Content[0].(*protocol.TextContent).Text
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		h.t.Fatalf("call %s: decode response: %v", name, err)
	}
	if result.IsError {
		h.t.Fatalf("call %s failed: %s", name, text)
	}
	return payload
}

// login runs the full mint -> exchange flow and leaves h.client holding the session cookie.
func (h *testHarness) login(actorID string) {
	h.t.Helper()
	mintBody, _ := json.Marshal(mintRequest{WorkspaceID: testWorkspaceID, ActorID: actorID})
	mintResp, err := h.client.Post(h.server.URL+"/dashboard/token", "application/json", bytes.NewReader(mintBody))
	if err != nil {
		h.t.Fatalf("mint request: %v", err)
	}
	defer mintResp.Body.Close()
	if mintResp.StatusCode != http.StatusOK {
		h.t.Fatalf("mint status = %d", mintResp.StatusCode)
	}
	var minted mintResponse
	if err := json.NewDecoder(mintResp.Body).Decode(&minted); err != nil {
		h.t.Fatal(err)
	}
	loginResp, err := h.client.Get(h.server.URL + "/dashboard/login?token=" + minted.Token)
	if err != nil {
		h.t.Fatalf("login request: %v", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		h.t.Fatalf("login (after redirect) status = %d", loginResp.StatusCode)
	}
}

func (h *testHarness) get(path string) *http.Response {
	h.t.Helper()
	resp, err := h.client.Get(h.server.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

func (h *testHarness) getJSON(path string, out any) *http.Response {
	h.t.Helper()
	resp := h.get(path)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			h.t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp
}

type fakeRegistry struct {
	targets map[string]registry.WorkspaceTarget
}

func (r *fakeRegistry) Lookup(_ context.Context, workspaceID string) (registry.WorkspaceTarget, error) {
	target, ok := r.targets[workspaceID]
	if !ok {
		return registry.WorkspaceTarget{}, registry.ErrWorkspaceNotFound
	}
	return target, nil
}

func TestMintTokenRejectsUnroutableWorkspace(t *testing.T) {
	h := newTestHarness(t)
	body, _ := json.Marshal(mintRequest{WorkspaceID: "does-not-exist", ActorID: "agent:x"})
	resp, err := h.client.Post(h.server.URL+"/dashboard/token", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestLoginExchangeRejectsUnknownToken(t *testing.T) {
	h := newTestHarness(t)
	resp, err := h.client.Get(h.server.URL + "/dashboard/login?token=not-a-real-token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestLoginExchangeRejectsSecondUse(t *testing.T) {
	h := newTestHarness(t)
	body, _ := json.Marshal(mintRequest{WorkspaceID: testWorkspaceID, ActorID: "agent:x"})
	mintResp, err := h.client.Post(h.server.URL+"/dashboard/token", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var minted mintResponse
	json.NewDecoder(mintResp.Body).Decode(&minted)
	mintResp.Body.Close()

	first, err := h.client.Get(h.server.URL + "/dashboard/login?token=" + minted.Token)
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first exchange status = %d", first.StatusCode)
	}

	second, err := h.client.Get(h.server.URL + "/dashboard/login?token=" + minted.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	if second.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want 401", second.StatusCode)
	}
}

func TestLoopWithoutSessionCookieIsUnauthorized(t *testing.T) {
	h := newTestHarness(t)
	resp := h.get("/dashboard/api/v1/loop")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// setupObjectiveWithOpenPlan creates a real objective through the actual write path (MCP
// tool calls, exactly like a real agent) with a plan proposed but not yet reviewed — a live
// "plan" gate — plus a registered dormant actor, so the resulting /api/v1/loop payload
// exercises the queue, the switcher and the glance rail against real data rather than fixtures.
func (h *testHarness) setupObjectiveWithOpenPlan() (objectiveID, planID string) {
	h.t.Helper()
	const actorID = "agent:dashboard-worker"
	h.call("register_actor", map[string]any{"actor_id": actorID, "kind": "agent", "display_name": "Dashboard Worker", "idempotency_key": "register"})
	objective := h.call("create_objective", map[string]any{
		"actor_id": actorID, "idempotency_key": "objective", "key": "OBJ-1", "title": "Objective One",
		"desired_outcome": "Outcome", "phase": "planning",
	})["result"].(map[string]any)
	plan := h.call("propose_plan", map[string]any{
		"objective_id": objective["id"], "actor_id": actorID, "idempotency_key": "plan", "title": "Plan One", "revision": 1,
		"items": []any{map[string]any{
			"client_ref": "item-1", "key": "ITEM-1", "title": "Item one", "kind": "research",
			"priority": "medium", "estimated_scope": "small", "execution_policy": "autonomous_with_report",
			"required_actor_kind": "agent",
		}},
	})["result"].(map[string]any)
	return objective["id"].(string), plan["plan"].(map[string]any)["id"].(string)
}

func TestLoopSnapshotSurfacesOpenPlanGate(t *testing.T) {
	h := newTestHarness(t)
	objectiveID, planID := h.setupObjectiveWithOpenPlan()
	h.login("human:reviewer")

	var snapshot LoopSnapshot
	resp := h.getJSON("/dashboard/api/v1/loop?objective_id="+objectiveID, &snapshot)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("loop status = %d", resp.StatusCode)
	}
	if snapshot.Objective.ID != objectiveID {
		t.Fatalf("objective id = %q, want %q", snapshot.Objective.ID, objectiveID)
	}
	if snapshot.Counts.NeedsYou < 1 {
		t.Fatalf("counts.needs_you = %d, want >= 1", snapshot.Counts.NeedsYou)
	}
	var gate *Gate
	for i := range snapshot.Gates {
		if snapshot.Gates[i].ID == planID {
			gate = &snapshot.Gates[i]
		}
	}
	if gate == nil {
		t.Fatalf("plan gate %s not present in %+v", planID, snapshot.Gates)
	}
	if gate.Kind != "plan" || gate.TargetID != planID {
		t.Fatalf("gate = %+v", gate)
	}
	if len(gate.AllowedDecisions) == 0 {
		t.Fatalf("gate has no allowed_decisions: %+v", gate)
	}

	// Auto-resolve: omitting objective_id must land on the same objective (it's the only one).
	var auto LoopSnapshot
	autoResp := h.getJSON("/dashboard/api/v1/loop", &auto)
	if autoResp.StatusCode != http.StatusOK {
		t.Fatalf("auto-resolve loop status = %d", autoResp.StatusCode)
	}
	if auto.Objective.ID != objectiveID {
		t.Fatalf("auto-resolved objective = %q, want %q", auto.Objective.ID, objectiveID)
	}
}

func TestObjectivesHandlerListsGateCounts(t *testing.T) {
	h := newTestHarness(t)
	objectiveID, _ := h.setupObjectiveWithOpenPlan()
	h.login("human:reviewer")

	var resp ObjectivesResponse
	httpResp := h.getJSON("/dashboard/api/v1/objectives", &resp)
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("objectives status = %d", httpResp.StatusCode)
	}
	if len(resp.Objectives) != 1 {
		t.Fatalf("objectives = %+v, want 1", resp.Objectives)
	}
	if resp.Objectives[0].ID != objectiveID || resp.Objectives[0].GateCount < 1 {
		t.Fatalf("objective summary = %+v", resp.Objectives[0])
	}
}

func TestGateDetailReturnsPlanEvidence(t *testing.T) {
	h := newTestHarness(t)
	objectiveID, planID := h.setupObjectiveWithOpenPlan()
	h.login("human:reviewer")

	var detail GateDetail
	resp := h.getJSON("/dashboard/api/v1/gate?kind=plan&id="+planID+"&objective_id="+objectiveID, &detail)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gate detail status = %d", resp.StatusCode)
	}
	if detail.Ask == "" {
		t.Fatal("expected non-empty ask text")
	}
	if detail.Evidence.Kind != "diff" {
		t.Fatalf("evidence kind = %q, want diff", detail.Evidence.Kind)
	}
	if len(detail.Facts) == 0 {
		t.Fatal("expected non-empty facts")
	}
}

func TestChangesHandlerReportsCursorMovement(t *testing.T) {
	h := newTestHarness(t)
	_, _ = h.setupObjectiveWithOpenPlan()
	h.login("human:reviewer")

	var first ChangesResponse
	h.getJSON("/dashboard/api/v1/changes?since=0", &first)
	if !first.Changed {
		t.Fatalf("expected changed=true against since=0, got %+v", first)
	}

	var second ChangesResponse
	resp := h.getJSON("/dashboard/api/v1/changes?since="+strconv.FormatInt(first.Cursor, 10), &second)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("changes status = %d", resp.StatusCode)
	}
	if second.Changed {
		t.Fatalf("expected changed=false immediately after observing cursor %d, got %+v", first.Cursor, second)
	}
}

func TestItemDetailHandlerServesReadOnlyItem(t *testing.T) {
	h := newTestHarness(t)
	objectiveID, planID := h.setupObjectiveWithOpenPlan()
	h.call("review_plan", map[string]any{"plan_id": planID, "actor_id": "human:reviewer", "idempotency_key": "review", "decision": "approved", "reason": "Approved.", "expected_version": 1})
	h.call("transition_objective", map[string]any{"objective_id": objectiveID, "actor_id": "human:reviewer", "idempotency_key": "planning-to-execution", "target_phase": "execution", "reason": "Go.", "expected_version": 1})
	items := h.call("list_items", map[string]any{"objective_id": objectiveID})["result"].(map[string]any)["items"].([]any)
	itemID := items[0].(map[string]any)["work_item"].(map[string]any)["id"].(string)

	h.login("human:reviewer")
	var detail itemDetail
	resp := h.getJSON("/dashboard/api/v1/item?id="+itemID, &detail)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("item detail status = %d", resp.StatusCode)
	}
	if detail.WorkItem.ID != itemID {
		t.Fatalf("item id = %q, want %q", detail.WorkItem.ID, itemID)
	}
	if !detail.ReadOnly {
		t.Fatalf("expected read_only=true for an item with no open gate, got %+v", detail.WorkItem)
	}
}
