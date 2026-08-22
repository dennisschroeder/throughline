package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Artifact struct {
	ID         string
	WorkItemID string
	Kind       string
	URI        string
	Title      string
	Metadata   json.RawMessage
	AttachedBy string
	CreatedAt  time.Time
}

func NewArtifact(artifact Artifact, now time.Time) (Artifact, error) {
	artifact.ID = strings.TrimSpace(artifact.ID)
	artifact.WorkItemID = strings.TrimSpace(artifact.WorkItemID)
	artifact.Kind = strings.TrimSpace(artifact.Kind)
	artifact.URI = strings.TrimSpace(artifact.URI)
	artifact.Title = strings.TrimSpace(artifact.Title)
	artifact.AttachedBy = strings.TrimSpace(artifact.AttachedBy)
	if artifact.ID == "" || artifact.WorkItemID == "" || artifact.Kind == "" || artifact.URI == "" || artifact.AttachedBy == "" {
		return Artifact{}, errors.New("artifact requires id, work item id, kind, URI, and attaching actor")
	}
	if strings.ContainsAny(artifact.URI, " \t\r\n") {
		return Artifact{}, errors.New("artifact URI must be an absolute URI")
	}
	parsed, err := url.Parse(artifact.URI)
	if err != nil || !parsed.IsAbs() {
		return Artifact{}, errors.New("artifact URI must be an absolute URI")
	}
	if len(artifact.Metadata) == 0 {
		artifact.Metadata = json.RawMessage(`{}`)
	}
	if err := validateJSONObject(artifact.Metadata); err != nil {
		return Artifact{}, fmt.Errorf("artifact metadata: %w", err)
	}
	artifact.Metadata = append(json.RawMessage(nil), artifact.Metadata...)
	artifact.CreatedAt = now.UTC()
	return artifact, nil
}

type RevisionAcceptanceState string

var ErrAcceptanceIncomplete = errors.New("output revision acceptance is incomplete")

const (
	RevisionProduced   RevisionAcceptanceState = "produced"
	RevisionAccepted   RevisionAcceptanceState = "accepted"
	RevisionRejected   RevisionAcceptanceState = "rejected"
	RevisionSuperseded RevisionAcceptanceState = "superseded"
)

type RevisionArtifact struct {
	ArtifactID string
	Role       string
}

type OutputRevision struct {
	ID               string
	ExpectedOutputID string
	OutputProfileID  string
	Revision         int
	Artifacts        []RevisionArtifact
	ContentDigest    string
	AcceptanceState  RevisionAcceptanceState
	ProducedBy       string
	ProducedAt       time.Time
	AcceptedBy       string
	AcceptedAt       time.Time
	AcceptanceReason string
}

func NewOutputRevision(id string, expected ExpectedOutput, profile Profile, revision int, artifacts []RevisionArtifact, contentDigest, producedBy string, now time.Time) (OutputRevision, error) {
	id = strings.TrimSpace(id)
	producedBy = strings.TrimSpace(producedBy)
	if id == "" || expected.ID == "" || profile.ID == "" || producedBy == "" {
		return OutputRevision{}, errors.New("output revision requires id, expected output, exact profile, and producer")
	}
	if expected.OutputProfileID != profile.ID {
		return OutputRevision{}, errors.New("output revision profile does not match expected output profile")
	}
	if revision < 1 {
		return OutputRevision{}, errors.New("output revision number must be positive")
	}
	if len(artifacts) == 0 {
		return OutputRevision{}, errors.New("output revision requires at least one artifact")
	}

	bindings := make([]RevisionArtifact, len(artifacts))
	seenArtifacts := make(map[string]struct{}, len(artifacts))
	for index, binding := range artifacts {
		binding.ArtifactID = strings.TrimSpace(binding.ArtifactID)
		binding.Role = strings.TrimSpace(binding.Role)
		if binding.ArtifactID == "" {
			return OutputRevision{}, errors.New("output revision artifact requires artifact id")
		}
		if _, exists := seenArtifacts[binding.ArtifactID]; exists {
			return OutputRevision{}, fmt.Errorf("output revision repeats artifact %q", binding.ArtifactID)
		}
		seenArtifacts[binding.ArtifactID] = struct{}{}
		if binding.Role == "" {
			binding.Role = "primary"
		}
		bindings[index] = binding
	}

	return OutputRevision{
		ID:               id,
		ExpectedOutputID: expected.ID,
		OutputProfileID:  profile.ID,
		Revision:         revision,
		Artifacts:        bindings,
		ContentDigest:    strings.TrimSpace(contentDigest),
		AcceptanceState:  RevisionProduced,
		ProducedBy:       producedBy,
		ProducedAt:       now.UTC(),
	}, nil
}

