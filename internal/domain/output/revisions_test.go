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

	// "not a URI" is rejected by the whitespace it contains, not because it lacks a
	// scheme; a schemeless relative reference with no whitespace at all must still be
	// rejected on its own, or it would be accepted with none of the workspace: scheme's
	// containment guarantee applied to it.
	_, err = NewArtifact(Artifact{ID: "artifact-3", WorkItemID: "item-1", Kind: "document", URI: "docs/report.md", AttachedBy: "agent:writer"}, now)
	if err == nil || !strings.Contains(err.Error(), "URI") {
		t.Fatalf("expected URI error for a schemeless relative reference, got %v", err)
	}
}

// TestNewArtifactAcceptsAWorkspaceRelativeReference is REP-05's first artifact
// criterion: a relative reference is a distinct, explicitly marked form, not
// inferred from a URI that happens to lack a scheme.
func TestNewArtifactAcceptsAWorkspaceRelativeReference(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	for _, spelling := range []string{"workspace:docs/report.md", "workspace:/docs/report.md", "workspace:///docs/report.md"} {
		artifact, err := NewArtifact(Artifact{
			ID: "artifact-1", WorkItemID: "item-1", Kind: "document", URI: spelling, AttachedBy: "agent:writer",
		}, now)
		if err != nil {
			t.Fatalf("%s: %v", spelling, err)
		}
		if artifact.URI != "workspace:docs/report.md" {
			t.Fatalf("%s normalized to %q, want the canonical spelling", spelling, artifact.URI)
		}
	}
}

// TestNewArtifactRejectsAWorkspaceReferenceCarryingLostComponents guards against
// silent information loss: a host, userinfo, query, or fragment on a
// workspace: URI would otherwise vanish from the reconstructed workspace:<path>
// form, letting two references the caller meant as distinct collide under the
// dedup lookup as though they were spellings of the same thing.
func TestNewArtifactRejectsAWorkspaceReferenceCarryingLostComponents(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	for _, uri := range []string{
		"workspace:docs/report.md?v=2",
		"workspace:docs/report.md#section",
		"workspace://host/docs/report.md",
		"workspace://user:pass@host/docs/report.md",
		"workspace://user@/docs/report.md", // userinfo without a host
	} {
		_, err := NewArtifact(Artifact{
			ID: "artifact-1", WorkItemID: "item-1", Kind: "document", URI: uri, AttachedBy: "agent:writer",
		}, now)
		if err == nil {
			t.Fatalf("%s: accepted, want rejection of the component that would be silently dropped", uri)
		}
	}
}

