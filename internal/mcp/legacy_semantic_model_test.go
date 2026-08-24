package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/throughline/internal/app"
	throughlinesqlite "github.com/dennisschroeder/throughline/internal/sqlite"
)

func TestLegacyInitializationIncludesSemanticInstructions(t *testing.T) {
	ctx := context.Background()
	database, err := throughlinesqlite.Open(ctx, filepath.Join(t.TempDir(), "throughline.db"))
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
