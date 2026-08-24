package main

import (
	"encoding/json"
	"testing"
)

func TestCanonicalModelMapsEveryRequiredSourceArea(t *testing.T) {
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
	paths := map[string]bool{}
	for _, mapping := range generated.SourceMapping {
		paths[mapping.Path] = true
	}
	for _, path := range []string{
		"docs/implementation-handoff.md",
		"internal/mcp/server.go",
		"internal/domain/work/planning.go",
		"internal/domain/output/revisions.go",
		"internal/domain/authority/actions.go",
		"internal/sqlite/migrations/0009_idempotency_without_actor_foreign_key.sql",
	} {
		if !paths[path] {
			t.Fatalf("canonical model does not map %s", path)
		}
	}
}