// TestNewArtifactNormalizesAWorkspaceReferenceIdempotently guards against a
// canonical form that fails its own validator on a second pass: the
// hierarchical spelling of a workspace: URI decodes percent-encoding before
// this function ever sees it, so a literal "?" or "#" in a filename must be
// re-escaped on the way out, not written back raw where it would reparse as
// a query or fragment next time.
func TestNewArtifactNormalizesAWorkspaceReferenceIdempotently(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	first, err := NewArtifact(Artifact{
		ID: "artifact-1", WorkItemID: "item-1", Kind: "document", URI: "workspace:///notes%3F.md", AttachedBy: "agent:writer",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewArtifact(Artifact{
		ID: "artifact-2", WorkItemID: "item-1", Kind: "document", URI: first.URI, AttachedBy: "agent:writer",
	}, now)
	if err != nil {
		t.Fatalf("the URI this function returned was rejected by the same function: %v", err)
	}
	if second.URI != first.URI {
		t.Fatalf("re-normalizing %q produced %q, want it unchanged", first.URI, second.URI)
	}
}

// TestNewArtifactNormalizesAPercentEncodedLeadingSlashIdempotently covers the
// opaque spelling specifically: an opaque path decodes to a leading slash
// (workspace:%2Fdocs%2Freport.md decodes to "/docs/report.md") the same way
// the hierarchical form's already-decoded Path does, but only the
// hierarchical branch trimmed that leading slash before this fix — so the
// opaque form's first normalization produced "workspace:/docs/report.md",
// and re-normalizing that output silently dropped the slash a second time
// instead of returning it unchanged.
func TestNewArtifactNormalizesAPercentEncodedLeadingSlashIdempotently(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	first, err := NewArtifact(Artifact{
		ID: "artifact-1", WorkItemID: "item-1", Kind: "document", URI: "workspace:%2Fdocs%2Freport.md", AttachedBy: "agent:writer",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.URI != "workspace:docs/report.md" {
		t.Fatalf("normalized URI = %q, want the leading slash trimmed like the hierarchical form", first.URI)
	}
	second, err := NewArtifact(Artifact{
		ID: "artifact-2", WorkItemID: "item-1", Kind: "document", URI: first.URI, AttachedBy: "agent:writer",
	}, now)
	if err != nil {
		t.Fatalf("the URI this function returned was rejected by the same function: %v", err)
	}
	if second.URI != first.URI {
		t.Fatalf("re-normalizing %q produced %q, want it unchanged", first.URI, second.URI)
	}
}

// TestNewArtifactDeduplicatesWorkspaceReferencesAcrossSpellings is REP-05's
// second criterion applied to the relative form: the opaque and hierarchical
// spellings of a workspace: URI naming the same literal file must normalize
// to the same string, or the two forms would silently defeat ArtifactByURI's
// dedup lookup for exactly the filenames that need percent-encoding at all.
func TestNewArtifactDeduplicatesWorkspaceReferencesAcrossSpellings(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	opaque, err := NewArtifact(Artifact{
		ID: "artifact-1", WorkItemID: "item-1", Kind: "document", URI: "workspace:notes%3F.md", AttachedBy: "agent:writer",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	hierarchical, err := NewArtifact(Artifact{
		ID: "artifact-2", WorkItemID: "item-1", Kind: "document", URI: "workspace:///notes%3F.md", AttachedBy: "agent:writer",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if opaque.URI != hierarchical.URI {
		t.Fatalf("opaque form normalized to %q, hierarchical form to %q, want the same string", opaque.URI, hierarchical.URI)
	}
}

// TestNewArtifactRejectsAWorkspaceReferenceThatEscapesTheRoot is REP-05's second
// artifact criterion, the containment half: no cleaning of a workspace-relative
// path may climb above canonical_root, and this must hold lexically since the
// domain layer never sees canonical_root itself.
func TestNewArtifactRejectsAWorkspaceReferenceThatEscapesTheRoot(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	for _, escaping := range []string{
		"workspace:../escape.md",
		"workspace:./a/../../escape.md",
		"workspace:..",
		"workspace:",
	} {
		_, err := NewArtifact(Artifact{
			ID: "artifact-1", WorkItemID: "item-1", Kind: "document", URI: escaping, AttachedBy: "agent:writer",
		}, now)
		if err == nil {
			t.Fatalf("%s: escaping reference accepted", escaping)
		}
	}
}

// TestNewArtifactNormalizesEquivalentAbsoluteURIsIdentically is REP-05's second
// artifact criterion, the dedup half: two differently-spelled references to the
// same resource must compare equal, since that equality is what the existing
// ArtifactByURI lookup in attachArtifactMutation relies on to deduplicate.
func TestNewArtifactNormalizesEquivalentAbsoluteURIsIdentically(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	for _, pair := range [][2]string{
		{"file:///a/./b.md", "file:///a/b.md"},
		{"file:///a/x/../b.md", "file:///a/b.md"},
		{"https://example.com/docs//report.md", "https://example.com/docs/report.md"},
	} {
		first, err := NewArtifact(Artifact{ID: "artifact-1", WorkItemID: "item-1", Kind: "document", URI: pair[0], AttachedBy: "agent:writer"}, now)
		if err != nil {
			t.Fatalf("%s: %v", pair[0], err)
		}
		second, err := NewArtifact(Artifact{ID: "artifact-2", WorkItemID: "item-1", Kind: "document", URI: pair[1], AttachedBy: "agent:writer"}, now)
		if err != nil {
			t.Fatalf("%s: %v", pair[1], err)
		}
		if first.URI != second.URI {
			t.Fatalf("%q and %q normalized to %q and %q, want the same string", pair[0], pair[1], first.URI, second.URI)
		}
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

func TestNewWorkItemValidationRecordBindsTheItemAsItsOnlySubject(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	record, err := NewWorkItemValidationRecord("review-1", " item-1 ", 42, "code-review", ValidatorHumanReview, VerdictPassed, nil, "human:reviewer", "", json.RawMessage(`{"rationale":"Read the diff."}`), true, now)
	if err != nil {
		t.Fatal(err)
	}
	if record.WorkItemID != "item-1" || record.OutputRevisionID != "" || record.SubjectSequence != 42 || !record.Degraded || record.Version != 1 {
		t.Fatalf("work item review = %#v", record)
	}
	for name, build := range map[string]func() (ValidationRecord, error){
		"no work item": func() (ValidationRecord, error) {
			return NewWorkItemValidationRecord("r", " ", 1, "code-review", ValidatorProbe, VerdictPassed, nil, "agent:ci", "", nil, false, now)
		},
		"successor use": func() (ValidationRecord, error) {
			return NewWorkItemValidationRecord("r", "item-1", 1, "code-review", ValidatorSuccessorUse, VerdictPassed, nil, "agent:ci", "", nil, false, now)
		},
		"negative sequence": func() (ValidationRecord, error) {
			return NewWorkItemValidationRecord("r", "item-1", -1, "code-review", ValidatorProbe, VerdictPassed, nil, "agent:ci", "", nil, false, now)
		},
		"human review without rationale": func() (ValidationRecord, error) {
			return NewWorkItemValidationRecord("r", "item-1", 1, "code-review", ValidatorHumanReview, VerdictPassed, nil, "human:reviewer", "", nil, false, now)
		},
	} {
		if _, err := build(); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}
