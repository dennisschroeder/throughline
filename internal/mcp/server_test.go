package mcp

import (
	"context"
	"encoding/json"
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
