package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dennisschroeder/workgraph/internal/domain/output"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
	"github.com/dennisschroeder/workgraph/internal/ports"
)

func (r *transactionRepository) HasApprovedPlan(ctx context.Context, objectiveID string) (bool, error) {
	var exists int
	if err := r.transaction.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM plans WHERE objective_id = ? AND commitment_state = ?
)`, objectiveID, work.PlanApproved).Scan(&exists); err != nil {
		return false, fmt.Errorf("query approved plan: %w", err)
	}
	return exists == 1, nil
}

func (r *transactionRepository) UpdateObjective(ctx context.Context, objective work.Objective, expectedVersion int) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE objectives
SET title = ?, description = ?, desired_outcome = ?, phase = ?, prior_phase = ?, updated_by = ?, version = ?, updated_at = ?
WHERE id = ? AND version = ?`,
		objective.Title, objective.Description, objective.DesiredOutcome, objective.Phase, nullableString(string(objective.PriorPhase)), nullableString(objective.UpdatedBy),
		objective.Version, formatTime(objective.UpdatedAt), objective.ID, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update objective phase: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) CreateContextRecord(ctx context.Context, record work.ContextRecord) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO context_records
  (id, objective_id, work_item_id, kind, title, body, status, confidence, source_uri,
   supersedes_id, version, created_at, updated_at, created_by, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ObjectiveID, nullableString(record.WorkItemID), record.Kind, record.Title,
		record.Body, record.Status, record.Confidence, record.SourceURI, nullableString(record.SupersedesID),
		record.Version, formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.CreatedBy,
		nullableString(record.UpdatedBy),
	)
	if err != nil {
		return fmt.Errorf("insert context record: %w", err)
	}
	return nil
}

func (r *transactionRepository) ContextRecord(ctx context.Context, id string) (work.ContextRecord, error) {
	record, err := scanContextRecord(r.transaction.QueryRowContext(ctx, contextRecordSelect+" WHERE id = ?", id))
	return record, mapNotFound(err)
}

func (r *transactionRepository) UpdateContextRecord(ctx context.Context, record work.ContextRecord, expectedVersion int) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE context_records
SET status = ?, version = ?, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?`,
		record.Status, record.Version, formatTime(record.UpdatedAt), nullableString(record.UpdatedBy),
		record.ID, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update context record: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) CreateQuestion(ctx context.Context, question work.Question) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO questions
  (id, objective_id, work_item_id, question, status, answer, requires_human_attention,
   version, created_by, resolved_by, created_at, resolved_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		question.ID, question.ObjectiveID, nullableString(question.WorkItemID), question.Text,
		question.Status, question.Answer, boolInt(question.RequiresHumanAttention), question.Version,
		question.CreatedBy, nullableString(question.ResolvedBy), formatTime(question.CreatedAt), nullableTime(question.ResolvedAt),
	)
	if err != nil {
		return fmt.Errorf("insert question: %w", err)
	}
	return nil
}

func (r *transactionRepository) Question(ctx context.Context, id string) (work.Question, error) {
	question, err := scanQuestion(r.transaction.QueryRowContext(ctx, questionSelect+" WHERE id = ?", id))
	return question, mapNotFound(err)
}

func (r *transactionRepository) UpdateQuestion(ctx context.Context, question work.Question, expectedVersion int) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE questions
SET status = ?, answer = ?, version = ?, resolved_by = ?, resolved_at = ?
WHERE id = ? AND version = ?`,
		question.Status, question.Answer, question.Version, nullableString(question.ResolvedBy),
		nullableTime(question.ResolvedAt), question.ID, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update question: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) CreateDecision(ctx context.Context, decision work.Decision) error {
	alternatives, err := json.Marshal(decision.Alternatives)
	if err != nil {
		return fmt.Errorf("encode decision alternatives: %w", err)
	}
	_, err = r.transaction.ExecContext(ctx, `
INSERT INTO decisions
  (id, objective_id, work_item_id, title, decision, rationale, alternatives_json, status,
   supersedes_id, decided_by, decided_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		decision.ID, decision.ObjectiveID, nullableString(decision.WorkItemID), decision.Title,
		decision.Outcome, decision.Rationale, string(alternatives), decision.Status,
		nullableString(decision.SupersedesID), decision.DecidedBy, formatTime(decision.DecidedAt),
		formatTime(decision.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert decision: %w", err)
	}
	return nil
}

func (r *transactionRepository) Decision(ctx context.Context, id string) (work.Decision, error) {
	decision, err := scanDecision(r.transaction.QueryRowContext(ctx, decisionSelect+" WHERE id = ?", id))
	return decision, mapNotFound(err)
}

func (r *transactionRepository) UpdateDecision(ctx context.Context, decision work.Decision) error {
	result, err := r.transaction.ExecContext(ctx,
		"UPDATE decisions SET status = ? WHERE id = ? AND status = ?",
		decision.Status, decision.ID, work.DecisionAccepted,
	)
	if err != nil {
		return fmt.Errorf("supersede decision: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) UpdatePlan(ctx context.Context, plan work.Plan, expectedVersion int) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE plans
SET commitment_state = ?, resolved_by = ?, resolved_at = ?, resolution_reason = ?,
    version = ?, updated_at = ?
WHERE id = ? AND version = ?`,
		plan.CommitmentState, nullableString(plan.ResolvedBy), nullableTime(plan.ResolvedAt),
		plan.ResolutionReason, plan.Version, formatTime(plan.UpdatedAt), plan.ID, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update plan review: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) LatestApprovedPlanRevision(ctx context.Context, objectiveID string) (int, error) {
	var revision int
	if err := r.transaction.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(revision), 0) FROM plans WHERE objective_id = ? AND commitment_state = ?",
		objectiveID, work.PlanApproved,
	).Scan(&revision); err != nil {
		return 0, fmt.Errorf("query latest approved plan revision: %w", err)
	}
	return revision, nil
}

