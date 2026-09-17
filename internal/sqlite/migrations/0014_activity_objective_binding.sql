-- Activity rows gain the objective they belong to. Before this, the
-- objective-filtered change feed matched only the objective's own events and
-- events of its work items, so questions, decisions, context records and plans
-- recorded against an objective were written to the feed without a binding
-- and silently missing from it.
--
-- The column is added in place rather than by rebuilding the table, so every
-- existing row keeps its sequence and sqlite_sequence keeps the cursor high
-- water mark. Backfilling needs UPDATE, which the append-only trigger rejects;
-- it is dropped for the backfill only and recreated unchanged before this
-- migration's transaction commits.
ALTER TABLE activity ADD COLUMN objective_id TEXT REFERENCES objectives(id) ON DELETE CASCADE;

DROP TRIGGER activity_is_append_only_update;

UPDATE activity SET objective_id = CASE
  WHEN work_item_id IS NOT NULL THEN (SELECT objective_id FROM work_items WHERE id = activity.work_item_id)
  WHEN entity_kind = 'objective' THEN (SELECT id FROM objectives WHERE id = activity.entity_id)
  WHEN entity_kind = 'plan' THEN (SELECT objective_id FROM plans WHERE id = activity.entity_id)
  WHEN entity_kind = 'question' THEN (SELECT objective_id FROM questions WHERE id = activity.entity_id)
  WHEN entity_kind = 'decision' THEN (SELECT objective_id FROM decisions WHERE id = activity.entity_id)
  WHEN entity_kind = 'context_record' THEN (SELECT objective_id FROM context_records WHERE id = activity.entity_id)
  WHEN entity_kind = 'approval' THEN (SELECT objective_id FROM approvals WHERE id = activity.entity_id)
END;

CREATE TRIGGER activity_is_append_only_update
BEFORE UPDATE ON activity
BEGIN
  SELECT RAISE(ABORT, 'activity is append-only');
END;

CREATE INDEX activity_by_objective_sequence ON activity(objective_id, sequence);