type ValidatorKind string

const (
	ValidatorStructure    ValidatorKind = "structure"
	ValidatorSchema       ValidatorKind = "schema"
	ValidatorEvaluation   ValidatorKind = "evaluation"
	ValidatorProvenance   ValidatorKind = "provenance"
	ValidatorHumanReview  ValidatorKind = "human_review"
	ValidatorPolicy       ValidatorKind = "policy"
	ValidatorProbe        ValidatorKind = "probe"
	ValidatorSuccessorUse ValidatorKind = "successor_use"
)

type ValidationVerdict string

const (
	VerdictPassed ValidationVerdict = "passed"
	VerdictFailed ValidationVerdict = "failed"
	VerdictWaived ValidationVerdict = "waived"
)

type ValidationRecord struct {
	ID                 string
	OutputRevisionID   string
	CriterionRef       string
	ValidatorKind      ValidatorKind
	Verdict            ValidationVerdict
	Score              *float64
	VerifierActorID    string
	EvidenceArtifactID string
	Details            json.RawMessage
	CreatedAt          time.Time
}

func NewValidationRecord(id string, revision OutputRevision, criterionRef string, kind ValidatorKind, verdict ValidationVerdict, score *float64, verifierActorID, evidenceArtifactID string, details json.RawMessage, now time.Time) (ValidationRecord, error) {
	id = strings.TrimSpace(id)
	criterionRef = strings.TrimSpace(criterionRef)
	verifierActorID = strings.TrimSpace(verifierActorID)
	evidenceArtifactID = strings.TrimSpace(evidenceArtifactID)
	if id == "" || revision.ID == "" || criterionRef == "" {
		return ValidationRecord{}, errors.New("validation record requires id, output revision, and criterion reference")
	}
	if !kind.supported() {
		return ValidationRecord{}, fmt.Errorf("unsupported validator kind %q", kind)
	}
	if verifierActorID == "" {
		if kind == ValidatorHumanReview {
			return ValidationRecord{}, errors.New("human review requires a named verifier")
		}
		return ValidationRecord{}, errors.New("validation record requires a verifier")
	}
	if !verdict.supported() {
		return ValidationRecord{}, fmt.Errorf("unsupported validation verdict %q", verdict)
	}
	if score != nil && (math.IsNaN(*score) || math.IsInf(*score, 0)) {
		return ValidationRecord{}, errors.New("validation score must be finite")
	}
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	if err := validateJSONObject(details); err != nil {
		return ValidationRecord{}, fmt.Errorf("validation details: %w", err)
	}
	if kind == ValidatorHumanReview {
		var reviewDetails struct {
			Rationale string `json:"rationale"`
		}
		if err := json.Unmarshal(details, &reviewDetails); err != nil {
			return ValidationRecord{}, fmt.Errorf("human review details: %w", err)
		}
		if strings.TrimSpace(reviewDetails.Rationale) == "" {
			return ValidationRecord{}, errors.New("human review requires rationale in details")
		}
	}

	var recordScore *float64
	if score != nil {
		value := *score
		recordScore = &value
	}
	return ValidationRecord{
		ID:                 id,
		OutputRevisionID:   revision.ID,
		CriterionRef:       criterionRef,
		ValidatorKind:      kind,
		Verdict:            verdict,
		Score:              recordScore,
		VerifierActorID:    verifierActorID,
		EvidenceArtifactID: evidenceArtifactID,
		Details:            append(json.RawMessage(nil), details...),
		CreatedAt:          now.UTC(),
	}, nil
}

