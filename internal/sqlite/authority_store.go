package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dennisschroeder/workgraph/internal/domain/authority"
	"github.com/dennisschroeder/workgraph/internal/ports"
)

func (r *transactionRepository) RequiredExternalActionsSatisfied(ctx context.Context, workItemID string) (bool, error) {
	return queryBoolean(ctx, r.transaction, `
SELECT NOT EXISTS(
  SELECT 1 FROM external_actions action
  WHERE action.work_item_id = ? AND action.required = 1
    AND NOT EXISTS(
      SELECT 1 FROM external_action_executions execution
      WHERE execution.external_action_id = action.id
        AND execution.external_action_revision = action.current_revision
        AND execution.state = 'succeeded'
    )
)`, workItemID)
}

func (r *transactionRepository) CreateExternalAction(ctx context.Context, action authority.ExternalAction) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO external_actions
  (id, work_item_id, action_type, required, title, rationale, current_revision, state, version,
   created_by, updated_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		action.ID, action.WorkItemID, action.ActionType, boolInt(action.Required), action.Title, action.Rationale,
		action.CurrentRevision, action.State, action.Version, action.CreatedBy, action.UpdatedBy,
		formatTime(action.CreatedAt), formatTime(action.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert external action: %w", err)
	}
	return nil
}

func (r *transactionRepository) ExternalAction(ctx context.Context, id string) (authority.ExternalAction, error) {
	action, err := scanExternalAction(r.transaction.QueryRowContext(ctx, externalActionSelect+" WHERE id = ?", id))
	return action, mapNotFound(err)
}

func (r *transactionRepository) UpdateExternalAction(ctx context.Context, action authority.ExternalAction, expectedVersion int) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE external_actions
SET action_type = ?, title = ?, rationale = ?, current_revision = ?, state = ?, version = ?, updated_by = ?, updated_at = ?
WHERE id = ? AND version = ?`, action.ActionType, action.Title, action.Rationale, action.CurrentRevision,
		action.State, action.Version, action.UpdatedBy, formatTime(action.UpdatedAt), action.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update external action: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) CreateExternalActionRevision(ctx context.Context, revision authority.ExternalActionRevision) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO external_action_revisions
  (external_action_id, revision, authorization_subject_json, authorization_subject_hash, proposed_by, proposed_at)
VALUES (?, ?, ?, ?, ?, ?)`, revision.ExternalActionID, revision.Revision, string(revision.AuthorizationSubject),
		revision.AuthorizationSubjectHash, revision.ProposedBy, formatTime(revision.ProposedAt))
	if err != nil {
		return fmt.Errorf("insert external action revision: %w", err)
	}
	return nil
}

func (r *transactionRepository) ExternalActionRevision(ctx context.Context, actionID string, revision int) (authority.ExternalActionRevision, error) {
	result, err := scanExternalActionRevision(r.transaction.QueryRowContext(ctx, externalActionRevisionSelect+" WHERE external_action_id = ? AND revision = ?", actionID, revision))
	return result, mapNotFound(err)
}

func (r *transactionRepository) CurrentExternalActionRevision(ctx context.Context, actionID string) (authority.ExternalActionRevision, error) {
	result, err := scanExternalActionRevision(r.transaction.QueryRowContext(ctx, externalActionRevisionSelect+`
WHERE external_action_id = ? AND revision = (
  SELECT current_revision FROM external_actions WHERE id = ?
)`, actionID, actionID))
	return result, mapNotFound(err)
}

func (r *transactionRepository) CreateActionApproval(ctx context.Context, approval authority.ActionApproval) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO approvals
  (id, external_action_id, external_action_revision, approved_for_actor_id, authorization_subject_hash,
   constraints_json, expires_at, request, status, requested_by, requested_at, resolved_by, resolved_at, rationale)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, approval.ID, approval.ExternalActionID,
		approval.ExternalActionRevision, approval.ApprovedForActorID, approval.AuthorizationSubjectHash,
		string(approval.Constraints), nullableTimePtr(approval.ExpiresAt), approval.Request, approval.Status,
		approval.RequestedBy, formatTime(approval.RequestedAt), nullableString(approval.ResolvedBy),
		nullableTimePtr(approval.ResolvedAt), approval.Rationale)
	if err != nil {
		return fmt.Errorf("insert action approval: %w", err)
	}
	return nil
}

func (r *transactionRepository) ActionApproval(ctx context.Context, id string) (authority.ActionApproval, error) {
	approval, err := scanActionApproval(r.transaction.QueryRowContext(ctx, actionApprovalSelect+" WHERE id = ?", id))
	return approval, mapNotFound(err)
}

