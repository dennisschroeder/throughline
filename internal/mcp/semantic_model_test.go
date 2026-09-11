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

// TestSemanticModelIdentifiesTheNewContextKindLifecycles is REP-06's second
// criterion, the "semantic model identifies it" half: non_goal and affected
// must appear in get_semantic_model's lifecycles section, on the same
// proposed -> accepted -> waived shape requirement, constraint and risk
// already share, not silently absent from the one place a session is meant
// to discover it.
func TestSemanticModelIdentifiesTheNewContextKindLifecycles(t *testing.T) {
	ctx, session := newSession(t)
	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: "get_semantic_model", Arguments: map[string]any{"section": "lifecycles"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("lifecycles section failed: %s", result.Content[0].(*protocol.TextContent).Text)
	}
	var payload struct {
		Result struct {
			Data []struct {
				ID          string     `json:"id"`
				Entity      string     `json:"entity"`
				Kinds       []string   `json:"kinds"`
				States      []string   `json:"states"`
				Transitions [][]string `json:"transitions"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].(*protocol.TextContent).Text), &payload); err != nil {
		t.Fatal(err)
	}
	var proposalLifecycle *struct {
		ID          string     `json:"id"`
		Entity      string     `json:"entity"`
		Kinds       []string   `json:"kinds"`
		States      []string   `json:"states"`
		Transitions [][]string `json:"transitions"`
	}
	// len(Kinds) > 0 guards against contains' vacuous-truth-on-empty behavior
	// (by design elsewhere, for "no requirement means anything satisfies
	// it") matching every kindless lifecycle here instead of the one that
	// actually names non_goal; matched is tracked separately so more than
	// one match is a failure too, not a silent last-one-wins overwrite.
	matched := 0
	for index := range payload.Result.Data {
		entry := &payload.Result.Data[index]
		if entry.Entity == "context_record" && len(entry.Kinds) > 0 && contains(entry.Kinds, "non_goal") {
			proposalLifecycle = entry
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("%d context_record lifecycles name non_goal, want exactly one: %#v", matched, payload.Result.Data)
	}
	if !contains(proposalLifecycle.Kinds, "affected") {
		t.Fatalf("non_goal's lifecycle kinds = %v, want affected on the same shape", proposalLifecycle.Kinds)
	}
	if !contains(proposalLifecycle.Kinds, "requirement") {
		t.Fatalf("non_goal's lifecycle kinds = %v, want requirement on the same shape it shares", proposalLifecycle.Kinds)
	}
	for _, state := range []string{"proposed", "accepted", "waived"} {
		if !contains(proposalLifecycle.States, state) {
			t.Fatalf("lifecycle states = %v, missing %q", proposalLifecycle.States, state)
		}
	}
}
