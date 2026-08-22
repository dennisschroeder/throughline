CREATE TABLE external_actions (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  action_type TEXT NOT NULL,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  title TEXT NOT NULL,
  rationale TEXT NOT NULL DEFAULT '',
  current_revision INTEGER NOT NULL DEFAULT 1 CHECK (current_revision > 0),
  state TEXT NOT NULL CHECK (state IN ('proposed', 'authorized', 'executing', 'succeeded', 'failed', 'rejected', 'cancelled', 'expired')),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_by TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  updated_by TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE external_action_revisions (
  external_action_id TEXT NOT NULL REFERENCES external_actions(id) ON DELETE CASCADE,
  revision INTEGER NOT NULL CHECK (revision > 0),
  authorization_subject_json TEXT NOT NULL CHECK (json_valid(authorization_subject_json)),
  authorization_subject_hash TEXT NOT NULL,
  proposed_by TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  proposed_at TEXT NOT NULL,
  PRIMARY KEY (external_action_id, revision),
  UNIQUE (external_action_id, authorization_subject_hash),
  UNIQUE (external_action_id, revision, authorization_subject_hash)
);

CREATE TABLE approvals_new (
  id TEXT PRIMARY KEY,
  objective_id TEXT REFERENCES objectives(id) ON DELETE CASCADE,
  plan_id TEXT REFERENCES plans(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  output_profile_id TEXT REFERENCES output_profiles(id) ON DELETE CASCADE,
  output_revision_id TEXT REFERENCES output_revisions(id) ON DELETE CASCADE,
  external_action_id TEXT,
  external_action_revision INTEGER,
  approved_for_actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
  authorization_subject_hash TEXT,
  constraints_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(constraints_json)),
  expires_at TEXT,
  request TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('requested', 'approved', 'rejected', 'revoked')),
  requested_by TEXT NOT NULL,
  requested_at TEXT NOT NULL,
  resolved_by TEXT,
  resolved_at TEXT,
  rationale TEXT NOT NULL DEFAULT '',
  CHECK (
    (plan_id IS NOT NULL) + (work_item_id IS NOT NULL) + (output_profile_id IS NOT NULL) +
    (output_revision_id IS NOT NULL) + (external_action_id IS NOT NULL) = 1
  ),
  CHECK (
    external_action_id IS NULL OR (
      external_action_revision IS NOT NULL AND approved_for_actor_id IS NOT NULL AND
      authorization_subject_hash IS NOT NULL
    )
  ),
  FOREIGN KEY (external_action_id, external_action_revision, authorization_subject_hash)
    REFERENCES external_action_revisions(external_action_id, revision, authorization_subject_hash)
);

INSERT INTO approvals_new
  (id, objective_id, plan_id, request, status, requested_by, requested_at, resolved_by, resolved_at, rationale)
SELECT id, objective_id, plan_id, request, status, requested_by, requested_at, resolved_by, resolved_at, rationale
FROM approvals;

DROP TABLE approvals;
ALTER TABLE approvals_new RENAME TO approvals;
CREATE INDEX approvals_by_plan ON approvals(plan_id, status);
CREATE INDEX approvals_by_external_action ON approvals(external_action_id, external_action_revision, status);
CREATE UNIQUE INDEX one_action_approval_per_revision
  ON approvals(external_action_id, external_action_revision)
  WHERE external_action_id IS NOT NULL;

CREATE TABLE authority_grants (
  id TEXT PRIMARY KEY,
  external_action_id TEXT NOT NULL,
  external_action_revision INTEGER NOT NULL,
  principal_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  authorization_subject_hash TEXT NOT NULL,
  constraints_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(constraints_json)),
  source_approval_id TEXT NOT NULL UNIQUE REFERENCES approvals(id) ON DELETE RESTRICT,
  granted_by TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  granted_at TEXT NOT NULL,
  expires_at TEXT,
  revoked_by TEXT REFERENCES actors(id) ON DELETE RESTRICT,
  revoked_at TEXT,
  revocation_reason TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (external_action_id, external_action_revision, authorization_subject_hash)
    REFERENCES external_action_revisions(external_action_id, revision, authorization_subject_hash)
);

CREATE INDEX authority_grants_by_action_principal
  ON authority_grants(external_action_id, external_action_revision, principal_actor_id, revoked_at, expires_at);

