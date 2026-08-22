package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dennisschroeder/workgraph/internal/domain/output"
	"github.com/dennisschroeder/workgraph/internal/domain/work"
	"github.com/dennisschroeder/workgraph/internal/ports"
)

func (r *transactionRepository) UpdateWorkItem(ctx context.Context, item work.WorkItem, expectedVersion int) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE work_items SET execution_status = ?, version = ?, updated_at = ?
WHERE id = ? AND version = ?`, item.ExecutionStatus, item.Version, formatTime(item.UpdatedAt), item.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update work item: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) CreateAcceptanceCriterion(ctx context.Context, criterion work.AcceptanceCriterion) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO acceptance_criteria
  (id, work_item_id, ordinal, text, required, status, resolved_at, resolved_by, resolution_rationale)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, criterion.ID, criterion.WorkItemID, criterion.Ordinal, criterion.Text,
		boolInt(criterion.Required), criterion.Status, nullableTime(criterion.ResolvedAt), nullableString(criterion.ResolvedBy), criterion.ResolutionRationale)
	if err != nil {
		return fmt.Errorf("insert acceptance criterion: %w", err)
	}
	return nil
}

func (r *transactionRepository) AcceptanceCriterion(ctx context.Context, id string) (work.AcceptanceCriterion, error) {
	criterion, err := scanAcceptanceCriterion(r.transaction.QueryRowContext(ctx, acceptanceCriterionSelect+" WHERE id = ?", id))
	return criterion, mapNotFound(err)
}

func (r *transactionRepository) UpdateAcceptanceCriterion(ctx context.Context, criterion work.AcceptanceCriterion) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE acceptance_criteria
SET status = ?, resolved_at = ?, resolved_by = ?, resolution_rationale = ?
WHERE id = ? AND status = ?`, criterion.Status, formatTime(criterion.ResolvedAt), criterion.ResolvedBy,
		criterion.ResolutionRationale, criterion.ID, work.AcceptancePending)
	if err != nil {
		return fmt.Errorf("resolve acceptance criterion: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) AcceptanceCriteriaSatisfied(ctx context.Context, workItemID string) (bool, error) {
	return queryBoolean(ctx, r.transaction, `
SELECT NOT EXISTS(
  SELECT 1 FROM acceptance_criteria
  WHERE work_item_id = ? AND required = 1 AND status = 'pending'
)`, workItemID)
}

func (r *transactionRepository) CreateDependency(ctx context.Context, dependency work.Dependency) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO dependencies (id, work_item_id, depends_on_item_id, kind, note, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?)`, dependency.ID, dependency.WorkItemID, dependency.DependsOnItemID,
		dependency.Kind, dependency.Note, formatTime(dependency.CreatedAt), dependency.CreatedBy)
	if err != nil {
		return fmt.Errorf("insert dependency: %w", err)
	}
	return nil
}

func (r *transactionRepository) DependencyCreatesCycle(ctx context.Context, workItemID, dependsOnItemID string) (bool, error) {
	return queryBoolean(ctx, r.transaction, `
WITH RECURSIVE reachable(id) AS (
  SELECT depends_on_item_id FROM dependencies WHERE work_item_id = ? AND kind = 'hard'
  UNION
  SELECT dependency.depends_on_item_id
  FROM dependencies dependency JOIN reachable ON dependency.work_item_id = reachable.id
  WHERE dependency.kind = 'hard'
)
SELECT EXISTS(SELECT 1 FROM reachable WHERE id = ?)`, dependsOnItemID, workItemID)
}

func (r *transactionRepository) HardDependenciesSatisfied(ctx context.Context, workItemID string) (bool, error) {
	return queryBoolean(ctx, r.transaction, `
SELECT NOT EXISTS(
  SELECT 1
  FROM dependencies dependency
  JOIN work_items prerequisite ON prerequisite.id = dependency.depends_on_item_id
  WHERE dependency.work_item_id = ? AND dependency.kind = 'hard' AND prerequisite.execution_status <> 'done'
)`, workItemID)
}

func (r *transactionRepository) CreateActivity(ctx context.Context, activity work.Activity) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO activity
  (id, entity_kind, entity_id, work_item_id, actor_id, event_type, summary, payload_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, activity.ID, activity.EntityKind, activity.EntityID,
		nullableString(activity.WorkItemID), activity.ActorID, activity.EventType, activity.Summary,
		string(activity.PayloadJSON), formatTime(activity.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert activity: %w", err)
	}
	return nil
}

func (r *transactionRepository) ExpectedOutput(ctx context.Context, id string) (output.ExpectedOutput, error) {
	expected, err := scanExpectedOutput(r.transaction.QueryRowContext(ctx, expectedOutputSelect+" WHERE id = ?", id))
	return expected, mapNotFound(err)
}

func (r *transactionRepository) NextOutputRevision(ctx context.Context, expectedOutputID string) (int, error) {
	var next int
	if err := r.transaction.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(revision), 0) + 1 FROM output_revisions WHERE expected_output_id = ?", expectedOutputID,
	).Scan(&next); err != nil {
		return 0, fmt.Errorf("query next output revision: %w", err)
	}
	return next, nil
}

func (r *transactionRepository) CreateArtifact(ctx context.Context, artifact output.Artifact) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO artifacts (id, work_item_id, kind, uri, title, metadata_json, attached_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, artifact.ID, artifact.WorkItemID, artifact.Kind, artifact.URI,
		artifact.Title, string(artifact.Metadata), artifact.AttachedBy, formatTime(artifact.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert artifact: %w", err)
	}
	return nil
}

func (r *transactionRepository) ArtifactByURI(ctx context.Context, workItemID, uri string) (output.Artifact, error) {
	artifact, err := scanArtifact(r.transaction.QueryRowContext(ctx,
		artifactSelect+" WHERE work_item_id = ? AND uri = ?", workItemID, uri))
	return artifact, mapNotFound(err)
}

func (r *transactionRepository) CreateOutputRevision(ctx context.Context, revision output.OutputRevision) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO output_revisions
  (id, expected_output_id, output_profile_id, revision, content_digest, acceptance_state,
   produced_by, produced_at, accepted_by, accepted_at, acceptance_reason)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, revision.ID, revision.ExpectedOutputID,
		revision.OutputProfileID, revision.Revision, revision.ContentDigest, revision.AcceptanceState,
		revision.ProducedBy, formatTime(revision.ProducedAt), nullableString(revision.AcceptedBy),
		nullableTime(revision.AcceptedAt), revision.AcceptanceReason)
	if err != nil {
		return fmt.Errorf("insert output revision: %w", err)
	}
	for _, binding := range revision.Artifacts {
		if _, err := r.transaction.ExecContext(ctx, `
INSERT INTO output_revision_artifacts (output_revision_id, artifact_id, role)
VALUES (?, ?, ?)`, revision.ID, binding.ArtifactID, binding.Role); err != nil {
			return fmt.Errorf("bind output revision artifact: %w", err)
		}
	}
	result, err := r.transaction.ExecContext(ctx,
		"UPDATE output_revisions SET bindings_finalized = 1 WHERE id = ? AND bindings_finalized = 0", revision.ID)
	if err != nil {
		return fmt.Errorf("finalize output revision artifacts: %w", err)
	}
	if err := requireChanged(result); err != nil {
		return fmt.Errorf("finalize output revision artifacts: %w", err)
	}
	return nil
}

func (r *transactionRepository) OutputRevision(ctx context.Context, id string) (output.OutputRevision, error) {
	revision, err := scanOutputRevision(r.transaction.QueryRowContext(ctx, outputRevisionSelect+" WHERE id = ?", id))
	if err != nil {
		return output.OutputRevision{}, mapNotFound(err)
	}
	revision.Artifacts, err = listRevisionArtifactBindings(ctx, r.transaction, revision.ID)
	return revision, err
}

func (r *transactionRepository) UpdateOutputRevisionAcceptance(ctx context.Context, revision output.OutputRevision) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE output_revisions
SET acceptance_state = ?, accepted_by = ?, accepted_at = ?, acceptance_reason = ?
WHERE id = ? AND acceptance_state = ?`, revision.AcceptanceState, revision.AcceptedBy,
		formatTime(revision.AcceptedAt), revision.AcceptanceReason, revision.ID, output.RevisionProduced)
	if err != nil {
		return fmt.Errorf("accept output revision: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) CreateValidationRecord(ctx context.Context, record output.ValidationRecord) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO output_validations
  (id, output_revision_id, criterion_ref, validator_kind, verdict, score, verifier_actor_id,
   evidence_artifact_id, details_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ID, record.OutputRevisionID, record.CriterionRef,
		record.ValidatorKind, record.Verdict, nullableFloat(record.Score), record.VerifierActorID,
		nullableString(record.EvidenceArtifactID), string(record.Details), formatTime(record.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert validation record: %w", err)
	}
	return nil
}

func (r *transactionRepository) ValidationRecords(ctx context.Context, outputRevisionID string) ([]output.ValidationRecord, error) {
	return listValidationRecords(ctx, r.transaction, outputRevisionID)
}

func (r *transactionRepository) CreateOutputRequirement(ctx context.Context, requirement output.OutputRequirement) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO output_requirements
  (id, work_item_id, required_output_revision_id, required_profile_name, version_constraint, required, note)
VALUES (?, ?, ?, ?, ?, ?, ?)`, requirement.ID, requirement.WorkItemID,
		nullableString(requirement.RequiredOutputRevisionID), nullableString(requirement.RequiredProfileName),
		nullableString(requirement.VersionConstraint), boolInt(requirement.Required), requirement.Note)
	if err != nil {
		return fmt.Errorf("insert output requirement: %w", err)
	}
	return nil
}

func (r *transactionRepository) OutputRequirementsSatisfied(ctx context.Context, workItemID string) (bool, error) {
	return queryBoolean(ctx, r.transaction, "SELECT "+outputRequirementsSatisfiedSQL, workItemID)
}

func (r *transactionRepository) ExpectedOutputsSatisfied(ctx context.Context, workItemID string) (bool, error) {
	return queryBoolean(ctx, r.transaction, `
SELECT NOT EXISTS(
  SELECT 1 FROM expected_outputs expected
  WHERE expected.work_item_id = ? AND expected.required = 1
    AND NOT EXISTS(
      SELECT 1 FROM output_revisions revision
      WHERE revision.expected_output_id = expected.id AND revision.acceptance_state = 'accepted'
    )
)`, workItemID)
}

func (s *Store) ListReadyWork(ctx context.Context) ([]ports.ReadyWorkItem, error) {
	return s.listReadyWork(ctx, "")
}

func (s *Store) ListReadyWorkForActor(ctx context.Context, actorID string) ([]ports.ReadyWorkItem, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, errors.New("ready work actor is required")
	}
	return s.listReadyWork(ctx, actorID)
}

func (s *Store) listReadyWork(ctx context.Context, actorID string) ([]ports.ReadyWorkItem, error) {
	arguments := []any{}
	actorJoin := ""
	actorFilters := ""
	claimActorFilter := ""
	if actorID != "" {
		actorJoin = " JOIN actors actor ON actor.id = ?"
		arguments = append(arguments, actorID)
		actorFilters = `
  AND (item.required_actor_kind = 'any' OR item.required_actor_kind = actor.kind)
  AND (item.execution_policy <> 'human_only' OR actor.kind = 'human')
  AND (
    item.execution_policy <> 'approval_required' OR EXISTS (
      SELECT 1 FROM approvals approval
      WHERE approval.work_item_id = item.id AND approval.approved_for_actor_id = actor.id
        AND approval.status = 'approved' AND (approval.expires_at IS NULL OR approval.expires_at > ?)
    )
  )
  AND NOT EXISTS (
    SELECT 1 FROM work_item_capabilities required
    WHERE required.work_item_id = item.id
      AND NOT EXISTS (
        SELECT 1 FROM actor_capabilities assigned
        WHERE assigned.actor_id = actor.id AND assigned.capability_slug = required.capability_slug
      )
  )`
		claimActorFilter = " AND claim.actor_id <> actor.id"
	}
	now := formatTime(time.Now().UTC())
	arguments = append(arguments, now)
	if actorID != "" {
		arguments = append(arguments, now)
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+prefixedObjectiveColumns("objective")+`, `+prefixedWorkItemColumns("item")+`
FROM work_items item
JOIN objectives objective ON objective.id = item.objective_id
`+actorJoin+`
WHERE objective.phase = 'execution'
  AND item.commitment_state = 'accepted'
  AND item.execution_status = 'ready'
  AND EXISTS (
    SELECT 1 FROM plans plan WHERE plan.id = item.plan_id AND plan.commitment_state = 'approved'
  )
  AND NOT EXISTS (
    SELECT 1 FROM dependencies dependency
    JOIN work_items prerequisite ON prerequisite.id = dependency.depends_on_item_id
    WHERE dependency.work_item_id = item.id AND dependency.kind = 'hard' AND prerequisite.execution_status <> 'done'
  )
  AND NOT EXISTS (
    SELECT 1 FROM questions question WHERE question.work_item_id = item.id AND question.status = 'open'
  )
	  AND NOT EXISTS (
	    SELECT 1 FROM claims claim
	    WHERE claim.work_item_id = item.id AND claim.released_at IS NULL AND claim.expires_at > ?`+claimActorFilter+`
	  )`+actorFilters+`
  AND `+strings.ReplaceAll(outputRequirementsSatisfiedSQL, "?", "item.id")+`
ORDER BY CASE item.priority WHEN 'urgent' THEN 1 WHEN 'high' THEN 2 WHEN 'medium' THEN 3 ELSE 4 END,
	         item.updated_at, item.key`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query ready work: %w", err)
	}
	defer rows.Close()
	var result []ports.ReadyWorkItem
	for rows.Next() {
		objective, item, err := scanReadyWorkItem(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, ports.ReadyWorkItem{Objective: objective, WorkItem: item})
	}
	return result, rows.Err()
}

func (s *Store) ListActivity(ctx context.Context, filter ports.ActivityFilter) ([]work.Activity, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := activitySelect + " WHERE sequence > ?"
	arguments := []any{filter.Since}
	if strings.TrimSpace(filter.WorkItemID) != "" {
		query += " AND work_item_id = ?"
		arguments = append(arguments, strings.TrimSpace(filter.WorkItemID))
	}
	query += " ORDER BY sequence LIMIT ?"
	arguments = append(arguments, limit)
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query activity: %w", err)
	}
	defer rows.Close()
	var result []work.Activity
	for rows.Next() {
		activity, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, activity)
	}
	return result, rows.Err()
}

func (s *Store) LatestActivitySequence(ctx context.Context) (int64, error) {
	var sequence int64
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) FROM activity").Scan(&sequence); err != nil {
		return 0, fmt.Errorf("query latest activity sequence: %w", err)
	}
	return sequence, nil
}

func (s *Store) ListAcceptedOutputs(ctx context.Context, filter ports.AcceptedOutputFilter) ([]ports.AcceptedOutput, error) {
	var result []ports.AcceptedOutput
	err := s.withinReadTransaction(ctx, func(reader sqlReader) error {
		var err error
		result, err = s.listAcceptedOutputs(ctx, reader, filter)
		return err
	})
	return result, err
}

func (s *Store) listAcceptedOutputs(ctx context.Context, reader sqlReader, filter ports.AcceptedOutputFilter) ([]ports.AcceptedOutput, error) {
	query := outputRevisionSelect + `
JOIN expected_outputs expected ON expected.id = output_revisions.expected_output_id
JOIN output_profiles profile ON profile.id = output_revisions.output_profile_id
JOIN work_items producer_item ON producer_item.id = expected.work_item_id
WHERE output_revisions.acceptance_state = 'accepted'`
	arguments := make([]any, 0, 6)
	if strings.TrimSpace(filter.ProfileName) != "" {
		query += " AND profile.name = ?"
		arguments = append(arguments, strings.TrimSpace(filter.ProfileName))
	}
	if strings.TrimSpace(filter.VersionConstraint) != "" {
		query += " AND profile.version = ?"
		arguments = append(arguments, strings.TrimPrefix(strings.TrimSpace(filter.VersionConstraint), "="))
	}
	if strings.TrimSpace(filter.ObjectiveID) != "" {
		query += " AND producer_item.objective_id = ?"
		arguments = append(arguments, strings.TrimSpace(filter.ObjectiveID))
	}
	if strings.TrimSpace(filter.ProducedBy) != "" {
		query += " AND output_revisions.produced_by = ?"
		arguments = append(arguments, strings.TrimSpace(filter.ProducedBy))
	}
	if !filter.AcceptedSince.IsZero() {
		query += " AND output_revisions.accepted_at >= ?"
		arguments = append(arguments, formatTime(filter.AcceptedSince))
	}
	query += " ORDER BY output_revisions.accepted_at DESC, output_revisions.id LIMIT ?"
	arguments = append(arguments, filter.Limit)
	rows, err := reader.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query accepted outputs: %w", err)
	}
	defer rows.Close()
	var result []ports.AcceptedOutput
	for rows.Next() {
		revision, err := scanOutputRevision(rows)
		if err != nil {
			return nil, err
		}
		expected, err := scanExpectedOutput(reader.QueryRowContext(ctx, expectedOutputSelect+" WHERE id = ?", revision.ExpectedOutputID))
		if err != nil {
			return nil, err
		}
		profile, err := scanProfile(reader.QueryRowContext(ctx, profileSelect+" WHERE id = ?", revision.OutputProfileID))
		if err != nil {
			return nil, err
		}
		artifacts, err := listRevisionArtifacts(ctx, reader, revision.ID)
		if err != nil {
			return nil, err
		}
		revision.Artifacts, err = listRevisionArtifactBindings(ctx, reader, revision.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, ports.AcceptedOutput{Revision: revision, ExpectedOutput: expected, Profile: profile, Artifacts: artifacts})
	}
	return result, rows.Err()
}

func (s *Store) listAcceptanceCriteria(ctx context.Context, reader sqlReader, workItemID string) ([]work.AcceptanceCriterion, error) {
	rows, err := reader.QueryContext(ctx, acceptanceCriterionSelect+" WHERE work_item_id = ? ORDER BY ordinal", workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []work.AcceptanceCriterion
	for rows.Next() {
		criterion, err := scanAcceptanceCriterion(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, criterion)
	}
	return result, rows.Err()
}

func (s *Store) listDependencies(ctx context.Context, reader sqlReader, workItemID string) ([]work.Dependency, error) {
	rows, err := reader.QueryContext(ctx, dependencySelect+" WHERE work_item_id = ? ORDER BY created_at, id", workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []work.Dependency
	for rows.Next() {
		dependency, err := scanDependency(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, dependency)
	}
	return result, rows.Err()
}

func (s *Store) listOutputRequirements(ctx context.Context, reader sqlReader, workItemID string) ([]output.OutputRequirement, error) {
	rows, err := reader.QueryContext(ctx, outputRequirementSelect+" WHERE work_item_id = ? ORDER BY id", workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []output.OutputRequirement
	for rows.Next() {
		requirement, err := scanOutputRequirement(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, requirement)
	}
	return result, rows.Err()
}

func (s *Store) listOutputRevisionDetails(ctx context.Context, reader sqlReader, workItemID string) ([]ports.OutputRevisionDetail, error) {
	rows, err := reader.QueryContext(ctx, outputRevisionSelect+`
JOIN expected_outputs expected ON expected.id = output_revisions.expected_output_id
WHERE expected.work_item_id = ? ORDER BY expected.ordinal, output_revisions.revision`, workItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ports.OutputRevisionDetail
	for rows.Next() {
		revision, err := scanOutputRevision(rows)
		if err != nil {
			return nil, err
		}
		revision.Artifacts, err = listRevisionArtifactBindings(ctx, reader, revision.ID)
		if err != nil {
			return nil, err
		}
		artifacts, err := listRevisionArtifacts(ctx, reader, revision.ID)
		if err != nil {
			return nil, err
		}
		validations, err := listValidationRecords(ctx, reader, revision.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, ports.OutputRevisionDetail{Revision: revision, Artifacts: artifacts, Validations: validations})
	}
	return result, rows.Err()
}

const acceptanceCriterionSelect = `
SELECT id, work_item_id, ordinal, text, required, status, resolved_at, resolved_by, resolution_rationale
FROM acceptance_criteria`

const dependencySelect = `
SELECT id, work_item_id, depends_on_item_id, kind, note, created_at, created_by
FROM dependencies`

const expectedOutputSelect = `
SELECT id, work_item_id, name, output_profile_id, contract_json, destination_hint, required, ordinal
FROM expected_outputs`

const artifactSelect = `
SELECT id, work_item_id, kind, uri, title, metadata_json, attached_by, created_at
FROM artifacts`

const outputRevisionSelect = `
SELECT output_revisions.id, output_revisions.expected_output_id, output_revisions.output_profile_id,
       output_revisions.revision, output_revisions.content_digest, output_revisions.acceptance_state,
       output_revisions.produced_by, output_revisions.produced_at, output_revisions.accepted_by,
       output_revisions.accepted_at, output_revisions.acceptance_reason
FROM output_revisions `

const outputRequirementSelect = `
SELECT id, work_item_id, required_output_revision_id, required_profile_name, version_constraint, required, note
FROM output_requirements`

const validationSelect = `
SELECT id, output_revision_id, criterion_ref, validator_kind, verdict, score, verifier_actor_id,
       evidence_artifact_id, details_json, created_at
FROM output_validations`

const activitySelect = `
SELECT sequence, id, entity_kind, entity_id, work_item_id, actor_id, event_type, summary, payload_json, created_at
FROM activity`

const outputRequirementsSatisfiedSQL = `NOT EXISTS(
  SELECT 1 FROM output_requirements requirement
  WHERE requirement.work_item_id = ? AND requirement.required = 1
    AND NOT EXISTS (
      SELECT 1
      FROM output_revisions revision
      JOIN output_profiles profile ON profile.id = revision.output_profile_id
      WHERE revision.acceptance_state = 'accepted'
        AND (
          revision.id = requirement.required_output_revision_id
          OR (
            requirement.required_output_revision_id IS NULL
            AND profile.name = requirement.required_profile_name
            AND (requirement.version_constraint = CAST(profile.version AS TEXT)
                 OR requirement.version_constraint = '=' || CAST(profile.version AS TEXT))
          )
        )
    )
)`

func scanAcceptanceCriterion(row scanner) (work.AcceptanceCriterion, error) {
	var criterion work.AcceptanceCriterion
	var required int
	var resolvedAt, resolvedBy sql.NullString
	if err := row.Scan(&criterion.ID, &criterion.WorkItemID, &criterion.Ordinal, &criterion.Text, &required,
		&criterion.Status, &resolvedAt, &resolvedBy, &criterion.ResolutionRationale); err != nil {
		return work.AcceptanceCriterion{}, err
	}
	criterion.Required = required == 1
	criterion.ResolvedBy = resolvedBy.String
	if resolvedAt.Valid {
		var err error
		criterion.ResolvedAt, err = parseTime(resolvedAt.String)
		if err != nil {
			return work.AcceptanceCriterion{}, err
		}
	}
	return criterion, nil
}

func scanDependency(row scanner) (work.Dependency, error) {
	var dependency work.Dependency
	var createdAt string
	if err := row.Scan(&dependency.ID, &dependency.WorkItemID, &dependency.DependsOnItemID, &dependency.Kind,
		&dependency.Note, &createdAt, &dependency.CreatedBy); err != nil {
		return work.Dependency{}, err
	}
	var err error
	dependency.CreatedAt, err = parseTime(createdAt)
	return dependency, err
}

func scanExpectedOutput(row scanner) (output.ExpectedOutput, error) {
	var expected output.ExpectedOutput
	var contract string
	var required int
	if err := row.Scan(&expected.ID, &expected.WorkItemID, &expected.Name, &expected.OutputProfileID,
		&contract, &expected.DestinationHint, &required, &expected.Ordinal); err != nil {
		return output.ExpectedOutput{}, err
	}
	expected.Contract = []byte(contract)
	expected.Required = required == 1
	return expected, nil
}

func scanArtifact(row scanner) (output.Artifact, error) {
	var artifact output.Artifact
	var metadata, createdAt string
	if err := row.Scan(&artifact.ID, &artifact.WorkItemID, &artifact.Kind, &artifact.URI,
		&artifact.Title, &metadata, &artifact.AttachedBy, &createdAt); err != nil {
		return output.Artifact{}, err
	}
	artifact.Metadata = []byte(metadata)
	var err error
	artifact.CreatedAt, err = parseTime(createdAt)
	return artifact, err
}

func scanOutputRevision(row scanner) (output.OutputRevision, error) {
	var revision output.OutputRevision
	var producedAt string
	var acceptedBy, acceptedAt sql.NullString
	if err := row.Scan(&revision.ID, &revision.ExpectedOutputID, &revision.OutputProfileID, &revision.Revision,
		&revision.ContentDigest, &revision.AcceptanceState, &revision.ProducedBy, &producedAt,
		&acceptedBy, &acceptedAt, &revision.AcceptanceReason); err != nil {
		return output.OutputRevision{}, err
	}
	var err error
	revision.ProducedAt, err = parseTime(producedAt)
	if err != nil {
		return output.OutputRevision{}, err
	}
	revision.AcceptedBy = acceptedBy.String
	if acceptedAt.Valid {
		revision.AcceptedAt, err = parseTime(acceptedAt.String)
	}
	return revision, err
}

func scanOutputRequirement(row scanner) (output.OutputRequirement, error) {
	var requirement output.OutputRequirement
	var revisionID, profileName, versionConstraint sql.NullString
	var required int
	if err := row.Scan(&requirement.ID, &requirement.WorkItemID, &revisionID, &profileName,
		&versionConstraint, &required, &requirement.Note); err != nil {
		return output.OutputRequirement{}, err
	}
	requirement.RequiredOutputRevisionID = revisionID.String
	requirement.RequiredProfileName = profileName.String
	requirement.VersionConstraint = versionConstraint.String
	requirement.Required = required == 1
	return requirement, nil
}

func scanValidationRecord(row scanner) (output.ValidationRecord, error) {
	var record output.ValidationRecord
	var score sql.NullFloat64
	var evidence sql.NullString
	var details, createdAt string
	if err := row.Scan(&record.ID, &record.OutputRevisionID, &record.CriterionRef, &record.ValidatorKind,
		&record.Verdict, &score, &record.VerifierActorID, &evidence, &details, &createdAt); err != nil {
		return output.ValidationRecord{}, err
	}
	if score.Valid {
		value := score.Float64
		record.Score = &value
	}
	record.EvidenceArtifactID = evidence.String
	record.Details = []byte(details)
	var err error
	record.CreatedAt, err = parseTime(createdAt)
	return record, err
}

func scanActivity(row scanner) (work.Activity, error) {
	var activity work.Activity
	var workItemID sql.NullString
	var payload, createdAt string
	if err := row.Scan(&activity.Sequence, &activity.ID, &activity.EntityKind, &activity.EntityID,
		&workItemID, &activity.ActorID, &activity.EventType, &activity.Summary, &payload, &createdAt); err != nil {
		return work.Activity{}, err
	}
	activity.WorkItemID = workItemID.String
	activity.PayloadJSON = []byte(payload)
	var err error
	activity.CreatedAt, err = parseTime(createdAt)
	return activity, err
}

func listValidationRecords(ctx context.Context, reader sqlReader, revisionID string) ([]output.ValidationRecord, error) {
	rows, err := reader.QueryContext(ctx, validationSelect+" WHERE output_revision_id = ? ORDER BY created_at, id", revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []output.ValidationRecord
	for rows.Next() {
		record, err := scanValidationRecord(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func listRevisionArtifactBindings(ctx context.Context, reader sqlReader, revisionID string) ([]output.RevisionArtifact, error) {
	rows, err := reader.QueryContext(ctx, `
SELECT artifact_id, role FROM output_revision_artifacts WHERE output_revision_id = ? ORDER BY artifact_id`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []output.RevisionArtifact
	for rows.Next() {
		var binding output.RevisionArtifact
		if err := rows.Scan(&binding.ArtifactID, &binding.Role); err != nil {
			return nil, err
		}
		result = append(result, binding)
	}
	return result, rows.Err()
}

func listRevisionArtifacts(ctx context.Context, reader sqlReader, revisionID string) ([]output.Artifact, error) {
	rows, err := reader.QueryContext(ctx, `
SELECT artifact.id, artifact.work_item_id, artifact.kind, artifact.uri, artifact.title,
       artifact.metadata_json, artifact.attached_by, artifact.created_at
FROM artifacts artifact
JOIN output_revision_artifacts binding ON binding.artifact_id = artifact.id
WHERE binding.output_revision_id = ? ORDER BY artifact.id`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []output.Artifact
	for rows.Next() {
		var artifact output.Artifact
		var metadata, createdAt string
		if err := rows.Scan(&artifact.ID, &artifact.WorkItemID, &artifact.Kind, &artifact.URI,
			&artifact.Title, &metadata, &artifact.AttachedBy, &createdAt); err != nil {
			return nil, err
		}
		artifact.Metadata = []byte(metadata)
		artifact.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		result = append(result, artifact)
	}
	return result, rows.Err()
}

func queryBoolean(ctx context.Context, reader sqlReader, query string, arguments ...any) (bool, error) {
	var value int
	if err := reader.QueryRowContext(ctx, query, arguments...).Scan(&value); err != nil {
		return false, err
	}
	return value == 1, nil
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func prefixedObjectiveColumns(alias string) string {
	return alias + `.id, ` + alias + `.key, ` + alias + `.title, ` + alias + `.description, ` +
		alias + `.desired_outcome, ` + alias + `.phase, ` + alias + `.prior_phase, ` + alias + `.updated_by, ` +
		alias + `.version, ` + alias + `.created_at, ` + alias + `.updated_at`
}

func prefixedWorkItemColumns(alias string) string {
	return alias + `.id, ` + alias + `.key, ` + alias + `.objective_id, ` + alias + `.plan_id, ` +
		alias + `.parent_id, ` + alias + `.title, ` + alias + `.description, ` + alias + `.kind, ` +
		alias + `.commitment_state, ` + alias + `.execution_status, ` + alias + `.priority, ` +
		alias + `.estimated_scope, ` + alias + `.execution_policy, ` + alias + `.required_actor_kind, ` +
		alias + `.attention_state, ` + alias + `.version, ` + alias + `.created_at, ` + alias + `.updated_at`
}

func scanReadyWorkItem(row scanner) (work.Objective, work.WorkItem, error) {
	var objective work.Objective
	var item work.WorkItem
	var objectivePriorPhase, objectiveUpdatedBy, planID, parentID sql.NullString
	var objectiveCreatedAt, objectiveUpdatedAt, itemCreatedAt, itemUpdatedAt string
	if err := row.Scan(
		&objective.ID, &objective.Key, &objective.Title, &objective.Description, &objective.DesiredOutcome,
		&objective.Phase, &objectivePriorPhase, &objectiveUpdatedBy, &objective.Version, &objectiveCreatedAt, &objectiveUpdatedAt,
		&item.ID, &item.Key, &item.ObjectiveID, &planID, &parentID, &item.Title, &item.Description, &item.Kind,
		&item.CommitmentState, &item.ExecutionStatus, &item.Priority, &item.EstimatedScope, &item.ExecutionPolicy,
		&item.RequiredActorKind, &item.AttentionState, &item.Version, &itemCreatedAt, &itemUpdatedAt,
	); err != nil {
		return work.Objective{}, work.WorkItem{}, err
	}
	objective.PriorPhase = work.ObjectivePhase(objectivePriorPhase.String)
	objective.UpdatedBy = objectiveUpdatedBy.String
	item.PlanID = planID.String
	item.ParentID = parentID.String
	var err error
	objective.CreatedAt, err = parseTime(objectiveCreatedAt)
	if err == nil {
		objective.UpdatedAt, err = parseTime(objectiveUpdatedAt)
	}
	if err == nil {
		item.CreatedAt, err = parseTime(itemCreatedAt)
	}
	if err == nil {
		item.UpdatedAt, err = parseTime(itemUpdatedAt)
	}
	return objective, item, err
}
