package output

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExpectedOutputRequiresActiveExactProfile(t *testing.T) {
	profile := Profile{ID: "profile", Name: "research_dossier", Version: 2, LifecycleState: ProfileProposed}
	_, err := NewExpectedOutput("output", "item", "Dossier", profile, json.RawMessage(`{}`), "", true, 1)
	if err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("expected inactive-profile error, got %v", err)
	}
}

func TestExpectedOutputNarrowingRequiresExplicitValidation(t *testing.T) {
	profile := Profile{ID: "profile", Name: "research_dossier", Version: 1, LifecycleState: ProfileActive}
	if _, err := NewExpectedOutput("output", "item", "Dossier", profile, json.RawMessage(`{"minimum_sources":3}`), "", true, 1); err == nil {
		t.Fatal("expected a non-empty instance contract without validation rules to be rejected")
	}
}

func TestExpectedOutputRejectsProfileCriterionCollision(t *testing.T) {
	profile := Profile{
		ID: "profile", Name: "research_dossier", Version: 1, LifecycleState: ProfileActive,
		Validation: json.RawMessage(`{"required":[{"kind":"structure"}]}`),
	}
	_, err := NewExpectedOutput("output", "item", "Dossier", profile, json.RawMessage(`{
		"validation":{"required":[{"kind":"evaluation","criterion_ref":"structure"}]}
	}`), "", true, 1)
	if err == nil || !strings.Contains(err.Error(), "duplicates profile validation criterion") {
		t.Fatalf("expected criterion-collision error, got %v", err)
	}
}

func TestProfileProposalRequiresReviewBeforeActivation(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	profile, err := NewProfileProposal(Profile{
		ID:           "profile-v2",
		Name:         "research_dossier",
		Version:      2,
		Description:  "Adds an explicit limitations section.",
		Structure:    json.RawMessage(`{"required":["limitations"]}`),
		Semantics:    json.RawMessage(`{}`),
		Validation:   json.RawMessage(`{"required":[{"kind":"human_review"}]}`),
		SupersedesID: "profile-v1",
		ProposedBy:   "agent:designer",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if profile.LifecycleState != ProfileProposed {
		t.Fatalf("proposal state = %q", profile.LifecycleState)
	}
	active, err := ReviewProfile(profile, ProfileActive, "human:reviewer", "Contract is usable.", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if active.LifecycleState != ProfileActive || active.ResolvedBy != "human:reviewer" {
		t.Fatalf("unexpected active profile: %#v", active)
	}
	if _, err := ReviewProfile(active, ProfileRejected, "human:reviewer", "", now.Add(2*time.Hour)); err == nil {
		t.Fatal("expected active profile to be immutable")
	}
}
