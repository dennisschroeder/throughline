package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dennisschroeder/throughline/internal/domain/work"
	"github.com/dennisschroeder/throughline/internal/ports"
)

func (r *transactionRepository) CreatePlanInput(ctx context.Context, input work.PlanInput) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO plan_inputs (id, plan_id, name, description, required, ordinal, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		input.ID, input.PlanID, input.Name, input.Description, boolInt(input.Required), input.Ordinal, formatTime(input.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert plan input: %w", err)
	}
	return nil
}

const planInputSelect = `
SELECT id, plan_id, name, description, required, ordinal, created_at
FROM plan_inputs`

func (r *transactionRepository) PlanInputs(ctx context.Context, planID string) ([]work.PlanInput, error) {
	rows, err := r.transaction.QueryContext(ctx, planInputSelect+" WHERE plan_id = ? ORDER BY ordinal", planID)
	if err != nil {
		return nil, fmt.Errorf("query plan inputs: %w", err)
	}
	defer rows.Close()
	var inputs []work.PlanInput
	for rows.Next() {
		var input work.PlanInput
		var required int
		var createdAt string
		if err := rows.Scan(&input.ID, &input.PlanID, &input.Name, &input.Description, &required, &input.Ordinal, &createdAt); err != nil {
			return nil, fmt.Errorf("scan plan input: %w", err)
		}
		input.Required = required == 1
		if input.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, rows.Err()
}

func (r *transactionRepository) CreatePlanStep(ctx context.Context, step work.PlanStep) error {
	definition, err := json.Marshal(step.Definition)
	if err != nil {
		return fmt.Errorf("encode plan step definition: %w", err)
	}
	_, err = r.transaction.ExecContext(ctx, `
INSERT INTO plan_steps
  (id, plan_id, client_ref, parent_step_id, key, title, description, kind, required, priority,
   estimated_scope, execution_policy, required_actor_kind, review_requirements_json, ordinal,
   definition_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		step.ID, step.PlanID, step.ClientRef, nullableString(step.ParentStepID), step.Key, step.Title,
		step.Description, step.Kind, boolInt(step.Required), step.Priority, step.EstimatedScope,
		step.ExecutionPolicy, step.RequiredActorKind, encodeReviewRequirements(step.ReviewRequirements),
		step.Ordinal, string(definition), formatTime(step.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("plan step key %q is already used by another plan; step keys are unique across the workspace because each run materializes its work item as \"<step key>/<run sequence>\": %w", step.Key, err)
		}
		return fmt.Errorf("insert plan step: %w", err)
	}
	return nil
}

// isUniqueViolation recognizes a SQLite uniqueness failure from its message,
// which is the only thing the CGo-free driver exposes without a typed error.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

const planStepSelect = `
SELECT id, plan_id, client_ref, parent_step_id, key, title, description, kind, required, priority,
       estimated_scope, execution_policy, required_actor_kind, review_requirements_json, ordinal,
       definition_json, created_at
FROM plan_steps`

func (r *transactionRepository) PlanSteps(ctx context.Context, planID string) ([]work.PlanStep, error) {
	rows, err := r.transaction.QueryContext(ctx, planStepSelect+" WHERE plan_id = ? ORDER BY ordinal", planID)
	if err != nil {
		return nil, fmt.Errorf("query plan steps: %w", err)
	}
	defer rows.Close()
	var steps []work.PlanStep
	for rows.Next() {
		step, err := scanPlanStep(rows)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, rows.Err()
}

func scanPlanStep(row scanner) (work.PlanStep, error) {
	var step work.PlanStep
	var parentStepID sql.NullString
	var required int
	var reviewRequirements, definition, createdAt string
	if err := row.Scan(&step.ID, &step.PlanID, &step.ClientRef, &parentStepID, &step.Key, &step.Title,
		&step.Description, &step.Kind, &required, &step.Priority, &step.EstimatedScope,
		&step.ExecutionPolicy, &step.RequiredActorKind, &reviewRequirements, &step.Ordinal,
		&definition, &createdAt); err != nil {
		return work.PlanStep{}, fmt.Errorf("scan plan step: %w", err)
	}
	step.ParentStepID = parentStepID.String
	step.Required = required == 1
	var err error
	if step.ReviewRequirements, err = decodeReviewRequirements(reviewRequirements); err != nil {
		return work.PlanStep{}, err
	}
	if err := json.Unmarshal([]byte(definition), &step.Definition); err != nil {
		return work.PlanStep{}, fmt.Errorf("decode plan step definition: %w", err)
	}
	step.CreatedAt, err = parseTime(createdAt)
	return step, err
}

func (r *transactionRepository) CreatePlanStepDependency(ctx context.Context, dependency work.PlanStepDependency) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO plan_step_dependencies (plan_step_id, depends_on_step_id) VALUES (?, ?)`,
		dependency.PlanStepID, dependency.DependsOnStepID)
	if err != nil {
		return fmt.Errorf("insert plan step dependency: %w", err)
	}
	return nil
}

func (r *transactionRepository) PlanStepDependencies(ctx context.Context, planID string) ([]work.PlanStepDependency, error) {
	rows, err := r.transaction.QueryContext(ctx, `
SELECT dependency.plan_step_id, dependency.depends_on_step_id
FROM plan_step_dependencies dependency
JOIN plan_steps step ON step.id = dependency.plan_step_id
WHERE step.plan_id = ?
ORDER BY dependency.plan_step_id, dependency.depends_on_step_id`, planID)
	if err != nil {
		return nil, fmt.Errorf("query plan step dependencies: %w", err)
	}
	defer rows.Close()
	var dependencies []work.PlanStepDependency
	for rows.Next() {
		var dependency work.PlanStepDependency
		if err := rows.Scan(&dependency.PlanStepID, &dependency.DependsOnStepID); err != nil {
			return nil, fmt.Errorf("scan plan step dependency: %w", err)
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, rows.Err()
}

// PlanStepDependencyCreatesCycle answers the same question for step
// definitions that DependencyCreatesCycle answers for work items, so a plan
// cannot be approved with a prerequisite loop that would deadlock every run
// materialized from it.
func (r *transactionRepository) PlanStepDependencyCreatesCycle(ctx context.Context, stepID, dependsOnStepID string) (bool, error) {
	if stepID == dependsOnStepID {
		return true, nil
	}
	return queryBoolean(ctx, r.transaction, `
WITH RECURSIVE reachable(id) AS (
  SELECT ?
  UNION
  SELECT dependency.depends_on_step_id
  FROM plan_step_dependencies dependency
  JOIN reachable ON reachable.id = dependency.plan_step_id
)
SELECT EXISTS(SELECT 1 FROM reachable WHERE id = ?)`, dependsOnStepID, stepID)
}

func (r *transactionRepository) CreatePlanRun(ctx context.Context, run work.PlanRun) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO plan_runs
  (id, objective_id, plan_id, run_key, sequence, status, binding_fingerprint, started_by, started_at,
   closed_by, closed_at, close_reason, version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?, ?)`,
		run.ID, run.ObjectiveID, run.PlanID, run.RunKey, run.Sequence, run.Status, run.BindingFingerprint,
		run.StartedBy, formatTime(run.StartedAt), run.Version, formatTime(run.CreatedAt), formatTime(run.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert plan run: %w", err)
	}
	return nil
}

const planRunSelect = `
SELECT id, objective_id, plan_id, run_key, sequence, status, binding_fingerprint, started_by,
       started_at, closed_by, closed_at, close_reason, version, created_at, updated_at
FROM plan_runs`

func (r *transactionRepository) PlanRun(ctx context.Context, id string) (work.PlanRun, error) {
	run, err := scanPlanRun(r.transaction.QueryRowContext(ctx, planRunSelect+" WHERE id = ?", id))
	return run, mapNotFound(err)
}

// PlanRunByKey resolves a run key within its objective and deliberately does
// not filter by actor: the key names the business execution instance, so a
// different actor retrying the same creation must find the same run rather
// than start a second one.
func (r *transactionRepository) PlanRunByKey(ctx context.Context, objectiveID, runKey string) (work.PlanRun, error) {
	run, err := scanPlanRun(r.transaction.QueryRowContext(ctx, planRunSelect+" WHERE objective_id = ? AND run_key = ?", objectiveID, runKey))
	return run, mapNotFound(err)
}

func scanPlanRun(row scanner) (work.PlanRun, error) {
	var run work.PlanRun
	var closedBy, closedAt, closeReason sql.NullString
	var startedAt, createdAt, updatedAt string
	if err := row.Scan(&run.ID, &run.ObjectiveID, &run.PlanID, &run.RunKey, &run.Sequence, &run.Status,
		&run.BindingFingerprint, &run.StartedBy, &startedAt, &closedBy, &closedAt, &closeReason,
		&run.Version, &createdAt, &updatedAt); err != nil {
		return work.PlanRun{}, err
	}
	run.ClosedBy = closedBy.String
	run.CloseReason = closeReason.String
	var err error
	if run.StartedAt, err = parseTime(startedAt); err != nil {
		return work.PlanRun{}, err
	}
	if closedAt.Valid {
		if run.ClosedAt, err = parseTime(closedAt.String); err != nil {
			return work.PlanRun{}, err
		}
	}
	if run.CreatedAt, err = parseTime(createdAt); err != nil {
		return work.PlanRun{}, err
	}
	run.UpdatedAt, err = parseTime(updatedAt)
	return run, err
}

// ActivePlanRunCount counts across every plan revision, because the limit is
// the objective's, not a revision's.
func (r *transactionRepository) ActivePlanRunCount(ctx context.Context, objectiveID string) (int, error) {
	var count int
	if err := r.transaction.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM plan_runs WHERE objective_id = ? AND status = 'active'", objectiveID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active plan runs: %w", err)
	}
	return count, nil
}

// NextPlanRunSequence is read inside the creating transaction and written in
// the same one. The UNIQUE(objective_id, sequence) constraint is what actually
// prevents two concurrent creations from agreeing on a number.
func (r *transactionRepository) NextPlanRunSequence(ctx context.Context, objectiveID string) (int, error) {
	var sequence sql.NullInt64
	if err := r.transaction.QueryRowContext(ctx,
		"SELECT MAX(sequence) FROM plan_runs WHERE objective_id = ?", objectiveID).Scan(&sequence); err != nil {
		return 0, fmt.Errorf("read latest plan run sequence: %w", err)
	}
	return int(sequence.Int64) + 1, nil
}

func (r *transactionRepository) UpdatePlanRun(ctx context.Context, run work.PlanRun, expectedVersion int) error {
	result, err := r.transaction.ExecContext(ctx, `
UPDATE plan_runs
SET status = ?, closed_by = ?, closed_at = ?, close_reason = ?, version = ?, updated_at = ?
WHERE id = ? AND version = ?`,
		run.Status, nullableString(run.ClosedBy), nullableTime(run.ClosedAt), nullableString(run.CloseReason),
		run.Version, formatTime(run.UpdatedAt), run.ID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update plan run: %w", err)
	}
	return requireChanged(result)
}

func (r *transactionRepository) CreateRunInputBinding(ctx context.Context, binding work.RunInputBinding) error {
	_, err := r.transaction.ExecContext(ctx, `
INSERT INTO run_input_bindings
  (id, plan_run_id, plan_input_id, name, kind, value, output_revision_id, locator, source_version,
   digest, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		binding.ID, binding.PlanRunID, binding.PlanInputID, binding.Name, binding.Kind, binding.Value,
		nullableString(binding.OutputRevisionID), binding.Locator, binding.SourceVersion, binding.Digest,
		binding.CreatedBy, formatTime(binding.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert run input binding: %w", err)
	}
	return nil
}

func (r *transactionRepository) RunInputBindings(ctx context.Context, planRunID string) ([]work.RunInputBinding, error) {
	rows, err := r.transaction.QueryContext(ctx, `
SELECT id, plan_run_id, plan_input_id, name, kind, value, output_revision_id, locator, source_version,
       digest, created_by, created_at
FROM run_input_bindings WHERE plan_run_id = ? ORDER BY name`, planRunID)
	if err != nil {
		return nil, fmt.Errorf("query run input bindings: %w", err)
	}
	defer rows.Close()
	var bindings []work.RunInputBinding
	for rows.Next() {
		var binding work.RunInputBinding
		var outputRevisionID sql.NullString
		var createdAt string
		if err := rows.Scan(&binding.ID, &binding.PlanRunID, &binding.PlanInputID, &binding.Name, &binding.Kind,
			&binding.Value, &outputRevisionID, &binding.Locator, &binding.SourceVersion, &binding.Digest,
			&binding.CreatedBy, &createdAt); err != nil {
			return nil, fmt.Errorf("scan run input binding: %w", err)
		}
		binding.OutputRevisionID = outputRevisionID.String
		if binding.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}

func (r *transactionRepository) PlanRunWorkItems(ctx context.Context, planRunID string) ([]work.WorkItem, error) {
	rows, err := r.transaction.QueryContext(ctx, workItemSelect+" WHERE plan_run_id = ? ORDER BY key", planRunID)
	if err != nil {
		return nil, fmt.Errorf("query plan run work items: %w", err)
	}
	defer rows.Close()
	var items []work.WorkItem
	for rows.Next() {
		item, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// PlanRunIsActive answers the run gate. An item with no run — legacy or
// unplanned work — reports false and the domain decides what that means for
// its origin.
func (r *transactionRepository) PlanRunIsActive(ctx context.Context, planRunID string) (bool, error) {
	if strings.TrimSpace(planRunID) == "" {
		return false, nil
	}
	return queryBoolean(ctx, r.transaction,
		"SELECT EXISTS(SELECT 1 FROM plan_runs WHERE id = ? AND status = 'active')", planRunID)
}

// OpenClaimsForRun lists every unreleased claim on the run's work items, so
// closing a run can end them in the same transaction rather than leaving
// exclusive leases on work that can no longer be done.
func (r *transactionRepository) OpenClaimsForRun(ctx context.Context, planRunID string) ([]work.Claim, error) {
	rows, err := r.transaction.QueryContext(ctx, `
SELECT claim.id, claim.work_item_id, claim.actor_id, claim.version, claim.acquired_at, claim.expires_at
FROM claims claim
JOIN work_items item ON item.id = claim.work_item_id
WHERE item.plan_run_id = ? AND claim.released_at IS NULL
ORDER BY claim.id`, planRunID)
	if err != nil {
		return nil, fmt.Errorf("query open run claims: %w", err)
	}
	defer rows.Close()
	var claims []work.Claim
	for rows.Next() {
		var claim work.Claim
		var acquiredAt, expiresAt string
		if err := rows.Scan(&claim.ID, &claim.WorkItemID, &claim.ActorID, &claim.Version, &acquiredAt, &expiresAt); err != nil {
			return nil, fmt.Errorf("scan open run claim: %w", err)
		}
		if claim.AcquiredAt, err = parseTime(acquiredAt); err != nil {
			return nil, err
		}
		if claim.ExpiresAt, err = parseTime(expiresAt); err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

// PlanRunClosureFacts aggregates the run's success gates out of the
// obligations its work items already declare. There is no separate run-wide
// requirement to evaluate: a run succeeds exactly when its items do.
func (r *transactionRepository) PlanRunClosureFacts(ctx context.Context, planRunID string) (work.RunClosureFacts, error) {
	items, err := r.PlanRunWorkItems(ctx, planRunID)
	if err != nil {
		return work.RunClosureFacts{}, err
	}
	facts := work.RunClosureFacts{
		RequiredStepItemsDone:      true,
		RemainingItemsTerminal:     true,
		OutputObligationsSatisfied: true,
		ActionObligationsSatisfied: true,
	}
	for _, item := range items {
		required, err := r.planStepRequired(ctx, item.OriginPlanStepID)
		if err != nil {
			return work.RunClosureFacts{}, err
		}
		if required && item.ExecutionStatus != work.StatusDone {
			facts.RequiredStepItemsDone = false
		}
		if item.ExecutionStatus != work.StatusDone && item.ExecutionStatus != work.StatusCancelled {
			facts.RemainingItemsTerminal = false
		}
		// A cancelled item releases its own local obligations; only work that
		// actually ran still has to show its outputs and actions.
		if item.ExecutionStatus == work.StatusCancelled {
			continue
		}
		expectedSatisfied, err := r.ExpectedOutputsSatisfied(ctx, item.ID)
		if err != nil {
			return work.RunClosureFacts{}, err
		}
		requirementsSatisfied, err := r.OutputRequirementsSatisfied(ctx, item.ID)
		if err != nil {
			return work.RunClosureFacts{}, err
		}
		if !expectedSatisfied || !requirementsSatisfied {
			facts.OutputObligationsSatisfied = false
		}
		actionsSatisfied, err := r.RequiredExternalActionsSatisfied(ctx, item.ID)
		if err != nil {
			return work.RunClosureFacts{}, err
		}
		if !actionsSatisfied {
			facts.ActionObligationsSatisfied = false
		}
	}
	return facts, nil
}

func (r *transactionRepository) planStepRequired(ctx context.Context, stepID string) (bool, error) {
	if strings.TrimSpace(stepID) == "" {
		return false, nil
	}
	var required int
	if err := r.transaction.QueryRowContext(ctx, "SELECT required FROM plan_steps WHERE id = ?", stepID).Scan(&required); err != nil {
		if err == sql.ErrNoRows {
			return false, ports.ErrNotFound
		}
		return false, fmt.Errorf("read plan step requirement: %w", err)
	}
	return required == 1, nil
}
