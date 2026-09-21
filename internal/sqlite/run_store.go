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

// MaterializedKeyNamespaceTaken reports whether any existing work item already
// occupies a key this step's runs would produce. New work items cannot take one
// — the separator is reserved — so this only ever finds work that predates the
// reservation, and finding it when the plan is written beats failing every run
// of an approved definition nobody can then change.
func (r *transactionRepository) MaterializedKeyNamespaceTaken(ctx context.Context, stepKey string) (bool, error) {
	// The prefix is compared with substr rather than matched with GLOB or
	// LIKE: a step key containing a wildcard or a character class would
	// otherwise be read as a pattern and match the wrong keys while missing
	// its own.
	namespace := work.MaterializedKeyNamespace(stepKey)
	return queryBoolean(ctx, r.transaction,
		"SELECT EXISTS(SELECT 1 FROM work_items WHERE substr(key, 1, length(?)) = ?)", namespace, namespace)
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

// planRunColumns is shared by the plain selects and the summary join, so a
// column added later cannot be silently missing from one of them.
var planRunColumns = []string{"id", "objective_id", "plan_id", "run_key", "sequence", "status",
	"binding_fingerprint", "started_by", "started_at", "closed_by", "closed_at", "close_reason",
	"version", "created_at", "updated_at"}

var planRunSelect = "SELECT " + strings.Join(planRunColumns, ", ") + " FROM plan_runs"

func prefixedPlanRunColumns(alias string) string {
	return prefixedColumns(alias, planRunColumns)
}

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
	targets, finish := planRunScanTargets(&run)
	if err := row.Scan(targets...); err != nil {
		return work.PlanRun{}, err
	}
	if err := finish(); err != nil {
		return work.PlanRun{}, err
	}
	return run, nil
}

