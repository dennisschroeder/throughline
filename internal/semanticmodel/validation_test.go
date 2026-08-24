package semanticmodel

import (
	"strings"
	"testing"
)

func TestModelValidationRejectsInvalidReferencesAndSections(t *testing.T) {
	model, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	invalidRelation := *model
	invalidRelation.Relations = append([]Relation(nil), model.Relations...)
	invalidRelation.Relations[0].To = "missing"
	if err := invalidRelation.Validate(); err == nil || !strings.Contains(err.Error(), "invalid endpoint") {
		t.Fatalf("relation validation error = %v", err)
	}

	invalidLifecycle := *model
	invalidLifecycle.Lifecycles = append([]Lifecycle(nil), model.Lifecycles...)
	invalidLifecycle.Lifecycles[0].Transitions = append([][2]string(nil), model.Lifecycles[0].Transitions...)
	invalidLifecycle.Lifecycles[0].Transitions[0] = [2]string{"missing", "discovery"}
	if err := invalidLifecycle.Validate(); err == nil || !strings.Contains(err.Error(), "invalid transition") {
		t.Fatalf("lifecycle validation error = %v", err)
	}

	invalidManifest := *model
	invalidManifest.Manifest.AvailableSections = append([]string(nil), model.Manifest.AvailableSections...)
	invalidManifest.Manifest.AvailableSections = append(invalidManifest.Manifest.AvailableSections, "unknown")
	if err := invalidManifest.Validate(); err == nil || !strings.Contains(err.Error(), "available sections") {
		t.Fatalf("manifest validation error = %v", err)
	}
}

func TestEveryAdvertisedSectionCanBeRead(t *testing.T) {
	model, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range AvailableSections() {
		result, missing, err := model.Section(section, nil)
		if err != nil || result == nil || len(missing) != 0 {
			t.Fatalf("section %q: result=%#v missing=%#v err=%v", section, result, missing, err)
		}
	}
}
