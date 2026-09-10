package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/registry"
	"github.com/dennisschroeder/throughline/internal/router"
)

// newResolveWorkspaceSession builds its own server/session, like newSession, but registers
// two workspaces at controlled, actually-canonicalized roots — one nested inside the
// other — since resolve_workspace's whole behavior is which of several ancestors wins, and
// the default single-workspace newSession has nothing to disambiguate against. It sets the
// registry entries directly rather than through testRegistry.register, because that helper
// stores a workspace's uncanonicalized Root: on a host where the temp directory itself
// sits behind a symlink (macOS's /var -> /private/var), that would silently stop matching
// what registry.CanonicalizeRoot computes for the same real directory.
func newResolveWorkspaceSession(t *testing.T) (context.Context, *protocol.ClientSession, string, string) {
	t.Helper()
	ctx := context.Background()
	outer := t.TempDir()
	nested := filepath.Join(outer, "nested", "inner")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	outerCanonical, err := registry.CanonicalizeRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	nestedCanonical, err := registry.CanonicalizeRoot(nested)
	if err != nil {
		t.Fatal(err)
	}

	fakeRegistry := newTestRegistry()
	fakeRegistry.targets["ws-outer"] = registry.WorkspaceTarget{
		WorkspaceID: "ws-outer", ProviderKind: registry.ProviderSQLite, ProviderLocator: "ws-outer",
		CanonicalRoot: outerCanonical, Generation: 1, LifecycleState: registry.LifecycleActive,
	}
	fakeRegistry.targets["ws-nested"] = registry.WorkspaceTarget{
		WorkspaceID: "ws-nested", ProviderKind: registry.ProviderSQLite, ProviderLocator: "ws-nested",
		CanonicalRoot: nestedCanonical, Generation: 1, LifecycleState: registry.LifecycleActive,
	}
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
	return ctx, session, outer, nested
}

// TestResolveWorkspaceFindsTheNearestRegisteredAncestor is REP-05's first
// criterion at the actual MCP surface a client calls: given a path under the
// nested workspace, resolve_workspace must return that workspace's id, not
// the outer one that also contains the path.
func TestResolveWorkspaceFindsTheNearestRegisteredAncestor(t *testing.T) {
	ctx, session, _, nested := newResolveWorkspaceSession(t)
	subdir := filepath.Join(nested, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "resolve_workspace", Arguments: map[string]any{"path": subdir}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("resolve_workspace failed: %s", result.Content[0].(*protocol.TextContent).Text)
	}
	var payload struct {
		Result struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"result"`
		Workspace struct {
			ID string `json:"id"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Result.WorkspaceID != "ws-nested" {
		t.Fatalf("resolve_workspace result = %q, want the nearest ancestor ws-nested", payload.Result.WorkspaceID)
	}
	// A workspaceless tool reports no established workspace of its own in the envelope.
	if payload.Workspace.ID != "" {
		t.Fatalf("resolve_workspace envelope workspace.id = %q, want empty", payload.Workspace.ID)
	}
}

// TestResolveWorkspaceReportsNotFoundOutsideAnyRegisteredRoot covers the
// negative case: a path under neither registered workspace must fail with the
// stable workspace_not_found code, not silently pick the wrong workspace.
func TestResolveWorkspaceReportsNotFoundOutsideAnyRegisteredRoot(t *testing.T) {
	ctx, session, _, _ := newResolveWorkspaceSession(t)
	elsewhere := t.TempDir()

	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "resolve_workspace", Arguments: map[string]any{"path": elsewhere}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("a path outside every registered workspace resolved successfully")
	}
	var payload struct {
		Error struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "workspace_not_found" {
		t.Fatalf("error code = %q, want workspace_not_found", payload.Error.Code)
	}
	if payload.Error.Retryable {
		t.Fatal("workspace_not_found reported retryable, want false")
	}
}

// TestResolveWorkspaceIsAdvertisedReadOnlyWithNoWorkspaceIDRequirement checks
// the tool's own shape: it must not require workspace_id (the entire point is
// to learn one), and must be annotated read-only like get_semantic_model.
func TestResolveWorkspaceIsAdvertisedReadOnlyWithNoWorkspaceIDRequirement(t *testing.T) {
	ctx, session, _, _ := newResolveWorkspaceSession(t)
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found *protocol.Tool
	for index := range tools.Tools {
		if tools.Tools[index].Name == "resolve_workspace" {
			found = tools.Tools[index]
		}
	}
	if found == nil {
		t.Fatal("resolve_workspace not advertised")
	}
	if found.Annotations == nil || !found.Annotations.ReadOnlyHint {
		t.Fatalf("resolve_workspace annotations = %#v, want read-only", found.Annotations)
	}
	encoded, err := json.Marshal(found.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	for _, required := range schema.Required {
		if required == "workspace_id" {
			t.Fatal("resolve_workspace requires workspace_id, but resolving one is the point of calling it")
		}
	}
}