func (r *transactionRepository) LatestPlanRevision(ctx context.Context, objectiveID string) (int, error) {
	var revision int
	if err := r.transaction.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(revision), 0) FROM plans WHERE objective_id = ?", objectiveID,
	).Scan(&revision); err != nil {
		return 0, fmt.Errorf("query latest plan revision: %w", err)
	}
	return revision, nil
}

func (r *transactionRepository) SupersedeEarlierPlans(ctx context.Context, objectiveID string, revision int, updatedAt time.Time) error {
	if _, err := r.transaction.ExecContext(ctx, `
UPDATE work_items
SET commitment_state = ?, version = version + 1, updated_at = ?
WHERE plan_id IN (
  SELECT id FROM plans WHERE objective_id = ? AND revision < ? AND commitment_state = ?
) AND commitment_state = ?`,
		work.ItemSuperseded, formatTime(updatedAt), objectiveID, revision, work.PlanApproved, work.ItemAccepted,
	); err != nil {
		return fmt.Errorf("supersede earlier plan work items: %w", err)
	}
	if _, err := r.transaction.ExecContext(ctx, `
UPDATE plans
SET commitment_state = ?, version = version + 1, updated_at = ?
WHERE objective_id = ? AND revision < ? AND commitment_state = ?`,
		work.PlanSuperseded, formatTime(updatedAt), objectiveID, revision, work.PlanApproved,
	); err != nil {
		return fmt.Errorf("supersede earlier plans: %w", err)
	}
	return nil
}

func (r *transactionRepository) SetPlanItemsCommitment(ctx context.Context, planID string, state work.ItemCommitment, updatedAt time.Time) error {
	_, err := r.transaction.ExecContext(ctx, `
UPDATE work_items
SET commitment_state = ?, version = version + 1, updated_at = ?
WHERE plan_id = ? AND commitment_state = ?`, state, formatTime(updatedAt), planID, work.ItemProposed)
	if err != nil {
		return fmt.Errorf("commit plan work items: %w", err)
	}
	return nil
}

func (r *transactionRepository) CreateApproval(ctx context.Context, approval work.Approval) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO approvals
  (id, objective_id, plan_id, work_item_id, output_profile_id, output_revision_id,
   request, status, version, requested_by, requested_at, resolved_by, resolved_at, rationale)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		approval.ID, nullableString(approval.ObjectiveID), nullableString(approval.PlanID), nullableString(approval.WorkItemID), nullableString(approval.OutputProfileID), nullableString(approval.OutputRevisionID),
		approval.Request, approval.Status, approval.Version, approval.RequestedBy, formatTime(approval.RequestedAt), nullableString(approval.ResolvedBy), nullableTime(approval.ResolvedAt), approval.Rationale,
	)
	if err != nil {
		return fmt.Errorf("insert approval: %w", err)
	}
	return nil
}

