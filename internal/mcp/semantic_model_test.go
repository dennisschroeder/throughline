package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dennisschroeder/throughline/internal/semanticmodel"
)

func TestSemanticModelInitializationAndReadContract(t *testing.T) {
	ctx, session := newSession(t)
	model, err := semanticmodel.Load()
	if err != nil {
		t.Fatal(err)
	}
	instructions := session.InitializeResult().Instructions
	for _, required := range []string{
		model.ModelVersion,
		model.ContentDigest,
		"WorkItem -> ExpectedOutput -> OutputRevision -> ValidationRecord",
		"ExternalAction revision -> AuthorizationSubject -> principal-bound AuthorityGrant",
		"Capability does not imply Authority",
		"Throughline never performs external effects",
		"get_semantic_model",
	} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("initialization instructions omit %q: %s", required, instructions)
		}
	}
	if len(instructions) > maxServerInstructionsBytes {
		t.Fatalf("initialization instructions are %d bytes", len(instructions))
	}

	for _, section := range semanticmodel.AvailableSections() {
		result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "get_semantic_model", Arguments: map[string]any{"section": section}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("section %q failed: %s", section, result.Content[0].(*protocol.TextContent).Text)
		}
	}

	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "get_semantic_model", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("default manifest failed: %s", result.Content[0].(*protocol.TextContent).Text)
	}
	var payload struct {
		Workspace struct {
			ChangeCursor string `json:"change_cursor"`
		} `json:"workspace"`
		Result struct {
			Section string `json:"section"`
			Data    struct {
				ModelVersion string `json:"model_version"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Result.Section != "manifest" || payload.Result.Data.ModelVersion != model.ModelVersion || payload.Workspace.ChangeCursor != "0" {
		t.Fatalf("default manifest payload = %#v", payload)
	}

	changes, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "get_changes", Arguments: map[string]any{"workspace_id": testWorkspaceID, "since": "0"}})
	if err != nil {
		t.Fatal(err)
	}
	if changes.IsError || strings.Contains(changes.Content[0].(*protocol.TextContent).Text, `"changes":[{`) {
		t.Fatalf("semantic read mutated activity: %s", changes.Content[0].(*protocol.TextContent).Text)
	}
}

func TestSemanticInstructionsRejectOversizeComposition(t *testing.T) {
	_, err := semanticInstructions(&semanticmodel.Model{
		Bootstrap:     strings.Repeat("x", maxServerInstructionsBytes),
		ModelVersion:  "1.0.0",
		ContentDigest: "digest",
	})
	if err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("oversize instruction error = %v", err)
	}
}