func AcceptOutputRevision(revision OutputRevision, expected ExpectedOutput, profile Profile, validations []ValidationRecord, acceptedBy, reason string, now time.Time) (OutputRevision, error) {
	acceptedBy = strings.TrimSpace(acceptedBy)
	reason = strings.TrimSpace(reason)
	if revision.AcceptanceState != RevisionProduced {
		return OutputRevision{}, errors.New("only produced output revisions can be accepted")
	}
	if revision.OutputProfileID == "" || revision.OutputProfileID != profile.ID {
		return OutputRevision{}, errors.New("output revision does not match exact output profile")
	}
	if revision.ExpectedOutputID != expected.ID || expected.OutputProfileID != profile.ID {
		return OutputRevision{}, errors.New("output revision does not match exact expected output contract")
	}
	if acceptedBy == "" || reason == "" {
		return OutputRevision{}, errors.New("output revision acceptance requires actor and reason")
	}
	required, err := combinedRequiredValidations(profile, expected.Contract)
	if err != nil {
		return OutputRevision{}, err
	}
	latest := latestValidationsForRevision(revision.ID, validations)
	for _, requirement := range required {
		record, exists := latest[requirement.CriterionRef]
		if !exists {
			return OutputRevision{}, fmt.Errorf("%w: required validation %q is missing", ErrAcceptanceIncomplete, requirement.CriterionRef)
		}
		if record.ValidatorKind != requirement.Kind {
			return OutputRevision{}, fmt.Errorf("%w: required validation %q uses validator kind %q, got %q", ErrAcceptanceIncomplete, requirement.CriterionRef, requirement.Kind, record.ValidatorKind)
		}
		if record.Verdict != VerdictPassed && record.Verdict != VerdictWaived {
			return OutputRevision{}, fmt.Errorf("%w: required validation %q has latest verdict %q", ErrAcceptanceIncomplete, requirement.CriterionRef, record.Verdict)
		}
	}

	accepted := revision
	accepted.Artifacts = append([]RevisionArtifact(nil), revision.Artifacts...)
	accepted.AcceptanceState = RevisionAccepted
	accepted.AcceptedBy = acceptedBy
	accepted.AcceptedAt = now.UTC()
	accepted.AcceptanceReason = reason
	return accepted, nil
}

func instanceRequiredValidations(contract json.RawMessage) ([]requiredValidation, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(contract, &object); err != nil {
		return nil, err
	}
	if len(object) == 0 {
		return nil, nil
	}
	validation, exists := object["validation"]
	if !exists {
		return nil, errors.New("non-empty contract requires validation.required")
	}
	required, err := parseRequiredValidations(validation)
	if err != nil {
		return nil, err
	}
	if len(required) == 0 {
		return nil, errors.New("non-empty contract requires at least one validation rule")
	}
	return required, nil
}

func IsRequiredValidationCriterion(expected ExpectedOutput, profile Profile, criterionRef string) (bool, error) {
	criterionRef = strings.TrimSpace(criterionRef)
	required, err := combinedRequiredValidations(profile, expected.Contract)
	if err != nil {
		return false, err
	}
	for _, requirement := range required {
		if requirement.CriterionRef == criterionRef {
			return true, nil
		}
	}
	return false, nil
}

func combinedRequiredValidations(profile Profile, contract json.RawMessage) ([]requiredValidation, error) {
	required, err := parseRequiredValidations(profile.Validation)
	if err != nil {
		return nil, fmt.Errorf("output profile validation: %w", err)
	}
	instanceRequired, err := instanceRequiredValidations(contract)
	if err != nil {
		return nil, fmt.Errorf("expected output contract: %w", err)
	}
	seen := make(map[string]struct{}, len(required)+len(instanceRequired))
	for _, requirement := range required {
		seen[requirement.CriterionRef] = struct{}{}
	}
	for _, requirement := range instanceRequired {
		if _, exists := seen[requirement.CriterionRef]; exists {
			return nil, fmt.Errorf("expected output contract duplicates profile validation criterion %q", requirement.CriterionRef)
		}
		seen[requirement.CriterionRef] = struct{}{}
		required = append(required, requirement)
	}
	return required, nil
}

type requiredValidation struct {
	Kind         ValidatorKind `json:"kind"`
	CriterionRef string        `json:"criterion_ref"`
	Rubric       string        `json:"rubric"`
}

