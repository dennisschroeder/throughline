-- A validation record can now bind to a work item as well as an output
-- revision, so review of work that produces no output revision has somewhere
-- to live; a work item declares the reviews done waits for.
--
-- output_validations is rebuilt because output_revision_id loses NOT NULL and
-- the table gains a subject CHECK, neither of which ALTER TABLE can do. No
-- table references it. Its append-only triggers belong to the old table and
-- are dropped with it, so they are recreated unchanged below.
ALTER TABLE work_items ADD COLUMN review_requirements_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(review_requirements_json));

ALTER TABLE output_validations RENAME TO output_validations_old;

CREATE TABLE output_validations (
  id TEXT PRIMARY KEY,
  output_revision_id TEXT REFERENCES output_revisions(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  criterion_ref TEXT NOT NULL,
  validator_kind TEXT NOT NULL CHECK (validator_kind IN ('structure', 'schema', 'evaluation', 'provenance', 'human_review', 'policy', 'probe', 'successor_use')),
  verdict TEXT NOT NULL CHECK (verdict IN ('passed', 'failed', 'waived')),
  score REAL,
  verifier_actor_id TEXT NOT NULL,
  evidence_artifact_id TEXT REFERENCES artifacts(id) ON DELETE RESTRICT,
  details_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  subject_sequence INTEGER NOT NULL DEFAULT 0 CHECK (subject_sequence >= 0),
  degraded INTEGER NOT NULL DEFAULT 0 CHECK (degraded IN (0, 1)),
  CHECK (json_valid(details_json)),
  CHECK ((output_revision_id IS NOT NULL) + (work_item_id IS NOT NULL) = 1)
);

-- Ordered by rowid so the rebuilt table keeps insertion order, which is how
-- the latest record for a requirement is found.
INSERT INTO output_validations
  (id, output_revision_id, criterion_ref, validator_kind, verdict, score, verifier_actor_id,
   evidence_artifact_id, details_json, created_at, version)
SELECT id, output_revision_id, criterion_ref, validator_kind, verdict, score, verifier_actor_id,
       evidence_artifact_id, details_json, created_at, version
FROM output_validations_old
ORDER BY rowid;

DROP TABLE output_validations_old;

CREATE INDEX output_validations_by_revision ON output_validations(output_revision_id, validator_kind, created_at);
CREATE INDEX output_validations_by_work_item ON output_validations(work_item_id, criterion_ref, validator_kind);

CREATE TRIGGER output_validation_is_append_only_update
BEFORE UPDATE ON output_validations
BEGIN
  SELECT RAISE(ABORT, 'output validation is append-only');
END;

CREATE TRIGGER output_validation_is_append_only_delete
BEFORE DELETE ON output_validations
BEGIN
  SELECT RAISE(ABORT, 'output validation is append-only');
END;
