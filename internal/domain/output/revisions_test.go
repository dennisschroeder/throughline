package output

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewArtifactValidatesExternalURI(t *testing.T) {
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	artifact, err := NewArtifact(Artifact{
		ID:         "artifact-1",
		WorkItemID: "item-1",
		Kind:       "document",
		URI:        "https://example.com/report.pdf",
		Metadata:   json.RawMessage(`{"format":"pdf"}`),
		AttachedBy: "agent:writer",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.CreatedAt != now || artifact.URI != "https://example.com/report.pdf" {
		t.Fatalf("unexpected artifact: %#v", artifact)
	}

	_, err = NewArtifact(Artifact{ID: "artifact-2", WorkItemID: "item-1", Kind: "document", URI: "not a URI", AttachedBy: "agent:writer"}, now)
	if err == nil || !strings.Contains(err.Error(), "URI") {
		t.Fatalf("expected URI error, got %v", err)
	}
}

func TestHumanReviewRequiresNamedVerifierAndRationale(t *testing.T) {
	now := time.Date(2026, 8, 21, 11, 0, 0, 0, time.UTC)
	revision := OutputRevision{ID: "revision-1"}

	_, err := NewValidationRecord("validation-1", revision, "research-quality/v1", ValidatorHumanReview, VerdictPassed, nil, "", "", json.RawMessage(`{"rationale":"Clear and complete."}`), now)
	if err == nil || !strings.Contains(err.Error(), "named verifier") {
		t.Fatalf("expected named-verifier error, got %v", err)
	}

	_, err = NewValidationRecord("validation-2", revision, "research-quality/v1", ValidatorHumanReview, VerdictPassed, nil, "human:reviewer", "", json.RawMessage(`{}`), now)
	if err == nil || !strings.Contains(err.Error(), "rationale") {
		t.Fatalf("expected rationale error, got %v", err)
	}
}

func TestAcceptOutputRevisionUsesLatestValidationPerCriterion(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	profile := activeProfile(json.RawMessage(`{
		"required": [
			{"kind":"structure"},
			{"kind":"human_review","rubric":"research-quality/v1"}
		]
	}`))
	expected, err := NewExpectedOutput("expected-1", "item-1", "Dossier", profile, nil, "", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := NewOutputRevision("revision-1", expected, profile, 1, []RevisionArtifact{{ArtifactID: "artifact-1"}}, "sha256:abc", "agent:writer", now)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := NewValidationRecord("validation-1", revision, "structure", ValidatorStructure, VerdictFailed, nil, "agent:validator", "", nil, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	passed, err := NewValidationRecord("validation-2", revision, "structure", ValidatorStructure, VerdictPassed, nil, "agent:validator", "", nil, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	review, err := NewValidationRecord("validation-3", revision, "research-quality/v1", ValidatorHumanReview, VerdictWaived, nil, "human:reviewer", "", json.RawMessage(`{"rationale":"Accepted under the documented exception."}`), now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	accepted, err := AcceptOutputRevision(revision, expected, profile, []ValidationRecord{failed, passed, review}, "human:owner", "All required validation criteria are satisfied.", now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if revision.AcceptanceState != RevisionProduced {
		t.Fatalf("input revision was mutated: %#v", revision)
	}
	if accepted.AcceptanceState != RevisionAccepted || accepted.AcceptedBy != "human:owner" {
		t.Fatalf("unexpected accepted revision: %#v", accepted)
	}
}

func TestAcceptOutputRevisionRejectsMissingAndLatestFailedValidation(t *testing.T) {
	now := time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC)
	profile := activeProfile(json.RawMessage(`{"required":[{"kind":"structure"},{"kind":"provenance"}]}`))
	expected, err := NewExpectedOutput("expected-1", "item-1", "Dossier", profile, nil, "", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := NewOutputRevision("revision-1", expected, profile, 1, []RevisionArtifact{{ArtifactID: "artifact-1"}}, "", "agent:writer", now)
	if err != nil {
		t.Fatal(err)
	}
	passed, err := NewValidationRecord("validation-1", revision, "structure", ValidatorStructure, VerdictPassed, nil, "agent:validator", "", nil, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	_, err = AcceptOutputRevision(revision, expected, profile, []ValidationRecord{passed}, "human:owner", "Reviewed.", now.Add(2*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("expected missing-provenance error, got %v", err)
	}

	failed, err := NewValidationRecord("validation-2", revision, "structure", ValidatorStructure, VerdictFailed, nil, "agent:validator", "", nil, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	waived, err := NewValidationRecord("validation-3", revision, "provenance", ValidatorProvenance, VerdictWaived, nil, "human:owner", "", nil, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, err = AcceptOutputRevision(revision, expected, profile, []ValidationRecord{passed, failed, waived}, "human:owner", "Reviewed.", now.Add(5*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected latest-failed error, got %v", err)
	}
}

func TestOutputRevisionValidationIsIsolatedByRevision(t *testing.T) {
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	profile := activeProfile(json.RawMessage(`{"required":[{"kind":"structure"}]}`))
	expected, err := NewExpectedOutput("expected-1", "item-1", "Dossier", profile, nil, "", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	revision1, err := NewOutputRevision("revision-1", expected, profile, 1, []RevisionArtifact{{ArtifactID: "artifact-1"}}, "", "agent:writer", now)
	if err != nil {
		t.Fatal(err)
	}
	revision2, err := NewOutputRevision("revision-2", expected, profile, 2, []RevisionArtifact{{ArtifactID: "artifact-2"}}, "", "agent:writer", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	validation, err := NewValidationRecord("validation-1", revision1, "structure", ValidatorStructure, VerdictPassed, nil, "agent:validator", "", nil, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	_, err = AcceptOutputRevision(revision2, expected, profile, []ValidationRecord{validation}, "human:owner", "Reviewed.", now.Add(3*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected revision-isolation error, got %v", err)
	}
}

func TestAcceptanceCombinesProfileAndInstanceValidation(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	profile := activeProfile(json.RawMessage(`{"required":[{"kind":"structure"}]}`))
	expected, err := NewExpectedOutput("expected-1", "item-1", "Dossier", profile, json.RawMessage(`{
		"minimum_sources": 3,
		"validation": {"required":[{"kind":"evaluation","criterion_ref":"minimum_sources"}]}
	}`), "", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := NewOutputRevision("revision-1", expected, profile, 1, []RevisionArtifact{{ArtifactID: "artifact-1"}}, "", "agent:writer", now)
	if err != nil {
		t.Fatal(err)
	}
	structure, err := NewValidationRecord("validation-1", revision, "structure", ValidatorStructure, VerdictPassed, nil, "agent:validator", "", nil, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcceptOutputRevision(revision, expected, profile, []ValidationRecord{structure}, "human:owner", "Reviewed.", now.Add(2*time.Minute)); !errors.Is(err, ErrAcceptanceIncomplete) {
		t.Fatalf("expected instance validation to remain required, got %v", err)
	}
	evaluation, err := NewValidationRecord("validation-2", revision, "minimum_sources", ValidatorEvaluation, VerdictPassed, nil, "human:owner", "", nil, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcceptOutputRevision(revision, expected, profile, []ValidationRecord{structure, evaluation}, "human:owner", "Reviewed.", now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredValidationCriterionCombinesProfileAndInstanceContract(t *testing.T) {
	profile := activeProfile(json.RawMessage(`{"required":[{"kind":"structure"}]}`))
	expected, err := NewExpectedOutput("expected-1", "item-1", "Dossier", profile, json.RawMessage(`{
		"validation": {"required":[{"kind":"evaluation","criterion_ref":"minimum_sources"}]}
	}`), "", true, 1)
	if err != nil {
		t.Fatal(err)
	}

	for _, criterion := range []string{"structure", "minimum_sources"} {
		required, err := IsRequiredValidationCriterion(expected, profile, criterion)
		if err != nil {
			t.Fatal(err)
		}
		if !required {
			t.Fatalf("criterion %q should be required", criterion)
		}
	}
	required, err := IsRequiredValidationCriterion(expected, profile, "consumer-readiness")
	if err != nil {
		t.Fatal(err)
	}
	if required {
		t.Fatal("supplemental successor-use criterion should not be required")
	}
}

func TestOutputRequirementSelectsExactlyOneTarget(t *testing.T) {
	revision := OutputRevision{ID: "revision-1", AcceptanceState: RevisionProduced}
	exact, err := NewExactOutputRequirement("requirement-1", "item-2", revision, true, "Reuse the accepted dossier.")
	if err != nil {
		t.Fatal(err)
	}
	if exact.RequiredOutputRevisionID != revision.ID || exact.RequiredProfileName != "" {
		t.Fatalf("unexpected exact requirement: %#v", exact)
	}

	compatible, err := NewProfileOutputRequirement("requirement-2", "item-3", "research_dossier", "=1", true, "An exact profile version.")
	if err != nil {
		t.Fatal(err)
	}
	if compatible.RequiredOutputRevisionID != "" || compatible.RequiredProfileName != "research_dossier" || compatible.VersionConstraint != "=1" {
		t.Fatalf("unexpected profile requirement: %#v", compatible)
	}
}

func activeProfile(validation json.RawMessage) Profile {
	return Profile{
		ID:             "profile-1",
		Name:           "research_dossier",
		Version:        1,
		LifecycleState: ProfileActive,
		Validation:     validation,
	}
}
