package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dennisschroeder/throughline/internal/domain/output"
	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

type Store struct {
	db *sql.DB
}

func (d *Database) Store() *Store {
	return &Store{db: d.db}
}

func (s *Store) WithinTransaction(ctx context.Context, operation func(ports.Repository) error) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer transaction.Rollback()
	if err := operation(&transactionRepository{transaction: transaction}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

type sqlReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) withinReadTransaction(ctx context.Context, operation func(sqlReader) error) error {
	transaction, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read transaction: %w", err)
	}
	defer transaction.Rollback()
	if err := operation(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit read transaction: %w", err)
	}
	return nil
}

func (s *Store) GetWorkItemContext(ctx context.Context, id string) (ports.WorkItemContext, error) {
	var result ports.WorkItemContext
	err := s.withinReadTransaction(ctx, func(reader sqlReader) error {
		var err error
		result, err = s.getWorkItemContext(ctx, reader, id)
		return err
	})
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	return result, nil
}

func (s *Store) SelectObjectiveContext(ctx context.Context, query ports.ObjectiveContextSelectionQuery) (ports.ObjectiveContextSelection, error) {
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var result ports.ObjectiveContextSelection
	err := s.withinReadTransaction(ctx, func(reader sqlReader) error {
		context, err := s.getObjectiveContext(ctx, reader, query.ObjectiveID)
		if err != nil {
			return err
		}
		result.Context = context
		itemIDs := make([]string, 0)
		ready, err := s.listReadyWork(ctx, reader, query.ActorID)
		if err != nil {
			return err
		}
		for _, item := range ready {
			if item.Objective.ID == context.Objective.ID {
				itemIDs = append(itemIDs, item.WorkItem.ID)
			}
		}
		if query.ActorID != "" {
			claimedRows, err := reader.QueryContext(ctx, `
SELECT item.id
FROM claims claim
JOIN work_items item ON item.id = claim.work_item_id
WHERE claim.actor_id = ? AND claim.released_at IS NULL AND claim.expires_at > ?
  AND item.objective_id = ?
ORDER BY item.updated_at, item.id`, query.ActorID, formatTime(time.Now().UTC()), context.Objective.ID)
			if err != nil {
				return fmt.Errorf("query actor claimed work: %w", err)
			}
			for claimedRows.Next() {
				var id string
				if err := claimedRows.Scan(&id); err != nil {
					_ = claimedRows.Close()
					return err
				}
				itemIDs = append(itemIDs, id)
			}
			if err := claimedRows.Err(); err != nil {
				_ = claimedRows.Close()
				return err
			}
			if err := claimedRows.Close(); err != nil {
				return err
			}
		}
		selected := make(map[string]bool, len(itemIDs))
		for _, id := range itemIDs {
			if selected[id] {
				continue
			}
			if len(result.WorkItems) == limit {
				break
			}
			item, err := s.getWorkItemContext(ctx, reader, id)
			if err != nil {
				return err
			}
			result.WorkItems = append(result.WorkItems, item)
			selected[id] = true
		}
		changes, err := s.listRecentActivity(ctx, reader, context.Objective.ID, selected, limit*2)
		if err != nil {
			return err
		}
		for _, change := range changes {
			if change.EntityID == context.Objective.ID || selected[change.WorkItemID] {
				result.RecentChanges = append(result.RecentChanges, change)
			}
		}
		return nil
	})
	if err != nil {
		return ports.ObjectiveContextSelection{}, err
	}
	return result, nil
}

func (s *Store) listRecentActivity(ctx context.Context, reader sqlReader, objectiveID string, workItemIDs map[string]bool, limit int) ([]work.Activity, error) {
	conditions := []string{"entity_kind = 'objective' AND entity_id = ?"}
	arguments := []any{objectiveID}
	if len(workItemIDs) > 0 {
		placeholders := make([]string, 0, len(workItemIDs))
		for workItemID := range workItemIDs {
			placeholders = append(placeholders, "?")
			arguments = append(arguments, workItemID)
		}
		conditions = append(conditions, "work_item_id IN ("+strings.Join(placeholders, ", ")+")")
	}
	arguments = append(arguments, limit)
	rows, err := reader.QueryContext(ctx, activitySelect+" WHERE ("+strings.Join(conditions, " OR ")+") ORDER BY sequence DESC LIMIT ?", arguments...)
	if err != nil {
		return nil, fmt.Errorf("query recent activity: %w", err)
	}
	defer rows.Close()
	result := make([]work.Activity, 0, limit)
	for rows.Next() {
		activity, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, activity)
	}
	return result, rows.Err()
}

