-- Objective.Priority orders parked ideas the same way WorkItem.Priority already
-- orders ready work; existing objectives default to medium, the same default
-- the app layer applies to a newly-created one that omits it.
ALTER TABLE objectives ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium'
  CHECK (priority IN ('low', 'medium', 'high', 'urgent'));

-- Appetite is an Objective's optional Measure: what the work is worth
-- spending, set before it starts. The zero value across all three columns
-- means "no appetite recorded" — the same convention Measure uses in Go.
ALTER TABLE objectives ADD COLUMN appetite_value REAL NOT NULL DEFAULT 0;
ALTER TABLE objectives ADD COLUMN appetite_unit TEXT NOT NULL DEFAULT '';
ALTER TABLE objectives ADD COLUMN appetite_basis TEXT NOT NULL DEFAULT ''
  CHECK (appetite_basis IN ('', 'estimated', 'measured'));

-- A WorkItem's optional Measure sits beside its coarse EstimatedScope hint;
-- the zero value across all three columns means "no measure recorded".
ALTER TABLE work_items ADD COLUMN measure_value REAL NOT NULL DEFAULT 0;
ALTER TABLE work_items ADD COLUMN measure_unit TEXT NOT NULL DEFAULT '';
ALTER TABLE work_items ADD COLUMN measure_basis TEXT NOT NULL DEFAULT ''
  CHECK (measure_basis IN ('', 'estimated', 'measured'));

-- non_goal and affected are new ContextRecord kinds sharing the proposed ->
-- accepted -> waived lifecycle requirement, constraint and risk already use.
-- The rebuild mirrors migration 0012's approach: SQLite cannot ALTER a CHECK
-- constraint in place, so the table is recreated. Every column definition,
-- constraint and index below is copied from migration 0002's original
-- CREATE TABLE unchanged except the kind CHECK's widened list — migration
-- 0012 shipped with one index silently dropped by its own rebuild, so this
-- one is checked against the original column by column instead of redesigned.
CREATE TABLE context_records_new (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN (
    'requirement', 'constraint', 'assumption', 'finding', 'risk', 'success_metric',
    'non_goal', 'affected'
  )),
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  confidence TEXT NOT NULL DEFAULT '',
  source_uri TEXT NOT NULL DEFAULT '',
  supersedes_id TEXT REFERENCES context_records(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  created_by TEXT NOT NULL,
  updated_by TEXT
);

INSERT INTO context_records_new
  (id, objective_id, work_item_id, kind, title, body, status, confidence, source_uri,
   supersedes_id, version, created_at, updated_at, created_by, updated_by)
SELECT id, objective_id, work_item_id, kind, title, body, status, confidence, source_uri,
       supersedes_id, version, created_at, updated_at, created_by, updated_by
FROM context_records;

DROP TABLE context_records;
ALTER TABLE context_records_new RENAME TO context_records;

CREATE INDEX context_by_objective_kind ON context_records(objective_id, kind, status);
