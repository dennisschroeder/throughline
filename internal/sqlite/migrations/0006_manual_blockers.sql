CREATE TABLE manual_blockers (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  reason TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active', 'resolved')),
  created_by TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  resolved_by TEXT REFERENCES actors(id) ON DELETE RESTRICT,
  resolved_at TEXT,
  resolution TEXT NOT NULL DEFAULT ''
);

CREATE INDEX manual_blockers_by_item_status ON manual_blockers(work_item_id, status);