// planRunScanTargets returns the destinations for planRunColumns, in order,
// and the conversion to run once they are filled. The summary join appends its
// aggregates after these, so the two readers cannot drift apart.
func planRunScanTargets(run *work.PlanRun) ([]any, func() error) {
	var closedBy, closedAt, closeReason sql.NullString
	var startedAt, createdAt, updatedAt string
	targets := []any{&run.ID, &run.ObjectiveID, &run.PlanID, &run.RunKey, &run.Sequence, &run.Status,
		&run.BindingFingerprint, &run.StartedBy, &startedAt, &closedBy, &closedAt, &closeReason,
		&run.Version, &createdAt, &updatedAt}
	return targets, func() error {
		run.ClosedBy = closedBy.String
		run.CloseReason = closeReason.String
		var err error
		if run.StartedAt, err = parseTime(startedAt); err != nil {
			return err
		}
		if closedAt.Valid {
			if run.ClosedAt, err = parseTime(closedAt.String); err != nil {
				return err
			}
		}
		if run.CreatedAt, err = parseTime(createdAt); err != nil {
			return err
		}
		run.UpdatedAt, err = parseTime(updatedAt)
		return err
	}
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
// the same one. What makes that safe is the transaction itself: writes begin
// immediate, so two creations serialize and the second reads the first's
// committed sequence and capacity. UNIQUE(objective_id, sequence) is the
// backstop behind that, not the mechanism — it would catch two writers that
// somehow did agree on a number, which the write lock is there to prevent.
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
		// Cancelling an item releases its own local obligations — its
		// criteria, outputs, output requirements and the actions that never
		// started. An effect that did start is not released by cancelling the
		// step that asked for it: it happened, and its result is still owed.
		if item.ExecutionStatus == work.StatusCancelled {
			settled, err := r.StartedExternalActionsSettled(ctx, item.ID)
			if err != nil {
				return work.RunClosureFacts{}, err
			}
			if !settled {
				facts.ActionObligationsSatisfied = false
			}
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

// ListPlanRuns answers "which executions exist here" in one bounded page,
// newest first. It resolves no latest run of its own: the caller filters and
// chooses, which is the whole point of the run being addressable.
func (s *Store) ListPlanRuns(ctx context.Context, filter ports.PlanRunFilter) (ports.PlanRunPage, error) {
	var page ports.PlanRunPage
	err := s.withinReadTransaction(ctx, func(reader sqlReader) error {
		var err error
		page, err = s.listPlanRuns(ctx, reader, filter)
		return err
	})
	return page, err
}

const maxPlanRunPage = 100

func (s *Store) listPlanRuns(ctx context.Context, reader sqlReader, filter ports.PlanRunFilter) (ports.PlanRunPage, error) {
	conditions := []string{"1 = 1"}
	arguments := []any{}
	if strings.TrimSpace(filter.ObjectiveID) != "" {
		conditions = append(conditions, "run.objective_id = ?")
		arguments = append(arguments, filter.ObjectiveID)
	}
	if strings.TrimSpace(filter.PlanID) != "" {
		conditions = append(conditions, "run.plan_id = ?")
		arguments = append(arguments, filter.PlanID)
	}
	if len(filter.Statuses) != 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			if !work.ValidPlanRunStatus(status) {
				return ports.PlanRunPage{}, fmt.Errorf("invalid plan run status %q", status)
			}
			placeholders = append(placeholders, "?")
			arguments = append(arguments, string(status))
		}
		conditions = append(conditions, "run.status IN ("+strings.Join(placeholders, ", ")+")")
	}
	where := " WHERE " + strings.Join(conditions, " AND ")

	// The total is read under the same snapshot as the page, so has_more and
	// the count cannot describe two different sets of runs.
	var total int
	if err := reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM plan_runs run"+where, arguments...).Scan(&total); err != nil {
		return ports.PlanRunPage{}, fmt.Errorf("count plan runs: %w", err)
	}
	limit := filter.Limit
	if limit <= 0 || limit > maxPlanRunPage {
		limit = maxPlanRunPage
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		return ports.PlanRunPage{}, fmt.Errorf("plan run listing offset %d is past the end of %d runs", offset, total)
	}
	// The counts and the revision come from the same statement as the runs
	// rather than from a query per row: a page of a hundred runs is one read,
	// not two hundred and one. They are aggregates of the run's own work, so
	// a run with no work reports zeros rather than dropping out of the page.
	rows, err := reader.QueryContext(ctx, `
SELECT `+prefixedPlanRunColumns("run")+`, plan.revision,
       COUNT(item.id),
       COALESCE(SUM(CASE WHEN item.execution_status = 'done' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN item.execution_status = 'cancelled' THEN 1 ELSE 0 END), 0)
FROM plan_runs run
JOIN plans plan ON plan.id = run.plan_id
LEFT JOIN work_items item ON item.plan_run_id = run.id
`+where+`
GROUP BY run.id
ORDER BY run.created_at DESC, run.sequence DESC, run.id DESC
LIMIT ? OFFSET ?`, append(append([]any{}, arguments...), limit, offset)...)
	if err != nil {
		return ports.PlanRunPage{}, fmt.Errorf("query plan runs: %w", err)
	}
	defer rows.Close()
	page := ports.PlanRunPage{Total: total}
	for rows.Next() {
		summary, err := scanPlanRunSummary(rows)
		if err != nil {
			return ports.PlanRunPage{}, err
		}
		page.Runs = append(page.Runs, summary)
	}
	if err := rows.Err(); err != nil {
		return ports.PlanRunPage{}, err
	}
	page.HasMore = offset+len(page.Runs) < total
	return page, nil
}

// scanPlanRunSummary reads a run and the aggregates of its own work. The
// numbers say how much of the run is still open; they never imply a status,
// because a run's status is only ever what someone explicitly set.
func scanPlanRunSummary(row scanner) (ports.PlanRunSummary, error) {
	var summary ports.PlanRunSummary
	targets, finish := planRunScanTargets(&summary.Run)
	targets = append(targets, &summary.PlanRevision, &summary.WorkItems, &summary.Done, &summary.Cancelled)
	if err := row.Scan(targets...); err != nil {
		return ports.PlanRunSummary{}, fmt.Errorf("scan plan run summary: %w", err)
	}
	if err := finish(); err != nil {
		return ports.PlanRunSummary{}, err
	}
	return summary, nil
}
