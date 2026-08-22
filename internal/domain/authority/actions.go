package authority

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ExternalActionState string

const (
	ActionProposed   ExternalActionState = "proposed"
	ActionAuthorized ExternalActionState = "authorized"
	ActionExecuting  ExternalActionState = "executing"
	ActionSucceeded  ExternalActionState = "succeeded"
	ActionFailed     ExternalActionState = "failed"
	ActionRejected   ExternalActionState = "rejected"
	ActionCancelled  ExternalActionState = "cancelled"
	ActionExpired    ExternalActionState = "expired"
)

type ExecutionState string

const (
	ExecutionStarted   ExecutionState = "started"
	ExecutionSucceeded ExecutionState = "succeeded"
	ExecutionFailed    ExecutionState = "failed"
)

// CanonicalAuthorizationSubject keeps the bytes and digest produced by one canonicalization pass.
type CanonicalAuthorizationSubject struct {
	JSON json.RawMessage
	Hash string
}

func NewCanonicalAuthorizationSubject(raw []byte) (CanonicalAuthorizationSubject, error) {
	canonical, err := CanonicalizeSubject(raw)
	if err != nil {
		return CanonicalAuthorizationSubject{}, err
	}
	digest := sha256.Sum256(canonical)
	return CanonicalAuthorizationSubject{JSON: append(json.RawMessage(nil), canonical...), Hash: fmt.Sprintf("%x", digest)}, nil
}

