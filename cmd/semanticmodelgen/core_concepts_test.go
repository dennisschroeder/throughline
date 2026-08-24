package main

import (
	"encoding/json"
	"testing"
)

func TestCanonicalModelIncludesAllCoreCoordinationConcepts(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderModel(root)
	if err != nil {
		t.Fatal(err)
	}
	var generated model
	if err := json.Unmarshal(rendered, &generated); err != nil {
		t.Fatal(err)
	}
	entities := map[string]bool{}
	for _, entity := range generated.Entities {
		entities[entity.ID] = true
	}
	for _, id := range []string{"blocker", "progress", "attention", "execution_policy"} {
		if !entities[id] {
			t.Fatalf("canonical model omits core concept %q", id)
		}
	}
	if generated.ModelVersion != "1.1.0" {
		t.Fatalf("semantic model version = %q, want 1.1.0", generated.ModelVersion)
	}
}
