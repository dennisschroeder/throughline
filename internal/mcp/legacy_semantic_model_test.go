package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/throughline/internal/app"
	"github.com/dennisschroeder/throughline/internal/config"
	"github.com/dennisschroeder/throughline/internal/router"
)

func TestLegacyInitializationIncludesSemanticInstructions(t *testing.T) {
	ctx := context.Background()
	workspace, _, err := config.Initialize(t.TempDir(), "", "legacy-init-workspace")
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
	connection, err := clientTransport.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	id, err := jsonrpc.MakeID(float64(1))
	if err != nil {
		t.Fatal(err)
	}
	request := &jsonrpc.Request{
		ID:     id,
		Method: "initialize",
		Params: json.RawMessage(`{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy-test","version":"v1"}}`),
	}
	if err := connection.Write(ctx, request); err != nil {
		t.Fatal(err)
	}
	message, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	response, ok := message.(*jsonrpc.Response)
	if !ok || response.Error != nil {
		t.Fatalf("initialize response = %#v", message)
	}
	var result struct {
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Instructions, "get_semantic_model") {
		t.Fatalf("legacy instructions omit semantic tool: %s", result.Instructions)
	}
}