type ExternalAction struct {
	ID              string
	WorkItemID      string
	ActionType      string
	Required        bool
	CurrentRevision int
	State           ExternalActionState
	Title           string
	Rationale       string
	Version         int
	CreatedBy       string
	UpdatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ExternalActionRevision struct {
	ExternalActionID         string
	Revision                 int
	AuthorizationSubject     json.RawMessage
	AuthorizationSubjectHash string
	ProposedBy               string
	ProposedAt               time.Time
}

func NewExternalAction(action ExternalAction, rawSubject []byte, proposedBy string, now time.Time) (ExternalAction, ExternalActionRevision, error) {
	proposedBy = strings.TrimSpace(proposedBy)
	if proposedBy == "" {
		return ExternalAction{}, ExternalActionRevision{}, errors.New("external action requires a proposer")
	}
	revision, err := NewExternalActionRevision(action.ID, 1, rawSubject, proposedBy, now)
	if err != nil {
		return ExternalAction{}, ExternalActionRevision{}, err
	}
	action.ID = strings.TrimSpace(action.ID)
	action.WorkItemID = strings.TrimSpace(action.WorkItemID)
	action.Title = strings.TrimSpace(action.Title)
	action.Rationale = strings.TrimSpace(action.Rationale)
	action.ActionType = subjectActionType(revision.AuthorizationSubject)
	action.CurrentRevision = 1
	action.State = ActionProposed
	action.Version = 1
	action.CreatedBy = proposedBy
	action.UpdatedBy = proposedBy
	action.CreatedAt = now.UTC()
	action.UpdatedAt = now.UTC()
	if err := action.Validate(); err != nil {
		return ExternalAction{}, ExternalActionRevision{}, err
	}
	return action, revision, nil
}

func NewExternalActionRevision(externalActionID string, revision int, rawSubject []byte, proposedBy string, now time.Time) (ExternalActionRevision, error) {
	externalActionID = strings.TrimSpace(externalActionID)
	proposedBy = strings.TrimSpace(proposedBy)
	if externalActionID == "" || proposedBy == "" || revision < 1 {
		return ExternalActionRevision{}, errors.New("external action revision requires action id, positive revision, and proposer")
	}
	subject, err := NewCanonicalAuthorizationSubject(rawSubject)
	if err != nil {
		return ExternalActionRevision{}, fmt.Errorf("external action revision subject: %w", err)
	}
	return ExternalActionRevision{
		ExternalActionID:         externalActionID,
		Revision:                 revision,
		AuthorizationSubject:     append(json.RawMessage(nil), subject.JSON...),
		AuthorizationSubjectHash: subject.Hash,
		ProposedBy:               proposedBy,
		ProposedAt:               now.UTC(),
	}, nil
}

func ReviseExternalAction(action ExternalAction, current ExternalActionRevision, rawSubject []byte, proposedBy string, now time.Time) (ExternalAction, ExternalActionRevision, error) {
	if action.ID == "" || current.ExternalActionID != action.ID || current.Revision != action.CurrentRevision {
		return ExternalAction{}, ExternalActionRevision{}, errors.New("external action revision does not match the current action")
	}
	if action.State == ActionExecuting || action.State == ActionSucceeded || action.State == ActionFailed {
		return ExternalAction{}, ExternalActionRevision{}, errors.New("executing or terminal external actions cannot be revised")
	}
	next, err := NewExternalActionRevision(action.ID, action.CurrentRevision+1, rawSubject, proposedBy, now)
	if err != nil {
		return ExternalAction{}, ExternalActionRevision{}, err
	}
	if bytes.Equal(current.AuthorizationSubject, next.AuthorizationSubject) {
		return ExternalAction{}, ExternalActionRevision{}, errors.New("authorization subject is unchanged; edit metadata without creating a revision")
	}
	action.ActionType = subjectActionType(next.AuthorizationSubject)
	action.CurrentRevision = next.Revision
	action.State = ActionProposed
	action.Version++
	action.UpdatedBy = strings.TrimSpace(proposedBy)
	action.UpdatedAt = now.UTC()
	return action, next, nil
}

func UpdateExternalActionMetadata(action ExternalAction, title, rationale, actor string, now time.Time) (ExternalAction, error) {
	title = strings.TrimSpace(title)
	rationale = strings.TrimSpace(rationale)
	actor = strings.TrimSpace(actor)
	if title == "" || actor == "" {
		return ExternalAction{}, errors.New("external action metadata requires title and actor")
	}
	action.Title = title
	action.Rationale = rationale
	action.Version++
	action.UpdatedBy = actor
	action.UpdatedAt = now.UTC()
	return action, nil
}

func (action ExternalAction) Validate() error {
	if action.ID == "" || action.WorkItemID == "" || action.ActionType == "" || action.Title == "" {
		return errors.New("external action requires id, work item id, action type, and title")
	}
	if action.CurrentRevision < 1 || action.Version < 1 {
		return errors.New("external action revision and version must be positive")
	}
	if !validExternalActionState(action.State) {
		return fmt.Errorf("invalid external action state %q", action.State)
	}
	return nil
}

func TransitionExternalAction(action ExternalAction, target ExternalActionState, now time.Time) (ExternalAction, error) {
	if !validExternalActionTransition(action.State, target) {
		return ExternalAction{}, fmt.Errorf("external action cannot transition from %q to %q", action.State, target)
	}
	action.State = target
	action.Version++
	action.UpdatedAt = now.UTC()
	return action, nil
}

type AuthorityGrant struct {
	ID                       string
	ExternalActionID         string
	ActionRevision           int
	PrincipalActorID         string
	AuthorizationSubjectHash string
	Constraints              json.RawMessage
	SourceApprovalID         string
	GrantedBy                string
	GrantedAt                time.Time
	ExpiresAt                *time.Time
	RevokedBy                string
	RevokedAt                *time.Time
}

type ApprovalStatus string

const (
	ApprovalRequested ApprovalStatus = "requested"
	ApprovalApproved  ApprovalStatus = "approved"
	ApprovalRejected  ApprovalStatus = "rejected"
	ApprovalRevoked   ApprovalStatus = "revoked"
)

type ActionApproval struct {
	ID                       string
	ExternalActionID         string
	ExternalActionRevision   int
	ApprovedForActorID       string
	AuthorizationSubjectHash string
	Constraints              json.RawMessage
	ExpiresAt                *time.Time
	Request                  string
	Status                   ApprovalStatus
	RequestedBy              string
	RequestedAt              time.Time
	ResolvedBy               string
	ResolvedAt               *time.Time
	Rationale                string
}

func NewActionApproval(approval ActionApproval, revision ExternalActionRevision, now time.Time) (ActionApproval, error) {
	approval.ID = strings.TrimSpace(approval.ID)
	approval.ApprovedForActorID = strings.TrimSpace(approval.ApprovedForActorID)
	approval.Request = strings.TrimSpace(approval.Request)
	approval.RequestedBy = strings.TrimSpace(approval.RequestedBy)
	if approval.ID == "" || approval.ApprovedForActorID == "" || approval.Request == "" || approval.RequestedBy == "" {
		return ActionApproval{}, errors.New("action approval requires id, principal, request, and requester")
	}
	constraints, err := canonicalJSONObject(approval.Constraints)
	if err != nil {
		return ActionApproval{}, fmt.Errorf("action approval constraints: %w", err)
	}
	if !bytes.Equal(constraints, revisionConstraints(revision.AuthorizationSubject)) {
		return ActionApproval{}, errors.New("action approval constraints must exactly match the authorization subject")
	}
	if approval.ExpiresAt != nil {
		expiresAt := approval.ExpiresAt.UTC()
		if !expiresAt.After(now) {
			return ActionApproval{}, errors.New("action approval expiry must be in the future")
		}
		approval.ExpiresAt = &expiresAt
	}
	approval.ExternalActionID = revision.ExternalActionID
	approval.ExternalActionRevision = revision.Revision
	approval.AuthorizationSubjectHash = revision.AuthorizationSubjectHash
	approval.Constraints = constraints
	approval.Status = ApprovalRequested
	approval.RequestedAt = now.UTC()
	approval.ResolvedBy = ""
	approval.ResolvedAt = nil
	approval.Rationale = ""
	return approval, nil
}

func ResolveActionApproval(approval ActionApproval, decision ApprovalStatus, actor, rationale string, now time.Time) (ActionApproval, error) {
	actor = strings.TrimSpace(actor)
	rationale = strings.TrimSpace(rationale)
	if approval.Status != ApprovalRequested {
		return ActionApproval{}, errors.New("only requested action approvals can be resolved")
	}
	if decision != ApprovalApproved && decision != ApprovalRejected {
		return ActionApproval{}, errors.New("action approval resolution must approve or reject")
	}
	if actor == "" || rationale == "" {
		return ActionApproval{}, errors.New("action approval resolution requires actor and rationale")
	}
	resolvedAt := now.UTC()
	approval.Status = decision
	approval.ResolvedBy = actor
	approval.ResolvedAt = &resolvedAt
	approval.Rationale = rationale
	return approval, nil
}

func RevokeActionApproval(approval ActionApproval, actor, rationale string, now time.Time) (ActionApproval, error) {
	actor = strings.TrimSpace(actor)
	rationale = strings.TrimSpace(rationale)
	if approval.Status != ApprovalApproved {
		return ActionApproval{}, errors.New("only approved action approvals can be revoked")
	}
	if actor == "" || rationale == "" {
		return ActionApproval{}, errors.New("action approval revocation requires actor and rationale")
	}
	resolvedAt := now.UTC()
	approval.Status = ApprovalRevoked
	approval.ResolvedBy = actor
	approval.ResolvedAt = &resolvedAt
	approval.Rationale = rationale
	return approval, nil
}

func NewAuthorityGrant(grant AuthorityGrant, revision ExternalActionRevision, now time.Time) (AuthorityGrant, error) {
	grant.ID = strings.TrimSpace(grant.ID)
	grant.PrincipalActorID = strings.TrimSpace(grant.PrincipalActorID)
	grant.SourceApprovalID = strings.TrimSpace(grant.SourceApprovalID)
	grant.GrantedBy = strings.TrimSpace(grant.GrantedBy)
	if grant.ID == "" || grant.PrincipalActorID == "" || grant.SourceApprovalID == "" || grant.GrantedBy == "" {
		return AuthorityGrant{}, errors.New("authority grant requires id, principal, source approval, and granter")
	}
	if err := revision.Validate(); err != nil {
		return AuthorityGrant{}, fmt.Errorf("authority grant revision: %w", err)
	}
	constraints, err := canonicalJSONObject(grant.Constraints)
	if err != nil {
		return AuthorityGrant{}, fmt.Errorf("authority grant constraints: %w", err)
	}
	if !bytes.Equal(constraints, revisionConstraints(revision.AuthorizationSubject)) {
		return AuthorityGrant{}, errors.New("authority grant constraints must exactly match the authorization subject")
	}
	if grant.ExpiresAt != nil {
		expiresAt := grant.ExpiresAt.UTC()
		if !expiresAt.After(now) {
			return AuthorityGrant{}, errors.New("authority grant expiry must be in the future")
		}
		grant.ExpiresAt = &expiresAt
	}
	grant.ExternalActionID = revision.ExternalActionID
	grant.ActionRevision = revision.Revision
	grant.AuthorizationSubjectHash = revision.AuthorizationSubjectHash
	grant.Constraints = constraints
	grant.GrantedAt = now.UTC()
	grant.RevokedBy = ""
	grant.RevokedAt = nil
	return grant, nil
}

func RevokeAuthorityGrant(grant AuthorityGrant, revokedBy string, now time.Time) (AuthorityGrant, error) {
	revokedBy = strings.TrimSpace(revokedBy)
	if revokedBy == "" {
		return AuthorityGrant{}, errors.New("authority grant revocation requires an actor")
	}
	if grant.RevokedAt != nil {
		return AuthorityGrant{}, errors.New("authority grant is already revoked")
	}
	revokedAt := now.UTC()
	grant.RevokedBy = revokedBy
	grant.RevokedAt = &revokedAt
	return grant, nil
}

func (grant AuthorityGrant) ActiveAt(now time.Time) bool {
	if grant.RevokedAt != nil {
		return false
	}
	return grant.ExpiresAt == nil || grant.ExpiresAt.After(now)
}

type AuthorizationDenialReason string

const (
	DenialActionNotAuthorized AuthorizationDenialReason = "external_action_not_authorized"
	DenialApprovalRequired    AuthorizationDenialReason = "approval_required"
	DenialApprovalStale       AuthorizationDenialReason = "approval_stale"
	DenialSubjectMismatch     AuthorizationDenialReason = "authorization_subject_mismatch"
	DenialPrincipalMismatch   AuthorizationDenialReason = "authority_principal_mismatch"
	DenialGrantExpired        AuthorizationDenialReason = "authority_grant_expired"
	DenialGrantRevoked        AuthorizationDenialReason = "authority_grant_revoked"
	DenialConstraintMismatch  AuthorizationDenialReason = "authority_constraint_mismatch"
	DenialCapabilityMismatch  AuthorizationDenialReason = "capability_mismatch"
)

type AuthorizationDenial struct {
	Reason AuthorizationDenialReason
}

type AuthorizationDecision struct {
	Authorized bool
	GrantID    string
	Denial     *AuthorizationDenial
}

func CheckAuthorization(action ExternalAction, revision ExternalActionRevision, grant *AuthorityGrant, principalActorID, subjectHash string, now time.Time) AuthorizationDecision {
	principalActorID = strings.TrimSpace(principalActorID)
	subjectHash = strings.TrimSpace(subjectHash)
	deny := func(reason AuthorizationDenialReason) AuthorizationDecision {
		return AuthorizationDecision{Denial: &AuthorizationDenial{Reason: reason}}
	}
	if action.ID != revision.ExternalActionID || action.CurrentRevision != revision.Revision {
		return deny(DenialApprovalStale)
	}
	if subjectHash != revision.AuthorizationSubjectHash {
		return deny(DenialSubjectMismatch)
	}
	if grant == nil {
		return deny(DenialApprovalRequired)
	}
	if grant.ExternalActionID != action.ID || grant.ActionRevision != revision.Revision || grant.AuthorizationSubjectHash != revision.AuthorizationSubjectHash {
		return deny(DenialApprovalStale)
	}
	if grant.PrincipalActorID != principalActorID {
		return deny(DenialPrincipalMismatch)
	}
	if !bytes.Equal(grant.Constraints, revisionConstraints(revision.AuthorizationSubject)) {
		return deny(DenialConstraintMismatch)
	}
	if grant.RevokedAt != nil {
		return deny(DenialGrantRevoked)
	}
	if grant.ExpiresAt != nil && !grant.ExpiresAt.After(now) {
		return deny(DenialGrantExpired)
	}
	return AuthorizationDecision{Authorized: true, GrantID: grant.ID}
}

type ExternalActionExecution struct {
	ID               string
	ExternalActionID string
	ActionRevision   int
	PrincipalActorID string
	AuthorityGrantID string
	State            ExecutionState
	Result           json.RawMessage
	EvidenceIDs      []string
	StartedAt        time.Time
	FinishedAt       time.Time
}

func NewExternalActionExecution(id string, action ExternalAction, revision ExternalActionRevision, principalActorID, authorityGrantID string, now time.Time) (ExternalActionExecution, error) {
	id = strings.TrimSpace(id)
	principalActorID = strings.TrimSpace(principalActorID)
	authorityGrantID = strings.TrimSpace(authorityGrantID)
	if id == "" || principalActorID == "" || authorityGrantID == "" {
		return ExternalActionExecution{}, errors.New("external action execution requires id, principal, and authority grant")
	}
	if action.ID != revision.ExternalActionID || action.CurrentRevision != revision.Revision {
		return ExternalActionExecution{}, errors.New("external action execution must bind the current action revision")
	}
	return ExternalActionExecution{
		ID:               id,
		ExternalActionID: action.ID,
		ActionRevision:   revision.Revision,
		PrincipalActorID: principalActorID,
		AuthorityGrantID: authorityGrantID,
		State:            ExecutionStarted,
		StartedAt:        now.UTC(),
	}, nil
}

func CompleteExternalActionExecution(execution ExternalActionExecution, target ExecutionState, result json.RawMessage, evidenceIDs []string, now time.Time) (ExternalActionExecution, error) {
	if execution.State != ExecutionStarted {
		return ExternalActionExecution{}, errors.New("only started external action executions can be completed")
	}
	if target != ExecutionSucceeded && target != ExecutionFailed {
		return ExternalActionExecution{}, fmt.Errorf("external action execution cannot complete as %q", target)
	}
	normalizedResult, err := canonicalJSONObject(result)
	if err != nil {
		return ExternalActionExecution{}, fmt.Errorf("external action execution result: %w", err)
	}
	normalizedEvidence, err := normalizeEvidenceIDs(evidenceIDs)
	if err != nil {
		return ExternalActionExecution{}, err
	}
	if target == ExecutionSucceeded && len(normalizedEvidence) == 0 {
		return ExternalActionExecution{}, errors.New("successful external action execution requires evidence")
	}
	execution.State = target
	execution.Result = normalizedResult
	execution.EvidenceIDs = normalizedEvidence
	execution.FinishedAt = now.UTC()
	return execution, nil
}

func (revision ExternalActionRevision) Validate() error {
	if revision.ExternalActionID == "" || revision.Revision < 1 || revision.ProposedBy == "" || len(revision.AuthorizationSubject) == 0 {
		return errors.New("external action revision requires action id, positive revision, subject, and proposer")
	}
	canonical, err := NewCanonicalAuthorizationSubject(revision.AuthorizationSubject)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical.JSON, revision.AuthorizationSubject) || revision.AuthorizationSubjectHash != canonical.Hash {
		return errors.New("external action revision subject bytes or hash are not canonical")
	}
	return nil
}