func parseRequiredValidations(raw json.RawMessage) ([]requiredValidation, error) {
	var definition struct {
		Required []requiredValidation `json:"required"`
	}
	if err := json.Unmarshal(raw, &definition); err != nil {
		return nil, err
	}
	seenCriteria := make(map[string]struct{}, len(definition.Required))
	for index := range definition.Required {
		requirement := &definition.Required[index]
		if !requirement.Kind.supported() {
			return nil, fmt.Errorf("required validation has unsupported kind %q", requirement.Kind)
		}
		requirement.CriterionRef = strings.TrimSpace(requirement.CriterionRef)
		requirement.Rubric = strings.TrimSpace(requirement.Rubric)
		if requirement.CriterionRef == "" {
			requirement.CriterionRef = requirement.Rubric
		}
		if requirement.CriterionRef == "" {
			requirement.CriterionRef = string(requirement.Kind)
		}
		if _, exists := seenCriteria[requirement.CriterionRef]; exists {
			return nil, fmt.Errorf("duplicate required validation criterion %q", requirement.CriterionRef)
		}
		seenCriteria[requirement.CriterionRef] = struct{}{}
	}
	return definition.Required, nil
}

func latestValidationsForRevision(revisionID string, validations []ValidationRecord) map[string]ValidationRecord {
	latest := make(map[string]ValidationRecord)
	for _, record := range validations {
		if record.OutputRevisionID != revisionID {
			continue
		}
		current, exists := latest[record.CriterionRef]
		if !exists || record.CreatedAt.After(current.CreatedAt) || (record.CreatedAt.Equal(current.CreatedAt) && record.ID > current.ID) {
			latest[record.CriterionRef] = record
		}
	}
	return latest
}

func (kind ValidatorKind) supported() bool {
	switch kind {
	case ValidatorStructure, ValidatorSchema, ValidatorEvaluation, ValidatorProvenance, ValidatorHumanReview, ValidatorPolicy, ValidatorProbe, ValidatorSuccessorUse:
		return true
	default:
		return false
	}
}

func (verdict ValidationVerdict) supported() bool {
	switch verdict {
	case VerdictPassed, VerdictFailed, VerdictWaived:
		return true
	default:
		return false
	}
}

type OutputRequirement struct {
	ID                       string
	WorkItemID               string
	RequiredOutputRevisionID string
	RequiredProfileName      string
	VersionConstraint        string
	Required                 bool
	Note                     string
}

func NewExactOutputRequirement(id, workItemID string, revision OutputRevision, required bool, note string) (OutputRequirement, error) {
	requirement := OutputRequirement{
		ID:                       strings.TrimSpace(id),
		WorkItemID:               strings.TrimSpace(workItemID),
		RequiredOutputRevisionID: strings.TrimSpace(revision.ID),
		Required:                 required,
		Note:                     strings.TrimSpace(note),
	}
	if requirement.ID == "" || requirement.WorkItemID == "" || requirement.RequiredOutputRevisionID == "" {
		return OutputRequirement{}, errors.New("exact output requirement requires id, work item id, and output revision")
	}
	return requirement, nil
}

func NewProfileOutputRequirement(id, workItemID, profileName, versionConstraint string, required bool, note string) (OutputRequirement, error) {
	requirement := OutputRequirement{
		ID:                  strings.TrimSpace(id),
		WorkItemID:          strings.TrimSpace(workItemID),
		RequiredProfileName: strings.TrimSpace(profileName),
		VersionConstraint:   strings.TrimSpace(versionConstraint),
		Required:            required,
		Note:                strings.TrimSpace(note),
	}
	if requirement.ID == "" || requirement.WorkItemID == "" || requirement.RequiredProfileName == "" || requirement.VersionConstraint == "" {
		return OutputRequirement{}, errors.New("profile output requirement requires id, work item id, profile name, and version constraint")
	}
	version := strings.TrimPrefix(requirement.VersionConstraint, "=")
	parsedVersion, err := strconv.Atoi(version)
	if err != nil || parsedVersion < 1 {
		return OutputRequirement{}, errors.New("profile output requirement supports only a positive exact version constraint")
	}
	return requirement, nil
}
