CREATE TABLE acceptance_criteria (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL CHECK (ordinal > 0),
  text TEXT NOT NULL,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  status TEXT NOT NULL CHECK (status IN ('pending', 'satisfied', 'waived')),
  resolved_at TEXT,
  resolved_by TEXT,
  resolution_rationale TEXT NOT NULL DEFAULT '',
  UNIQUE (work_item_id, ordinal)
);

CREATE TABLE dependencies (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  depends_on_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL CHECK (kind IN ('hard', 'soft', 'related')),
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL,
  CHECK (work_item_id <> depends_on_item_id),
  UNIQUE (work_item_id, depends_on_item_id, kind)
);

CREATE TABLE artifacts (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  uri TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  attached_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (work_item_id, uri),
  CHECK (json_valid(metadata_json))
);

CREATE TABLE output_revisions (
  id TEXT PRIMARY KEY,
  expected_output_id TEXT NOT NULL REFERENCES expected_outputs(id) ON DELETE CASCADE,
  output_profile_id TEXT NOT NULL REFERENCES output_profiles(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL CHECK (revision > 0),
  content_digest TEXT NOT NULL DEFAULT '',
  acceptance_state TEXT NOT NULL DEFAULT 'produced' CHECK (acceptance_state IN ('produced', 'accepted', 'rejected', 'superseded')),
  produced_by TEXT NOT NULL,
  produced_at TEXT NOT NULL,
  accepted_by TEXT,
  accepted_at TEXT,
  acceptance_reason TEXT NOT NULL DEFAULT '',
	bindings_finalized INTEGER NOT NULL DEFAULT 0 CHECK (bindings_finalized IN (0, 1)),
  UNIQUE (expected_output_id, revision)
);

CREATE TABLE output_revision_artifacts (
  output_revision_id TEXT NOT NULL REFERENCES output_revisions(id) ON DELETE CASCADE,
  artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE RESTRICT,
  role TEXT NOT NULL DEFAULT 'primary',
  PRIMARY KEY (output_revision_id, artifact_id)
);

CREATE TABLE output_requirements (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  required_output_revision_id TEXT REFERENCES output_revisions(id) ON DELETE RESTRICT,
  required_profile_name TEXT,
  version_constraint TEXT,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  note TEXT NOT NULL DEFAULT '',
  CHECK (
    (required_output_revision_id IS NOT NULL AND required_profile_name IS NULL AND version_constraint IS NULL)
    OR
    (required_output_revision_id IS NULL AND required_profile_name IS NOT NULL AND version_constraint IS NOT NULL)
  )
);

CREATE TABLE output_validations (
  id TEXT PRIMARY KEY,
  output_revision_id TEXT NOT NULL REFERENCES output_revisions(id) ON DELETE CASCADE,
  criterion_ref TEXT NOT NULL,
  validator_kind TEXT NOT NULL CHECK (validator_kind IN ('structure', 'schema', 'evaluation', 'provenance', 'human_review', 'policy', 'probe', 'successor_use')),
  verdict TEXT NOT NULL CHECK (verdict IN ('passed', 'failed', 'waived')),
  score REAL,
  verifier_actor_id TEXT NOT NULL,
  evidence_artifact_id TEXT REFERENCES artifacts(id) ON DELETE RESTRICT,
  details_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  CHECK (json_valid(details_json))
);

CREATE TABLE activity (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  entity_kind TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  actor_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  summary TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  CHECK (json_valid(payload_json))
);

CREATE INDEX acceptance_criteria_by_item ON acceptance_criteria(work_item_id, ordinal);
CREATE INDEX dependencies_by_dependent ON dependencies(work_item_id, kind);
CREATE INDEX dependencies_by_prerequisite ON dependencies(depends_on_item_id, kind);
CREATE INDEX output_revisions_by_expected_state ON output_revisions(expected_output_id, acceptance_state, revision);
CREATE INDEX output_requirements_by_item ON output_requirements(work_item_id, required);
CREATE INDEX output_validations_by_revision ON output_validations(output_revision_id, validator_kind, created_at);
CREATE INDEX activity_by_sequence ON activity(sequence);
CREATE INDEX activity_by_item_sequence ON activity(work_item_id, sequence);

CREATE TRIGGER output_revision_content_is_immutable
BEFORE UPDATE OF expected_output_id, output_profile_id, revision, content_digest, produced_by, produced_at ON output_revisions
BEGIN
  SELECT RAISE(ABORT, 'output revision content is immutable');
END;

CREATE TRIGGER output_revision_is_not_deletable
BEFORE DELETE ON output_revisions
BEGIN
  SELECT RAISE(ABORT, 'output revision is immutable');
END;

CREATE TRIGGER output_revision_artifact_is_immutable_update
BEFORE UPDATE ON output_revision_artifacts
BEGIN
  SELECT RAISE(ABORT, 'output revision artifact binding is immutable');
END;

CREATE TRIGGER output_revision_artifact_is_finalized_insert
BEFORE INSERT ON output_revision_artifacts
WHEN (SELECT bindings_finalized FROM output_revisions WHERE id = NEW.output_revision_id) = 1
BEGIN
  SELECT RAISE(ABORT, 'output revision artifact bindings are finalized');
END;

CREATE TRIGGER output_revision_bindings_cannot_reopen
BEFORE UPDATE OF bindings_finalized ON output_revisions
WHEN OLD.bindings_finalized = 1 OR NEW.bindings_finalized <> 1
BEGIN
  SELECT RAISE(ABORT, 'output revision artifact bindings are finalized');
END;

CREATE TRIGGER output_revision_artifact_is_immutable_delete
BEFORE DELETE ON output_revision_artifacts
BEGIN
  SELECT RAISE(ABORT, 'output revision artifact binding is immutable');
END;

CREATE TRIGGER artifact_is_immutable_update
BEFORE UPDATE ON artifacts
BEGIN
  SELECT RAISE(ABORT, 'artifact reference is immutable');
END;

CREATE TRIGGER artifact_is_immutable_delete
BEFORE DELETE ON artifacts
BEGIN
  SELECT RAISE(ABORT, 'artifact reference is immutable');
END;

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

CREATE TRIGGER activity_is_append_only_update
BEFORE UPDATE ON activity
BEGIN
  SELECT RAISE(ABORT, 'activity is append-only');
END;

CREATE TRIGGER activity_is_append_only_delete
BEFORE DELETE ON activity
BEGIN
  SELECT RAISE(ABORT, 'activity is append-only');
END;