func validExternalActionState(state ExternalActionState) bool {
	switch state {
	case ActionProposed, ActionAuthorized, ActionExecuting, ActionSucceeded, ActionFailed, ActionRejected, ActionCancelled, ActionExpired:
		return true
	default:
		return false
	}
}

func validExternalActionTransition(current, target ExternalActionState) bool {
	switch current {
	case ActionProposed:
		return target == ActionAuthorized || target == ActionRejected || target == ActionCancelled
	case ActionAuthorized:
		return target == ActionExecuting || target == ActionExpired
	case ActionExecuting:
		return target == ActionSucceeded || target == ActionFailed
	default:
		return false
	}
}

func subjectActionType(canonicalSubject json.RawMessage) string {
	var subject struct {
		ActionType string `json:"action_type"`
	}
	_ = json.Unmarshal(canonicalSubject, &subject)
	return subject.ActionType
}

func revisionConstraints(canonicalSubject json.RawMessage) json.RawMessage {
	var subject struct {
		Constraints json.RawMessage `json:"constraints"`
	}
	_ = json.Unmarshal(canonicalSubject, &subject)
	constraints, _ := canonicalJSONObject(subject.Constraints)
	return constraints
}

func canonicalJSONObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("must be a JSON object")
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, errors.New("must be a JSON object")
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(object); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})), nil
}

func normalizeEvidenceIDs(evidenceIDs []string) ([]string, error) {
	result := make([]string, 0, len(evidenceIDs))
	seen := make(map[string]struct{}, len(evidenceIDs))
	for _, evidenceID := range evidenceIDs {
		evidenceID = strings.TrimSpace(evidenceID)
		if evidenceID == "" {
			return nil, errors.New("external action execution evidence id is required")
		}
		if _, exists := seen[evidenceID]; exists {
			return nil, fmt.Errorf("external action execution repeats evidence %q", evidenceID)
		}
		seen[evidenceID] = struct{}{}
		result = append(result, evidenceID)
	}
	return result, nil
}