CREATE TABLE external_action_executions (
  id TEXT PRIMARY KEY,
  external_action_id TEXT NOT NULL,
  external_action_revision INTEGER NOT NULL,
  principal_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  authority_grant_id TEXT NOT NULL REFERENCES authority_grants(id) ON DELETE RESTRICT,
  state TEXT NOT NULL CHECK (state IN ('executing', 'succeeded', 'failed')),
  started_at TEXT NOT NULL,
  finished_at TEXT,
  result_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(result_json)),
  evidence_artifact_id TEXT REFERENCES artifacts(id) ON DELETE RESTRICT,
  FOREIGN KEY (external_action_id, external_action_revision)
    REFERENCES external_action_revisions(external_action_id, revision),
  CHECK (
    (state = 'executing' AND finished_at IS NULL)
    OR (state IN ('succeeded', 'failed') AND finished_at IS NOT NULL)
  ),
  CHECK (state <> 'succeeded' OR evidence_artifact_id IS NOT NULL)
);

CREATE UNIQUE INDEX one_executing_attempt_per_action_revision
  ON external_action_executions(external_action_id, external_action_revision) WHERE state = 'executing';
CREATE INDEX external_actions_by_item_state ON external_actions(work_item_id, state, updated_at);

CREATE TRIGGER external_action_revision_is_immutable_update
BEFORE UPDATE ON external_action_revisions
BEGIN
  SELECT RAISE(ABORT, 'external action revision is immutable');
END;

CREATE TRIGGER external_action_revision_is_immutable_delete
BEFORE DELETE ON external_action_revisions
BEGIN
  SELECT RAISE(ABORT, 'external action revision is immutable');
END;

CREATE TRIGGER authority_grant_is_append_only_delete
BEFORE DELETE ON authority_grants
BEGIN
  SELECT RAISE(ABORT, 'authority grant is append-only');
END;

CREATE TRIGGER approval_transition_is_valid
BEFORE UPDATE ON approvals
WHEN NEW.id <> OLD.id
  OR NEW.objective_id IS NOT OLD.objective_id
  OR NEW.plan_id IS NOT OLD.plan_id
  OR NEW.work_item_id IS NOT OLD.work_item_id
  OR NEW.output_profile_id IS NOT OLD.output_profile_id
  OR NEW.output_revision_id IS NOT OLD.output_revision_id
  OR NEW.external_action_id IS NOT OLD.external_action_id
  OR NEW.external_action_revision IS NOT OLD.external_action_revision
  OR NEW.approved_for_actor_id IS NOT OLD.approved_for_actor_id
  OR NEW.authorization_subject_hash IS NOT OLD.authorization_subject_hash
  OR NEW.constraints_json <> OLD.constraints_json
  OR NEW.expires_at IS NOT OLD.expires_at
  OR NEW.request <> OLD.request
  OR NEW.requested_by <> OLD.requested_by
  OR NEW.requested_at <> OLD.requested_at
  OR (OLD.status = 'requested' AND NEW.status NOT IN ('approved', 'rejected'))
  OR (OLD.status = 'approved' AND NEW.status <> 'revoked')
  OR OLD.status NOT IN ('requested', 'approved')
BEGIN
  SELECT RAISE(ABORT, 'approval transition is invalid');
END;

CREATE TRIGGER authority_grant_revocation_is_one_way
BEFORE UPDATE ON authority_grants
WHEN NEW.id <> OLD.id
  OR NEW.external_action_id <> OLD.external_action_id
  OR NEW.external_action_revision <> OLD.external_action_revision
  OR NEW.principal_actor_id <> OLD.principal_actor_id
  OR NEW.authorization_subject_hash <> OLD.authorization_subject_hash
  OR NEW.constraints_json <> OLD.constraints_json
  OR NEW.source_approval_id <> OLD.source_approval_id
  OR NEW.granted_by <> OLD.granted_by
  OR NEW.granted_at <> OLD.granted_at
  OR NEW.expires_at IS NOT OLD.expires_at
  OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS NULL)
BEGIN
  SELECT RAISE(ABORT, 'authority grant may only be revoked');
END;

CREATE TRIGGER external_action_execution_is_append_only_delete
BEFORE DELETE ON external_action_executions
BEGIN
  SELECT RAISE(ABORT, 'external action execution is append-only');
END;

CREATE TRIGGER external_action_execution_transition_is_valid
BEFORE UPDATE ON external_action_executions
WHEN NEW.id <> OLD.id
  OR NEW.external_action_id <> OLD.external_action_id
  OR NEW.external_action_revision <> OLD.external_action_revision
  OR NEW.principal_actor_id <> OLD.principal_actor_id
  OR NEW.authority_grant_id <> OLD.authority_grant_id
  OR OLD.state <> 'executing'
  OR NEW.state NOT IN ('succeeded', 'failed')
  OR OLD.finished_at IS NOT NULL
  OR NEW.finished_at IS NULL
BEGIN
  SELECT RAISE(ABORT, 'external action execution transition is invalid');
END;
