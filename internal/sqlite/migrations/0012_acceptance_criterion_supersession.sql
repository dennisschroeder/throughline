-- A wrong acceptance criterion could previously only be waived, which records
-- that the condition was excused rather than that it was replaced, or left in
-- place to block completion forever. Superseding keeps the predecessor exactly
-- as it was and points the replacement at it.
--
-- The table is rebuilt rather than altered: the status CHECK has to admit a
-- fourth value, and the unique ordinal has to stop applying to history, neither
-- of which ALTER TABLE can do.
CREATE TABLE acceptance_criteria_new (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL CHECK (ordinal > 0),
  text TEXT NOT NULL,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  status TEXT NOT NULL CHECK (status IN ('pending', 'satisfied', 'waived', 'superseded')),
  resolved_at TEXT,
  resolved_by TEXT,
  resolution_rationale TEXT NOT NULL DEFAULT '',
  supersedes_id TEXT REFERENCES acceptance_criteria(id),
  supersession_reason TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)
);

INSERT INTO acceptance_criteria_new
  (id, work_item_id, ordinal, text, required, status, resolved_at, resolved_by, resolution_rationale, version)
SELECT id, work_item_id, ordinal, text, required, status, resolved_at, resolved_by, resolution_rationale, version
FROM acceptance_criteria;

DROP TABLE acceptance_criteria;
ALTER TABLE acceptance_criteria_new RENAME TO acceptance_criteria;

-- Recreated: DROP TABLE took it with the old table, and the two partial indexes
-- below cannot serve the queries that actually run, because neither
-- listAcceptanceCriteria nor the completion gate carries the status predicate
-- they are partial on. Without this both fall back to a full table scan.
CREATE INDEX acceptance_criteria_by_item ON acceptance_criteria(work_item_id, ordinal);

-- An ordinal identifies a criterion among the ones that still count. A
-- superseded criterion keeps the ordinal it had, so its replacement can carry
-- the same one and the pair reads as one revised condition.
CREATE UNIQUE INDEX acceptance_criteria_active_ordinal
  ON acceptance_criteria(work_item_id, ordinal) WHERE status <> 'superseded';

-- A criterion supersedes at most one predecessor, and a predecessor is replaced
-- at most once, so the chain from a live criterion back through its history is
-- unambiguous.
CREATE UNIQUE INDEX acceptance_criteria_one_replacement
  ON acceptance_criteria(supersedes_id) WHERE supersedes_id IS NOT NULL;
