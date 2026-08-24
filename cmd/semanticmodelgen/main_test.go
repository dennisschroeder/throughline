package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderModelIsDeterministicAndCanonical(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "docs/architecture.md", "# Architecture\n")
	writeFixture(t, root, "ontology/throughline.json", `{
	  "schema_version":"1.0",
	  "model_version":"1.0.0",
	  "bootstrap":"Use the model.",
	  "entities":[{"id":"zeta"},{"id":"alpha"}],
	  "relations":[],
	  "lifecycles":[],
	  "invariants":[],
	  "source_mappings":[{"id":"architecture","path":"docs/architecture.md","heading":"# Architecture"}]
	}`)

	first, err := renderModel(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderModel(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("rendered model is not byte-for-byte deterministic")
	}
	if bytes.Contains(first, []byte(root)) || bytes.Contains(first, []byte("created_at")) {
		t.Fatalf("generated model includes machine-specific data: %s", first)
	}

	var generated model
	if err := json.Unmarshal(first, &generated); err != nil {
		t.Fatal(err)
	}
	if generated.Entities[0].ID != "alpha" || generated.ContentDigest == "" || generated.SourceDigest == "" {
		t.Fatalf("unexpected generated model: %#v", generated)
	}

	writeFixture(t, root, "docs/architecture.md", "# Architecture\nChanged\n")
	changed, err := renderModel(root)
	if err != nil {
		t.Fatal(err)
	}
	var changedModel model
	if err := json.Unmarshal(changed, &changedModel); err != nil {
		t.Fatal(err)
	}
	if changedModel.SourceDigest == generated.SourceDigest {
		t.Fatal("source digest did not change after a mapped source changed")
	}
}

func TestRenderModelRejectsMissingSourceAnchor(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "docs/architecture.md", "# Architecture\n")
	writeFixture(t, root, "ontology/throughline.json", `{
	  "schema_version":"1.0", "model_version":"1.0.0", "bootstrap":"Use the model.",
	  "entities":[], "relations":[], "lifecycles":[], "invariants":[],
	  "source_mappings":[{"id":"architecture","path":"docs/architecture.md","heading":"# Missing"}]
	}`)

	_, err := renderModel(root)
	if err == nil || !strings.Contains(err.Error(), "anchor") {
		t.Fatalf("render error = %v, want missing anchor", err)
	}
}

func TestCanonicalJSONRejectsMalformedRecords(t *testing.T) {
	_, err := canonicalJSON(map[string]any{
		"entities":        []any{"not an object"},
		"relations":       []any{},
		"lifecycles":      []any{},
		"invariants":      []any{},
		"source_mappings": []any{},
	})
	if err == nil || !strings.Contains(err.Error(), "entries must be objects with IDs") {
		t.Fatalf("canonical error = %v", err)
	}
}

func writeFixture(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
