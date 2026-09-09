package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/dennisschroeder/throughline/internal/ports"
)

type effectTable struct {
	table    string
	kind     string
	id       string
	version  string
	kindExpr string
}

var effectTables = []effectTable{
	{table: "objectives", kind: "objective", id: "NEW.id", version: "NEW.version"},
	{table: "plans", kind: "plan", id: "NEW.id", version: "NEW.version"},
	{table: "work_items", kind: "work_item", id: "NEW.id", version: "NEW.version"},
	{table: "output_profiles", kind: "output_profile", id: "NEW.id", version: "NEW.state_version"},
	{table: "expected_outputs", kind: "expected_output", id: "NEW.id", version: "NEW.version"},
	{table: "context_records", kind: "context_record", id: "NEW.id", version: "NEW.version"},
	{table: "questions", kind: "question", id: "NEW.id", version: "NEW.version"},
	{table: "decisions", kind: "decision", id: "NEW.id", version: "NEW.version"},
	{table: "approvals", id: "NEW.id", version: "NEW.version", kindExpr: "CASE WHEN NEW.external_action_id IS NOT NULL THEN 'action_approval' WHEN NEW.work_item_id IS NOT NULL AND NEW.approved_for_actor_id IS NOT NULL THEN 'execution_approval' ELSE 'approval' END"},
	{table: "capabilities", kind: "capability", id: "NEW.slug", version: "NEW.version"},
	{table: "work_item_capabilities", kind: "work_item_capability", id: "NEW.work_item_id || ':' || NEW.capability_slug", version: "NEW.version"},
	{table: "acceptance_criteria", kind: "acceptance_criterion", id: "NEW.id", version: "NEW.version"},
	{table: "dependencies", kind: "dependency", id: "NEW.id", version: "NEW.version"},
	{table: "artifacts", kind: "artifact", id: "NEW.id", version: "NEW.version"},
	{table: "output_revisions", kind: "output_revision", id: "NEW.id", version: "NEW.state_version"},
	{table: "output_revision_artifacts", kind: "output_revision_artifact", id: "NEW.output_revision_id || ':' || NEW.artifact_id", version: "NEW.version"},
	{table: "output_requirements", kind: "output_requirement", id: "NEW.id", version: "NEW.version"},
	{table: "output_validations", kind: "validation_record", id: "NEW.id", version: "NEW.version"},
	{table: "actors", kind: "actor", id: "NEW.id", version: "NEW.version"},
	{table: "actor_capabilities", kind: "actor_capability", id: "NEW.actor_id || ':' || NEW.capability_slug", version: "NEW.version"},
	{table: "claims", kind: "claim", id: "NEW.id", version: "NEW.version"},
	{table: "progress_entries", kind: "progress_entry", id: "NEW.id", version: "NEW.version"},
	{table: "manual_blockers", kind: "manual_blocker", id: "NEW.id", version: "NEW.version"},
	{table: "external_actions", kind: "external_action", id: "NEW.id", version: "NEW.version"},
	{table: "external_action_revisions", kind: "external_action_revision", id: "NEW.external_action_id || ':' || NEW.revision", version: "NEW.version"},
	{table: "authority_grants", kind: "authority_grant", id: "NEW.id", version: "NEW.version"},
	{table: "external_action_executions", kind: "external_action_execution", id: "NEW.id", version: "NEW.version"},
}

// effectObjectCount is the temp table plus one insert, update and delete trigger
// for every collected table.
var effectObjectCount = 1 + 3*len(effectTables)