func (r *transactionRepository) UpdateActionApproval(ctx context.Context, approval authority.ActionApproval) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE approvals SET status = ?, resolved_by = ?, resolved_at = ?, rationale = ?
WHERE id = ? AND status IN ('requested', 'approved')`, approval.Status, nullableString(approval.ResolvedBy),
		nullableTimePtr(approval.ResolvedAt), approval.Rationale, approval.ID)
	if err != nil {
		return fmt.Errorf("update action approval: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) CreateAuthorityGrant(ctx context.Context, grant authority.AuthorityGrant) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO authority_grants
  (id, external_action_id, external_action_revision, principal_actor_id, authorization_subject_hash,
   constraints_json, source_approval_id, granted_by, granted_at, expires_at, revoked_by, revoked_at, revocation_reason)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, grant.ID, grant.ExternalActionID, grant.ActionRevision,
		grant.PrincipalActorID, grant.AuthorizationSubjectHash, string(grant.Constraints), grant.SourceApprovalID,
		grant.GrantedBy, formatTime(grant.GrantedAt), nullableTimePtr(grant.ExpiresAt), nullableString(grant.RevokedBy),
		nullableTimePtr(grant.RevokedAt), "")
	if err != nil {
		return fmt.Errorf("insert authority grant: %w", err)
	}
	return nil
}

func (r *transactionRepository) AuthorityGrant(ctx context.Context, id string) (authority.AuthorityGrant, error) {
	grant, err := scanAuthorityGrant(r.transaction.QueryRowContext(ctx, authorityGrantSelect+" WHERE id = ?", id))
	return grant, mapNotFound(err)
}

func (r *transactionRepository) AuthorityGrantByApproval(ctx context.Context, approvalID string) (authority.AuthorityGrant, error) {
	grant, err := scanAuthorityGrant(r.transaction.QueryRowContext(ctx, authorityGrantSelect+" WHERE source_approval_id = ?", approvalID))
	return grant, mapNotFound(err)
}

func (r *transactionRepository) UpdateAuthorityGrant(ctx context.Context, grant authority.AuthorityGrant) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE authority_grants SET revoked_by = ?, revoked_at = ?, revocation_reason = ?
WHERE id = ? AND revoked_at IS NULL`, nullableString(grant.RevokedBy), nullableTimePtr(grant.RevokedAt),
		"revoked", grant.ID)
	if err != nil {
		return fmt.Errorf("revoke authority grant: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) AuthorityGrantForPrincipal(ctx context.Context, actionID string, revision int, principalID string) (*authority.AuthorityGrant, error) {
	grant, err := scanAuthorityGrant(r.transaction.QueryRowContext(ctx, authorityGrantSelect+`
WHERE external_action_id = ? AND external_action_revision = ?
ORDER BY CASE WHEN principal_actor_id = ? THEN 0 ELSE 1 END, granted_at DESC LIMIT 1`, actionID, revision, principalID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query authority grant: %w", err)
	}
	return &grant, nil
}

func (r *transactionRepository) CreateExternalActionExecution(ctx context.Context, execution authority.ExternalActionExecution, evidenceArtifactID string) error {
	result := "{}"
	if len(execution.Result) != 0 {
		result = string(execution.Result)
	}
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO external_action_executions
  (id, external_action_id, external_action_revision, principal_actor_id, authority_grant_id, state,
   started_at, finished_at, result_json, evidence_artifact_id)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, execution.ID, execution.ExternalActionID, execution.ActionRevision,
		execution.PrincipalActorID, execution.AuthorityGrantID, executionStateForStorage(execution.State),
		formatTime(execution.StartedAt), nullableTime(execution.FinishedAt), result, nullableString(evidenceArtifactID))
	if err != nil {
		return fmt.Errorf("insert external action execution: %w", err)
	}
	return nil
}

func (r *transactionRepository) ExternalActionExecution(ctx context.Context, id string) (authority.ExternalActionExecution, error) {
	execution, err := scanExternalActionExecution(r.transaction.QueryRowContext(ctx, externalActionExecutionSelect+" WHERE id = ?", id))
	return execution, mapNotFound(err)
}

func (r *transactionRepository) UpdateExternalActionExecution(ctx context.Context, execution authority.ExternalActionExecution, evidenceArtifactID string) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE external_action_executions SET state = ?, finished_at = ?, result_json = ?, evidence_artifact_id = ?
WHERE id = ? AND state = 'executing'`, executionStateForStorage(execution.State), formatTime(execution.FinishedAt),
		string(execution.Result), nullableString(evidenceArtifactID), execution.ID)
	if err != nil {
		return fmt.Errorf("complete external action execution: %w", err)
	}
	return requireChanged(result)
}

func listExternalActionDetails(ctx context.Context, reader sqlReader, workItemID string) ([]ports.ExternalActionDetail, error) {
	rows, err := reader.QueryContext(ctx, externalActionSelect+" WHERE work_item_id = ? ORDER BY created_at, id", workItemID)
	if err != nil {
		return nil, fmt.Errorf("query external actions: %w", err)
	}
	defer rows.Close()
	var details []ports.ExternalActionDetail
	for rows.Next() {
		action, err := scanExternalAction(rows)
		if err != nil {
			return nil, err
		}
		revision, err := scanExternalActionRevision(reader.QueryRowContext(ctx, externalActionRevisionSelect+" WHERE external_action_id = ? AND revision = ?", action.ID, action.CurrentRevision))
		if err != nil {
			return nil, err
		}
		grants, err := listAuthorityGrants(ctx, reader, action.ID)
		if err != nil {
			return nil, err
		}
		executions, err := listExternalActionExecutions(ctx, reader, action.ID)
		if err != nil {
			return nil, err
		}
		details = append(details, ports.ExternalActionDetail{Action: action, Revision: revision, Grants: grants, Executions: executions})
	}
	return details, rows.Err()
}

func listAuthorityGrants(ctx context.Context, reader sqlReader, actionID string) ([]authority.AuthorityGrant, error) {
	rows, err := reader.QueryContext(ctx, authorityGrantSelect+" WHERE external_action_id = ? ORDER BY granted_at, id", actionID)
	if err != nil {
		return nil, fmt.Errorf("query authority grants: %w", err)
	}
	defer rows.Close()
	var grants []authority.AuthorityGrant
	for rows.Next() {
		grant, err := scanAuthorityGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

func listExternalActionExecutions(ctx context.Context, reader sqlReader, actionID string) ([]authority.ExternalActionExecution, error) {
	rows, err := reader.QueryContext(ctx, externalActionExecutionSelect+" WHERE external_action_id = ? ORDER BY started_at, id", actionID)
	if err != nil {
		return nil, fmt.Errorf("query external action executions: %w", err)
	}
	defer rows.Close()
	var executions []authority.ExternalActionExecution
	for rows.Next() {
		execution, err := scanExternalActionExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, execution)
	}
	return executions, rows.Err()
}

const externalActionSelect = `
SELECT id, work_item_id, action_type, required, title, rationale, current_revision, state, version,
       created_by, updated_by, created_at, updated_at
FROM external_actions`

const externalActionRevisionSelect = `
SELECT external_action_id, revision, authorization_subject_json, authorization_subject_hash, proposed_by, proposed_at
FROM external_action_revisions`

const actionApprovalSelect = `
SELECT id, external_action_id, external_action_revision, approved_for_actor_id, authorization_subject_hash,
       constraints_json, expires_at, request, status, requested_by, requested_at, resolved_by, resolved_at, rationale
FROM approvals`

const authorityGrantSelect = `
SELECT id, external_action_id, external_action_revision, principal_actor_id, authorization_subject_hash,
       constraints_json, source_approval_id, granted_by, granted_at, expires_at, revoked_by, revoked_at
FROM authority_grants`

const externalActionExecutionSelect = `
SELECT id, external_action_id, external_action_revision, principal_actor_id, authority_grant_id, state,
       started_at, finished_at, result_json, evidence_artifact_id
FROM external_action_executions`

func scanExternalAction(row scanner) (authority.ExternalAction, error) {
	var action authority.ExternalAction
	var required int
	var createdAt, updatedAt string
	if err := row.Scan(&action.ID, &action.WorkItemID, &action.ActionType, &required, &action.Title, &action.Rationale,
		&action.CurrentRevision, &action.State, &action.Version, &action.CreatedBy, &action.UpdatedBy, &createdAt, &updatedAt); err != nil {
		return authority.ExternalAction{}, err
	}
	action.Required = required == 1
	var err error
	action.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return authority.ExternalAction{}, err
	}
	action.UpdatedAt, err = parseTime(updatedAt)
	return action, err
}

func scanExternalActionRevision(row scanner) (authority.ExternalActionRevision, error) {
	var revision authority.ExternalActionRevision
	var subject, proposedAt string
	if err := row.Scan(&revision.ExternalActionID, &revision.Revision, &subject,
		&revision.AuthorizationSubjectHash, &revision.ProposedBy, &proposedAt); err != nil {
		return authority.ExternalActionRevision{}, err
	}
	revision.AuthorizationSubject = json.RawMessage(subject)
	var err error
	revision.ProposedAt, err = parseTime(proposedAt)
	return revision, err
}

func scanActionApproval(row scanner) (authority.ActionApproval, error) {
	var approval authority.ActionApproval
	var constraints string
	var expiresAt, resolvedAt, resolvedBy sql.NullString
	var requestedAt string
	if err := row.Scan(&approval.ID, &approval.ExternalActionID, &approval.ExternalActionRevision, &approval.ApprovedForActorID,
		&approval.AuthorizationSubjectHash, &constraints, &expiresAt, &approval.Request, &approval.Status,
		&approval.RequestedBy, &requestedAt, &resolvedBy, &resolvedAt, &approval.Rationale); err != nil {
		return authority.ActionApproval{}, err
	}
	approval.Constraints = json.RawMessage(constraints)
	var err error
	approval.RequestedAt, err = parseTime(requestedAt)
	if err != nil {
		return authority.ActionApproval{}, err
	}
	if expiresAt.Valid {
		value, err := parseTime(expiresAt.String)
		if err != nil {
			return authority.ActionApproval{}, err
		}
		approval.ExpiresAt = &value
	}
	if resolvedAt.Valid {
		value, err := parseTime(resolvedAt.String)
		if err != nil {
			return authority.ActionApproval{}, err
		}
		approval.ResolvedAt = &value
	}
	if resolvedBy.Valid {
		approval.ResolvedBy = resolvedBy.String
	}
	return approval, nil
}

func scanAuthorityGrant(row scanner) (authority.AuthorityGrant, error) {
	var grant authority.AuthorityGrant
	var constraints, grantedAt string
	var expiresAt, revokedAt, revokedBy sql.NullString
	if err := row.Scan(&grant.ID, &grant.ExternalActionID, &grant.ActionRevision, &grant.PrincipalActorID,
		&grant.AuthorizationSubjectHash, &constraints, &grant.SourceApprovalID, &grant.GrantedBy, &grantedAt,
		&expiresAt, &revokedBy, &revokedAt); err != nil {
		return authority.AuthorityGrant{}, err
	}
	grant.Constraints = json.RawMessage(constraints)
	var err error
	grant.GrantedAt, err = parseTime(grantedAt)
	if err != nil {
		return authority.AuthorityGrant{}, err
	}
	if expiresAt.Valid {
		value, err := parseTime(expiresAt.String)
		if err != nil {
			return authority.AuthorityGrant{}, err
		}
		grant.ExpiresAt = &value
	}
	if revokedAt.Valid {
		value, err := parseTime(revokedAt.String)
		if err != nil {
			return authority.AuthorityGrant{}, err
		}
		grant.RevokedAt = &value
	}
	if revokedBy.Valid {
		grant.RevokedBy = revokedBy.String
	}
	return grant, nil
}

func scanExternalActionExecution(row scanner) (authority.ExternalActionExecution, error) {
	var execution authority.ExternalActionExecution
	var state string
	var startedAt string
	var finishedAt sql.NullString
	var evidenceID sql.NullString
	var result string
	if err := row.Scan(&execution.ID, &execution.ExternalActionID, &execution.ActionRevision, &execution.PrincipalActorID,
		&execution.AuthorityGrantID, &state, &startedAt, &finishedAt, &result, &evidenceID); err != nil {
		return authority.ExternalActionExecution{}, err
	}
	execution.Result = json.RawMessage(result)
	if state == "executing" {
		execution.State = authority.ExecutionStarted
	} else {
		execution.State = authority.ExecutionState(state)
	}
	var err error
	execution.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return authority.ExternalActionExecution{}, err
	}
	if finishedAt.Valid {
		execution.FinishedAt, err = parseTime(finishedAt.String)
		if err != nil {
			return authority.ExternalActionExecution{}, err
		}
	}
	if evidenceID.Valid {
		execution.EvidenceIDs = []string{evidenceID.String}
	}
	return execution, nil
}

func executionStateForStorage(state authority.ExecutionState) string {
	if state == authority.ExecutionStarted {
		return "executing"
	}
	return string(state)
}

func nullableTimePtr(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return formatTime(*value)
}