func (r *transactionRepository) Approval(ctx context.Context, id string) (work.Approval, error) {
	approval, err := scanApproval(r.transaction.QueryRowContext(ctx, approvalSelect+" WHERE id = ?", id))
	return approval, mapNotFound(err)
}

func (r *transactionRepository) UpdateApproval(ctx context.Context, approval work.Approval, expectedVersion int) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE approvals
SET status = ?, version = ?, resolved_by = ?, resolved_at = ?, rationale = ?
WHERE id = ? AND version = ?`, approval.Status, approval.Version, nullableString(approval.ResolvedBy), nullableTime(approval.ResolvedAt), approval.Rationale, approval.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update approval: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) AddWorkItemCapability(ctx context.Context, workItemID, capability string) error {
	if _, err := r.transaction.ExecContext(ctx, "INSERT OR IGNORE INTO capabilities(slug) VALUES (?)", capability); err != nil {
		return fmt.Errorf("insert capability: %w", err)
	}
	if _, err := r.transaction.ExecContext(ctx,
		"INSERT INTO work_item_capabilities(work_item_id, capability_slug) VALUES (?, ?)",
		workItemID, capability,
	); err != nil {
		return fmt.Errorf("link work item capability: %w", err)
	}
	return nil
}

func (r *transactionRepository) OutputProfileByID(ctx context.Context, id string) (output.Profile, error) {
	profile, err := scanProfile(r.transaction.QueryRowContext(ctx, profileSelect+" WHERE id = ?", id))
	return profile, mapNotFound(err)
}

func (r *transactionRepository) LatestOutputProfileVersion(ctx context.Context, name string) (int, error) {
	var version int
	if err := r.transaction.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM output_profiles WHERE name = ?", name,
	).Scan(&version); err != nil {
		return 0, fmt.Errorf("query latest output profile version: %w", err)
	}
	return version, nil
}

func (r *transactionRepository) CreateOutputProfile(ctx context.Context, profile output.Profile) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO output_profiles
  (id, name, version, state_version, description, lifecycle_state, structure_json, semantics_json,
   validation_json, built_in, supersedes_id, proposed_by, proposed_at, resolved_by,
   resolved_at, resolution_reason, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		profile.ID, profile.Name, profile.Version, profile.StateVersion, profile.Description, profile.LifecycleState,
		string(profile.Structure), string(profile.Semantics), string(profile.Validation), boolInt(profile.BuiltIn),
		nullableString(profile.SupersedesID), nullableString(profile.ProposedBy), nullableTime(profile.ProposedAt),
		nullableString(profile.ResolvedBy), nullableTime(profile.ResolvedAt), profile.ResolutionReason,
		formatTime(profile.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert output profile: %w", err)
	}
	return nil
}

func (r *transactionRepository) UpdateOutputProfile(ctx context.Context, profile output.Profile) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE output_profiles
SET lifecycle_state = ?, state_version = ?, resolved_by = ?, resolved_at = ?, resolution_reason = ?
WHERE id = ? AND lifecycle_state = ? AND state_version = ?`,
		profile.LifecycleState, profile.StateVersion, nullableString(profile.ResolvedBy), nullableTime(profile.ResolvedAt),
		profile.ResolutionReason, profile.ID, output.ProfileProposed, profile.StateVersion-1,
	)
	if err != nil {
		return fmt.Errorf("update output profile review: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) SupersedeOutputProfile(ctx context.Context, id string) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE output_profiles
SET lifecycle_state = ?, state_version = state_version + 1
WHERE id = ? AND lifecycle_state = ?`, output.ProfileSuperseded, id, output.ProfileActive)
	if err != nil {
		return fmt.Errorf("supersede output profile: %w", err)
	}
	return requireChanged(result)
}

func requireChanged(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}
	if changed != 1 {
		return ports.ErrVersionConflict
	}
	return nil
}

func (s *Store) GetObjectiveContext(ctx context.Context, id string) (ports.ObjectiveContext, error) {
	var result ports.ObjectiveContext
	err := s.withinReadTransaction(ctx, func(reader sqlReader) error {
		var err error
		result, err = s.getObjectiveContext(ctx, reader, id)
		return err
	})
	if err != nil {
		return ports.ObjectiveContext{}, err
	}
	return result, nil
}

func (s *Store) getObjectiveContext(ctx context.Context, reader sqlReader, id string) (ports.ObjectiveContext, error) {
	objective, err := scanObjective(reader.QueryRowContext(ctx, objectiveSelect+" WHERE id = ?", id))
	if err != nil {
		return ports.ObjectiveContext{}, mapNotFound(err)
	}
	result := ports.ObjectiveContext{Objective: objective}
	if result.ContextRecords, err = s.listContextRecords(ctx, reader, id); err != nil {
		return ports.ObjectiveContext{}, err
	}
	if result.Plans, err = s.listPlanContexts(ctx, reader, id); err != nil {
		return ports.ObjectiveContext{}, err
	}
	if result.Questions, err = s.listQuestions(ctx, reader, id); err != nil {
		return ports.ObjectiveContext{}, err
	}
	if result.Decisions, err = s.listDecisions(ctx, reader, id); err != nil {
		return ports.ObjectiveContext{}, err
	}
	if result.Approvals, err = s.listApprovals(ctx, reader, id); err != nil {
		return ports.ObjectiveContext{}, err
	}
	return result, nil
}

func (s *Store) ListOutputProfiles(ctx context.Context) ([]output.Profile, error) {
	rows, err := s.db.QueryContext(ctx, profileSelect+" ORDER BY name, version")
	if err != nil {
		return nil, fmt.Errorf("query output profiles: %w", err)
	}
	defer rows.Close()
	var profiles []output.Profile
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (s *Store) listContextRecords(ctx context.Context, reader sqlReader, objectiveID string) ([]work.ContextRecord, error) {
	rows, err := reader.QueryContext(ctx, contextRecordSelect+" WHERE objective_id = ? ORDER BY created_at, id", objectiveID)
	if err != nil {
		return nil, fmt.Errorf("query context records: %w", err)
	}
	defer rows.Close()
	var records []work.ContextRecord
	for rows.Next() {
		record, err := scanContextRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) listPlanContexts(ctx context.Context, reader sqlReader, objectiveID string) ([]ports.PlanContext, error) {
	rows, err := reader.QueryContext(ctx, planSelect+" WHERE objective_id = ? ORDER BY revision", objectiveID)
	if err != nil {
		return nil, fmt.Errorf("query plans: %w", err)
	}
	var plans []work.Plan
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var result []ports.PlanContext
	for _, plan := range plans {
		items, err := s.listPlannedItems(ctx, reader, plan.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, ports.PlanContext{Plan: plan, Items: items})
	}
	return result, nil
}

func (s *Store) listPlannedItems(ctx context.Context, reader sqlReader, planID string) ([]ports.PlannedWorkItem, error) {
	rows, err := reader.QueryContext(ctx, workItemSelect+" WHERE plan_id = ? ORDER BY created_at, id", planID)
	if err != nil {
		return nil, fmt.Errorf("query planned work items: %w", err)
	}
	var workItems []work.WorkItem
	for rows.Next() {
		item, err := scanWorkItem(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		workItems = append(workItems, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var result []ports.PlannedWorkItem
	for _, item := range workItems {
		capabilities, err := s.listCapabilities(ctx, reader, item.ID)
		if err != nil {
			return nil, err
		}
		expected, err := s.listExpectedOutputs(ctx, reader, item.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, ports.PlannedWorkItem{WorkItem: item, RequiredCapabilities: capabilities, ExpectedOutputs: expected})
	}
	return result, nil
}

func (s *Store) listCapabilities(ctx context.Context, reader sqlReader, workItemID string) ([]string, error) {
	rows, err := reader.QueryContext(ctx, "SELECT capability_slug FROM work_item_capabilities WHERE work_item_id = ? ORDER BY capability_slug", workItemID)
	if err != nil {
		return nil, fmt.Errorf("query work item capabilities: %w", err)
	}
	defer rows.Close()
	var capabilities []string
	for rows.Next() {
		var capability string
		if err := rows.Scan(&capability); err != nil {
			return nil, err
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities, rows.Err()
}

func (s *Store) listExpectedOutputs(ctx context.Context, reader sqlReader, workItemID string) ([]output.ExpectedOutputDetail, error) {
	rows, err := reader.QueryContext(ctx, `
SELECT
  e.id, e.work_item_id, e.name, e.output_profile_id, e.contract_json,
  e.destination_hint, e.required, e.ordinal,
  p.id, p.name, p.version, p.state_version, p.description, p.lifecycle_state,
  p.structure_json, p.semantics_json, p.validation_json, p.built_in,
  p.supersedes_id, p.proposed_by, p.proposed_at, p.resolved_by, p.resolved_at,
  p.resolution_reason, p.created_at
FROM expected_outputs e
JOIN output_profiles p ON p.id = e.output_profile_id
WHERE e.work_item_id = ?
ORDER BY e.ordinal`, workItemID)
	if err != nil {
		return nil, fmt.Errorf("query expected outputs: %w", err)
	}
	defer rows.Close()
	var result []output.ExpectedOutputDetail
	for rows.Next() {
		detail, err := scanExpectedOutputDetail(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, detail)
	}
	return result, rows.Err()
}

func scanExpectedOutputDetail(row scanner) (output.ExpectedOutputDetail, error) {
	var detail output.ExpectedOutputDetail
	var contract, structure, semantics, validation, profileCreatedAt string
	var required, builtIn int
	var supersedesID, proposedBy, proposedAt, resolvedBy, resolvedAt sql.NullString
	if err := row.Scan(
		&detail.ExpectedOutput.ID, &detail.ExpectedOutput.WorkItemID, &detail.ExpectedOutput.Name,
		&detail.ExpectedOutput.OutputProfileID, &contract, &detail.ExpectedOutput.DestinationHint,
		&required, &detail.ExpectedOutput.Ordinal, &detail.Profile.ID, &detail.Profile.Name,
		&detail.Profile.Version, &detail.Profile.StateVersion, &detail.Profile.Description, &detail.Profile.LifecycleState,
		&structure, &semantics, &validation, &builtIn, &supersedesID, &proposedBy,
		&proposedAt, &resolvedBy, &resolvedAt, &detail.Profile.ResolutionReason, &profileCreatedAt,
	); err != nil {
		return output.ExpectedOutputDetail{}, fmt.Errorf("scan expected output: %w", err)
	}
	detail.ExpectedOutput.Contract = json.RawMessage(contract)
	detail.ExpectedOutput.Required = required == 1
	detail.Profile.Structure = json.RawMessage(structure)
	detail.Profile.Semantics = json.RawMessage(semantics)
	detail.Profile.Validation = json.RawMessage(validation)
	detail.Profile.BuiltIn = builtIn == 1
	detail.Profile.SupersedesID = supersedesID.String
	detail.Profile.ProposedBy = proposedBy.String
	detail.Profile.ResolvedBy = resolvedBy.String
	var err error
	if proposedAt.Valid {
		detail.Profile.ProposedAt, err = parseTime(proposedAt.String)
		if err != nil {
			return output.ExpectedOutputDetail{}, err
		}
	}
	if resolvedAt.Valid {
		detail.Profile.ResolvedAt, err = parseTime(resolvedAt.String)
		if err != nil {
			return output.ExpectedOutputDetail{}, err
		}
	}
	detail.Profile.CreatedAt, err = parseTime(profileCreatedAt)
	return detail, err
}

func (s *Store) listQuestions(ctx context.Context, reader sqlReader, objectiveID string) ([]work.Question, error) {
	rows, err := reader.QueryContext(ctx, questionSelect+" WHERE objective_id = ? ORDER BY created_at, id", objectiveID)
	if err != nil {
		return nil, fmt.Errorf("query questions: %w", err)
	}
	defer rows.Close()
	var result []work.Question
	for rows.Next() {
		question, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, question)
	}
	return result, rows.Err()
}

func (s *Store) listDecisions(ctx context.Context, reader sqlReader, objectiveID string) ([]work.Decision, error) {
	rows, err := reader.QueryContext(ctx, decisionSelect+" WHERE objective_id = ? ORDER BY created_at, id", objectiveID)
	if err != nil {
		return nil, fmt.Errorf("query decisions: %w", err)
	}
	defer rows.Close()
	var result []work.Decision
	for rows.Next() {
		decision, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, decision)
	}
	return result, rows.Err()
}

func (s *Store) listApprovals(ctx context.Context, reader sqlReader, objectiveID string) ([]work.Approval, error) {
	rows, err := reader.QueryContext(ctx, approvalSelect+" WHERE objective_id = ? ORDER BY resolved_at, id", objectiveID)
	if err != nil {
		return nil, fmt.Errorf("query approvals: %w", err)
	}
	defer rows.Close()
	var result []work.Approval
	for rows.Next() {
		approval, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, approval)
	}
	return result, rows.Err()
}

const contextRecordSelect = `
SELECT id, objective_id, work_item_id, kind, title, body, status, confidence, source_uri,
       supersedes_id, version, created_at, updated_at, created_by, updated_by
FROM context_records`

const questionSelect = `
SELECT id, objective_id, work_item_id, question, status, answer, requires_human_attention,
       version, created_by, resolved_by, created_at, resolved_at
FROM questions`

const decisionSelect = `
SELECT id, objective_id, work_item_id, title, decision, rationale, alternatives_json, status,
       supersedes_id, decided_by, decided_at, created_at
FROM decisions`

const approvalSelect = `
SELECT id, objective_id, plan_id, work_item_id, output_profile_id, output_revision_id,
       request, status, version, requested_by, requested_at, resolved_by, resolved_at, rationale
FROM approvals`

func scanContextRecord(row scanner) (work.ContextRecord, error) {
	var record work.ContextRecord
	var workItemID, supersedesID, updatedBy sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&record.ID, &record.ObjectiveID, &workItemID, &record.Kind, &record.Title, &record.Body,
		&record.Status, &record.Confidence, &record.SourceURI, &supersedesID, &record.Version,
		&createdAt, &updatedAt, &record.CreatedBy, &updatedBy,
	); err != nil {
		return work.ContextRecord{}, err
	}
	record.WorkItemID = workItemID.String
	record.SupersedesID = supersedesID.String
	record.UpdatedBy = updatedBy.String
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return work.ContextRecord{}, err
	}
	record.UpdatedAt, err = parseTime(updatedAt)
	return record, err
}

func scanQuestion(row scanner) (work.Question, error) {
	var question work.Question
	var workItemID, resolvedBy, resolvedAt sql.NullString
	var attention int
	var createdAt string
	if err := row.Scan(
		&question.ID, &question.ObjectiveID, &workItemID, &question.Text, &question.Status,
		&question.Answer, &attention, &question.Version, &question.CreatedBy, &resolvedBy,
		&createdAt, &resolvedAt,
	); err != nil {
		return work.Question{}, err
	}
	question.WorkItemID = workItemID.String
	question.RequiresHumanAttention = attention == 1
	question.ResolvedBy = resolvedBy.String
	var err error
	question.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return work.Question{}, err
	}
	if resolvedAt.Valid {
		question.ResolvedAt, err = parseTime(resolvedAt.String)
	}
	return question, err
}

func scanDecision(row scanner) (work.Decision, error) {
	var decision work.Decision
	var workItemID, supersedesID sql.NullString
	var alternatives, decidedAt, createdAt string
	if err := row.Scan(
		&decision.ID, &decision.ObjectiveID, &workItemID, &decision.Title, &decision.Outcome,
		&decision.Rationale, &alternatives, &decision.Status, &supersedesID, &decision.DecidedBy,
		&decidedAt, &createdAt,
	); err != nil {
		return work.Decision{}, err
	}
	decision.WorkItemID = workItemID.String
	decision.SupersedesID = supersedesID.String
	if err := json.Unmarshal([]byte(alternatives), &decision.Alternatives); err != nil {
		return work.Decision{}, fmt.Errorf("decode decision alternatives: %w", err)
	}
	var err error
	decision.DecidedAt, err = parseTime(decidedAt)
	if err != nil {
		return work.Decision{}, err
	}
	decision.CreatedAt, err = parseTime(createdAt)
	return decision, err
}

func scanApproval(row scanner) (work.Approval, error) {
	var approval work.Approval
	var requestedAt string
	var objectiveID, planID, workItemID, profileID, revisionID, resolvedBy, resolvedAt sql.NullString
	if err := row.Scan(
		&approval.ID, &objectiveID, &planID, &workItemID, &profileID, &revisionID,
		&approval.Request, &approval.Status, &approval.Version, &approval.RequestedBy, &requestedAt, &resolvedBy, &resolvedAt, &approval.Rationale,
	); err != nil {
		return work.Approval{}, err
	}
	approval.ObjectiveID = objectiveID.String
	approval.PlanID = planID.String
	approval.WorkItemID = workItemID.String
	approval.OutputProfileID = profileID.String
	approval.OutputRevisionID = revisionID.String
	approval.ResolvedBy = resolvedBy.String
	var err error
	approval.RequestedAt, err = parseTime(requestedAt)
	if err != nil {
		return work.Approval{}, err
	}
	if resolvedAt.Valid {
		approval.ResolvedAt, err = parseTime(resolvedAt.String)
	}
	return approval, err
}
