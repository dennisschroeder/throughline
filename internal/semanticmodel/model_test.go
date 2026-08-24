package semanticmodel

import (
	"bytes"
	"os"
	"testing"
)

func TestEmbeddedModelIsValidAndDeterministic(t *testing.T) {
	model, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Entities) < 20 || len(model.Relations) < 10 {
		t.Fatalf("model counts = %d entities, %d relations", len(model.Entities), len(model.Relations))
	}
	manifest, missing, err := model.Section("manifest", nil)
	if err != nil || len(missing) != 0 {
		t.Fatalf("manifest = %#v, missing = %#v, err = %v", manifest, missing, err)
	}
	if model.Manifest.ContentDigest != model.ContentDigest || model.Manifest.SourceDigest != model.SourceDigest {
		t.Fatal("manifest digests do not match model digests")
	}
	if _, err := os.Stat("model.generated.json"); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(generated, []byte{}) {
		t.Fatal("generated model is empty")
	}
}

func TestSectionFiltersAndReportsUnknownIDs(t *testing.T) {
	model, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	result, missing, err := model.Section("entities", []string{"work_item", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.([]any)) != 1 || len(missing) != 1 || missing[0] != "missing" {
		t.Fatalf("result = %#v, missing = %#v", result, missing)
	}
}
