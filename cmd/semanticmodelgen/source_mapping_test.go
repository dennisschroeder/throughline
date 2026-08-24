package main

import (
	"strings"
	"testing"
)

func TestRenderModelRejectsDirectorySourceMappings(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "docs/architecture.md", "# Architecture\n")
	writeFixture(t, root, "ontology/throughline.json", `{
  "schema_version":"1.0", "model_version":"1.0.0", "bootstrap":"Use the model.",
  "entities":[], "relations":[], "lifecycles":[], "invariants":[],
  "source_mappings":[{"id":"docs","path":"docs","heading":"# Architecture"}]
}`)

	_, err := renderModel(root)
	if err == nil || !strings.Contains(err.Error(), "must reference a file") {
		t.Fatalf("render error = %v", err)
	}
}