func (s *Store) ListWorkItemContexts(ctx context.Context) ([]ports.WorkItemContext, error) {
	var result []ports.WorkItemContext
	err := s.withinReadTransaction(ctx, func(reader sqlReader) error {
		rows, err := reader.QueryContext(ctx, "SELECT id FROM work_items ORDER BY updated_at DESC, id")
		if err != nil {
			return fmt.Errorf("list work item ids: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			item, err := s.getWorkItemContext(ctx, reader, id)
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) getWorkItemContext(ctx context.Context, reader sqlReader, id string) (ports.WorkItemContext, error) {
	item, err := scanWorkItem(reader.QueryRowContext(ctx, workItemSelect+" WHERE id = ?", id))
	if err != nil {
		return ports.WorkItemContext{}, mapNotFound(err)
	}
	objective, err := scanObjective(reader.QueryRowContext(ctx, objectiveSelect+" WHERE id = ?", item.ObjectiveID))
	if err != nil {
		return ports.WorkItemContext{}, mapNotFound(err)
	}
	var plan *work.Plan
	if item.PlanID != "" {
		loadedPlan, err := scanPlan(reader.QueryRowContext(ctx, planSelect+" WHERE id = ?", item.PlanID))
		if err != nil {
			return ports.WorkItemContext{}, mapNotFound(err)
		}
		plan = &loadedPlan
	}
	expectedOutputs, err := s.listExpectedOutputs(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	capabilities, err := s.listCapabilities(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	criteria, err := s.listAcceptanceCriteria(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	dependencies, err := s.listDependencies(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	requirements, err := s.listOutputRequirements(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	revisions, err := s.listOutputRevisionDetails(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	claims, err := listClaims(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	progress, err := listProgressEntries(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	artifacts, err := listWorkItemArtifacts(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	externalActions, err := listExternalActionDetails(ctx, reader, item.ID)
	if err != nil {
		return ports.WorkItemContext{}, err
	}
	return ports.WorkItemContext{
		Objective:            objective,
		Plan:                 plan,
		WorkItem:             item,
		RequiredCapabilities: capabilities,
		ExpectedOutputs:      expectedOutputs,
		AcceptanceCriteria:   criteria,
		Dependencies:         dependencies,
		OutputRequirements:   requirements,
		OutputRevisions:      revisions,
		Claims:               claims,
		Progress:             progress,
		Artifacts:            artifacts,
		ExternalActions:      externalActions,
	}, nil
}

type transactionRepository struct {
	transaction *sql.Tx
}

func (r *transactionRepository) CreateObjective(ctx context.Context, objective work.Objective) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO objectives
  (id, key, title, description, desired_outcome, phase, version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		objective.ID,
		objective.Key,
		objective.Title,
		objective.Description,
		objective.DesiredOutcome,
		objective.Phase,
		objective.Version,
		formatTime(objective.CreatedAt),
		formatTime(objective.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert objective: %w", err)
	}
	return nil
}

func (r *transactionRepository) Objective(ctx context.Context, id string) (work.Objective, error) {
	objective, err := scanObjective(r.transaction.QueryRowContext(ctx, objectiveSelect+" WHERE id = ?", id))
	return objective, mapNotFound(err)
}

func (r *transactionRepository) CreatePlan(ctx context.Context, plan work.Plan) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO plans
  (id, objective_id, title, summary, revision, commitment_state, proposed_by, proposed_at,
   resolved_by, resolved_at, resolution_reason, version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		plan.ID,
		plan.ObjectiveID,
		plan.Title,
		plan.Summary,
		plan.Revision,
		plan.CommitmentState,
		nullableString(plan.ProposedBy),
		nullableTime(plan.ProposedAt),
		nullableString(plan.ResolvedBy),
		nullableTime(plan.ResolvedAt),
		plan.ResolutionReason,
		plan.Version,
		formatTime(plan.CreatedAt),
		formatTime(plan.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert plan: %w", err)
	}
	return nil
}

func (r *transactionRepository) Plan(ctx context.Context, id string) (work.Plan, error) {
	plan, err := scanPlan(r.transaction.QueryRowContext(ctx, planSelect+" WHERE id = ?", id))
	return plan, mapNotFound(err)
}

func (r *transactionRepository) CreateWorkItem(ctx context.Context, item work.WorkItem) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO work_items
  (id, key, objective_id, plan_id, parent_id, title, description, kind, commitment_state,
   execution_status, priority, estimated_scope, execution_policy, required_actor_kind,
   attention_state, version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID,
		item.Key,
		item.ObjectiveID,
		nullableString(item.PlanID),
		nullableString(item.ParentID),
		item.Title,
		item.Description,
		item.Kind,
		item.CommitmentState,
		item.ExecutionStatus,
		item.Priority,
		item.EstimatedScope,
		item.ExecutionPolicy,
		item.RequiredActorKind,
		item.AttentionState,
		item.Version,
		formatTime(item.CreatedAt),
		formatTime(item.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert work item: %w", err)
	}
	return nil
}

func (r *transactionRepository) WorkItem(ctx context.Context, id string) (work.WorkItem, error) {
	item, err := scanWorkItem(r.transaction.QueryRowContext(ctx, workItemSelect+" WHERE id = ?", id))
	return item, mapNotFound(err)
}

func (r *transactionRepository) OutputProfile(ctx context.Context, name string, version int) (output.Profile, error) {
	profile, err := scanProfile(r.transaction.QueryRowContext(ctx, profileSelect+" WHERE name = ? AND version = ?", name, version))
	return profile, mapNotFound(err)
}

func (r *transactionRepository) CreateExpectedOutput(ctx context.Context, expected output.ExpectedOutput) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO expected_outputs
  (id, work_item_id, name, output_profile_id, contract_json, destination_hint, required, ordinal)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		expected.ID,
		expected.WorkItemID,
		expected.Name,
		expected.OutputProfileID,
		string(expected.Contract),
		expected.DestinationHint,
		boolInt(expected.Required),
		expected.Ordinal,
	)
	if err != nil {
		return fmt.Errorf("insert expected output: %w", err)
	}
	return nil
}

const objectiveSelect = `
SELECT id, key, title, description, desired_outcome, phase, prior_phase, updated_by,
       version, created_at, updated_at
FROM objectives`

const planSelect = `
SELECT id, objective_id, title, summary, revision, commitment_state,
       proposed_by, proposed_at, resolved_by, resolved_at, resolution_reason,
       version, created_at, updated_at
FROM plans`

const workItemSelect = `
SELECT id, key, objective_id, plan_id, parent_id, title, description, kind, commitment_state,
       execution_status, priority, estimated_scope, execution_policy, required_actor_kind,
       attention_state, version, created_at, updated_at
FROM work_items`

const profileSelect = `
SELECT id, name, version, state_version, description, lifecycle_state, structure_json, semantics_json,
       validation_json, built_in, supersedes_id, proposed_by, proposed_at,
       resolved_by, resolved_at, resolution_reason, created_at
FROM output_profiles`

type scanner interface {
	Scan(...any) error
}

func scanObjective(row scanner) (work.Objective, error) {
	var objective work.Objective
	var createdAt, updatedAt string
	var priorPhase, updatedBy sql.NullString
	if err := row.Scan(
		&objective.ID,
		&objective.Key,
		&objective.Title,
		&objective.Description,
		&objective.DesiredOutcome,
		&objective.Phase,
		&priorPhase,
		&updatedBy,
		&objective.Version,
		&createdAt,
		&updatedAt,
	); err != nil {
		return work.Objective{}, err
	}
	objective.PriorPhase = work.ObjectivePhase(priorPhase.String)
	objective.UpdatedBy = updatedBy.String
	var err error
	objective.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return work.Objective{}, err
	}
	objective.UpdatedAt, err = parseTime(updatedAt)
	return objective, err
}

func scanPlan(row scanner) (work.Plan, error) {
	var plan work.Plan
	var createdAt, updatedAt string
	var proposedBy, proposedAt, resolvedBy, resolvedAt sql.NullString
	if err := row.Scan(
		&plan.ID,
		&plan.ObjectiveID,
		&plan.Title,
		&plan.Summary,
		&plan.Revision,
		&plan.CommitmentState,
		&proposedBy,
		&proposedAt,
		&resolvedBy,
		&resolvedAt,
		&plan.ResolutionReason,
		&plan.Version,
		&createdAt,
		&updatedAt,
	); err != nil {
		return work.Plan{}, err
	}
	plan.ProposedBy = proposedBy.String
	plan.ResolvedBy = resolvedBy.String
	var err error
	if proposedAt.Valid {
		plan.ProposedAt, err = parseTime(proposedAt.String)
		if err != nil {
			return work.Plan{}, err
		}
	}
	if resolvedAt.Valid {
		plan.ResolvedAt, err = parseTime(resolvedAt.String)
		if err != nil {
			return work.Plan{}, err
		}
	}
	plan.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return work.Plan{}, err
	}
	plan.UpdatedAt, err = parseTime(updatedAt)
	return plan, err
}

func scanWorkItem(row scanner) (work.WorkItem, error) {
	var item work.WorkItem
	var planID, parentID sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&item.ID,
		&item.Key,
		&item.ObjectiveID,
		&planID,
		&parentID,
		&item.Title,
		&item.Description,
		&item.Kind,
		&item.CommitmentState,
		&item.ExecutionStatus,
		&item.Priority,
		&item.EstimatedScope,
		&item.ExecutionPolicy,
		&item.RequiredActorKind,
		&item.AttentionState,
		&item.Version,
		&createdAt,
		&updatedAt,
	); err != nil {
		return work.WorkItem{}, err
	}
	item.PlanID = planID.String
	item.ParentID = parentID.String
	var err error
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return work.WorkItem{}, err
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	return item, err
}

func scanProfile(row scanner) (output.Profile, error) {
	var profile output.Profile
	var structure, semantics, validation, createdAt string
	var builtIn int
	var supersedesID, proposedBy, proposedAt, resolvedBy, resolvedAt sql.NullString
	if err := row.Scan(
		&profile.ID,
		&profile.Name,
		&profile.Version,
		&profile.StateVersion,
		&profile.Description,
		&profile.LifecycleState,
		&structure,
		&semantics,
		&validation,
		&builtIn,
		&supersedesID,
		&proposedBy,
		&proposedAt,
		&resolvedBy,
		&resolvedAt,
		&profile.ResolutionReason,
		&createdAt,
	); err != nil {
		return output.Profile{}, err
	}
	profile.Structure = json.RawMessage(structure)
	profile.Semantics = json.RawMessage(semantics)
	profile.Validation = json.RawMessage(validation)
	profile.BuiltIn = builtIn == 1
	profile.SupersedesID = supersedesID.String
	profile.ProposedBy = proposedBy.String
	profile.ResolvedBy = resolvedBy.String
	var err error
	if proposedAt.Valid {
		profile.ProposedAt, err = parseTime(proposedAt.String)
		if err != nil {
			return output.Profile{}, err
		}
	}
	if resolvedAt.Valid {
		profile.ResolvedAt, err = parseTime(resolvedAt.String)
		if err != nil {
			return output.Profile{}, err
		}
	}
	profile.CreatedAt, err = parseTime(createdAt)
	return profile, err
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrNotFound
	}
	return err
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse database timestamp %q: %w", value, err)
	}
	return parsed.UTC(), nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}