// beginEffectCollection arms the write-set collector for this transaction.
//
// The collector lives in the connection's TEMP schema, so it belongs to exactly
// one connection and cannot be observed by a concurrent transaction. It is
// rebuilt whenever the expected objects are not all present rather than tracked
// in Go state: database/sql may discard a connection and open a fresh one at any
// time, and a stale "already installed" flag would silently drop every effect on
// the replacement connection.
func (r *transactionRepository) beginEffectCollection(ctx context.Context) error {
	var installed int
	if err := r.transaction.QueryRowContext(ctx, `
SELECT count(*) FROM temp.sqlite_master
WHERE (type = 'table' AND name = 'mutation_effects')
   OR (type = 'trigger' AND name LIKE 'effects\_%' ESCAPE '\')`).Scan(&installed); err != nil {
		return fmt.Errorf("inspect mutation effect collector: %w", err)
	}
	if installed != effectObjectCount {
		if _, err := r.transaction.ExecContext(ctx, `
CREATE TEMP TABLE IF NOT EXISTS mutation_effects (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  version INTEGER NOT NULL CHECK (version > 0)
)`); err != nil {
			return fmt.Errorf("initialize mutation effect collector: %w", err)
		}
		for _, table := range effectTables {
			if err := r.createEffectTriggers(ctx, table); err != nil {
				return err
			}
		}
	}
	if _, err := r.transaction.ExecContext(ctx, "DELETE FROM mutation_effects"); err != nil {
		return fmt.Errorf("reset mutation effect collector: %w", err)
	}
	return nil
}

func (r *transactionRepository) createEffectTriggers(ctx context.Context, table effectTable) error {
	name := strings.ReplaceAll(table.table, "-", "_")
	deleteID := strings.ReplaceAll(table.id, "NEW.", "OLD.")
	deleteVersion := strings.ReplaceAll(table.version, "NEW.", "OLD.")
	insertKind := fmt.Sprintf("%q", table.kind)
	if table.kindExpr != "" {
		insertKind = table.kindExpr
	}
	deleteKind := strings.ReplaceAll(insertKind, "NEW.", "OLD.")
	statements := []string{
		fmt.Sprintf("CREATE TEMP TRIGGER IF NOT EXISTS effects_%s_insert AFTER INSERT ON main.%s BEGIN INSERT INTO mutation_effects(kind, entity_id, version) VALUES (%s, %s, %s); END", name, table.table, insertKind, table.id, table.version),
		fmt.Sprintf("CREATE TEMP TRIGGER IF NOT EXISTS effects_%s_update AFTER UPDATE ON main.%s BEGIN INSERT INTO mutation_effects(kind, entity_id, version) VALUES (%s, %s, %s); END", name, table.table, insertKind, table.id, table.version),
		fmt.Sprintf("CREATE TEMP TRIGGER IF NOT EXISTS effects_%s_delete AFTER DELETE ON main.%s BEGIN INSERT INTO mutation_effects(kind, entity_id, version) VALUES (%s, %s, %s); END", name, table.table, deleteKind, deleteID, deleteVersion),
	}
	for _, statement := range statements {
		if _, err := r.transaction.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("create %s mutation effect trigger: %w", table.table, err)
		}
	}
	return nil
}

func (r *transactionRepository) CollectEffects(ctx context.Context) ([]ports.Effect, error) {
	rows, err := r.transaction.QueryContext(ctx, `
WITH latest AS (
  SELECT kind, entity_id, MAX(sequence) AS sequence, MIN(sequence) AS first_sequence
  FROM mutation_effects
  GROUP BY kind, entity_id
)
SELECT effect.kind, effect.entity_id, effect.version
FROM latest
JOIN mutation_effects effect ON effect.sequence = latest.sequence
ORDER BY latest.first_sequence`)
	if err != nil {
		return nil, fmt.Errorf("query mutation effects: %w", err)
	}
	defer rows.Close()
	effects := make([]ports.Effect, 0)
	for rows.Next() {
		var effect ports.Effect
		if err := rows.Scan(&effect.Kind, &effect.ID, &effect.Version); err != nil {
			return nil, fmt.Errorf("scan mutation effect: %w", err)
		}
		effects = append(effects, effect)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mutation effects: %w", err)
	}
	return effects, nil
}
