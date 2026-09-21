-- Reusable plans and separate plan runs.
--
-- A plan stops being a batch of executable work items and becomes a reusable
-- definition: named plan_inputs it needs, plan_steps it is made of, and the
-- dependencies between those steps. A plan_run is one execution of one exact
-- approved revision; it binds its inputs immutably and materializes one fresh
-- work item per step.
--
-- This migration is purely additive. It invents nothing: no run, no run key,
-- no binding, no plan step and no activity is synthesized for work that
-- already exists. Every work item present before this migration is marked
-- origin 'legacy' and keeps exactly the behavior it had, with no run of its
-- own. That includes items with no plan: they were executable under the old
-- rules, and re-labelling them as non-executable proposals would rewrite
-- history rather than preserve it. Reusing an old plan means proposing a new
-- revision from its demonstrable contents and having that reviewed.

ALTER TABLE objectives ADD COLUMN mode TEXT NOT NULL DEFAULT 'finite' CHECK (mode IN ('finite', 'ongoing'));
ALTER TABLE objectives ADD COLUMN max_concurrent_runs INTEGER NOT NULL DEFAULT 1 CHECK (max_concurrent_runs > 0);

CREATE TABLE plan_inputs (
  id TEXT PRIMARY KEY,
  plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  required INTEGER NOT NULL DEFAULT 0 CHECK (required IN (0, 1)),
  ordinal INTEGER NOT NULL CHECK (ordinal > 0),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  UNIQUE(plan_id, name),
  UNIQUE(plan_id, ordinal)
);

-- definition_json is the step's owned child data — required capabilities,
-- acceptance criteria, expected outputs, exact output requirements and
-- external action proposals. It is a frozen snapshot copied verbatim into
-- each run's work item and is never queried across steps, so it is one
-- immutable record rather than five tables no other row references.
CREATE TABLE plan_steps (
  id TEXT PRIMARY KEY,
  plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  client_ref TEXT NOT NULL,
  parent_step_id TEXT REFERENCES plan_steps(id) ON DELETE RESTRICT,
  key TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  priority TEXT NOT NULL CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
  estimated_scope TEXT NOT NULL CHECK (estimated_scope IN ('xs', 'small', 'medium', 'large', 'unknown')),
  execution_policy TEXT NOT NULL CHECK (execution_policy IN ('human_only', 'agent_may_propose', 'approval_required', 'autonomous_with_report')),
  required_actor_kind TEXT NOT NULL CHECK (required_actor_kind IN ('any', 'human', 'agent')),
  review_requirements_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(review_requirements_json)),
  ordinal INTEGER NOT NULL CHECK (ordinal > 0),
  definition_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(definition_json)),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  UNIQUE(plan_id, client_ref),
  UNIQUE(plan_id, ordinal)
);

-- A step key is globally unique, like a work item key, because each run
-- materializes its work item as "<step key>/<run sequence>" and work_items.key
-- is itself globally unique. Two objectives that both declared a step called
-- "research" would otherwise collide on their first runs, and the failure
-- would surface as an opaque uniqueness error at run creation rather than when
-- the plan was written.
CREATE UNIQUE INDEX plan_steps_key_is_unique ON plan_steps(key);

CREATE TABLE plan_step_dependencies (
  plan_step_id TEXT NOT NULL REFERENCES plan_steps(id) ON DELETE CASCADE,
  depends_on_step_id TEXT NOT NULL REFERENCES plan_steps(id) ON DELETE CASCADE,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  PRIMARY KEY (plan_step_id, depends_on_step_id),
  CHECK (plan_step_id <> depends_on_step_id)
);

-- A run carries one status, not a lifecycle plus an outcome: 'finished' only
-- ever meant succeeded or failed. 'cancelled' asserts no outcome.
--
-- run_key is objective-unique and opaque. binding_fingerprint records what the
-- run was created from, so a replayed key can be told apart from the same key
-- reused with different content.
CREATE TABLE plan_runs (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
  run_key TEXT NOT NULL,
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  status TEXT NOT NULL CHECK (status IN ('active', 'succeeded', 'failed', 'cancelled')),
  binding_fingerprint TEXT NOT NULL,
  started_by TEXT NOT NULL,
  started_at TEXT NOT NULL,
  closed_by TEXT,
  closed_at TEXT,
  close_reason TEXT,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(objective_id, run_key),
  UNIQUE(objective_id, sequence),
  CHECK ((status = 'active') = (closed_at IS NULL))
);

-- A revision proposed from what a run turned up records which run that was.
-- It is provenance and nothing more: no observation of a run ever changes a
-- plan, and a revision still only becomes runnable by being approved.
ALTER TABLE plans ADD COLUMN derived_from_plan_run_id TEXT REFERENCES plan_runs(id) ON DELETE SET NULL;

CREATE INDEX plan_runs_by_objective_status ON plan_runs(objective_id, status, sequence);
CREATE INDEX plan_runs_by_plan ON plan_runs(plan_id, sequence);

-- A binding is written when its run is created and never updated or deleted;
-- a wrong binding is answered by cancelling the run, not by editing what a run
-- claims to have worked from. An external binding needs a stable locator plus
-- either an immutable source version or a content digest: Throughline stores
-- and shape-checks those and never fetches the source.
CREATE TABLE run_input_bindings (
  id TEXT PRIMARY KEY,
  plan_run_id TEXT NOT NULL REFERENCES plan_runs(id) ON DELETE CASCADE,
  plan_input_id TEXT NOT NULL REFERENCES plan_inputs(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('value', 'output_revision', 'external')),
  value TEXT NOT NULL DEFAULT '',
  output_revision_id TEXT REFERENCES output_revisions(id) ON DELETE RESTRICT,
  locator TEXT NOT NULL DEFAULT '',
  source_version TEXT NOT NULL DEFAULT '',
  digest TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE(plan_run_id, plan_input_id),
  CHECK (
    (kind = 'value' AND value <> '' AND output_revision_id IS NULL AND locator = '') OR
    (kind = 'output_revision' AND output_revision_id IS NOT NULL AND value = '' AND locator = '') OR
    (kind = 'external' AND locator <> '' AND (source_version <> '' OR digest <> '') AND value = '' AND output_revision_id IS NULL)
  )
);

CREATE TRIGGER run_input_binding_is_immutable_update
BEFORE UPDATE ON run_input_bindings
BEGIN
  SELECT RAISE(ABORT, 'run input binding is immutable');
END;

ALTER TABLE work_items ADD COLUMN origin TEXT NOT NULL DEFAULT 'legacy' CHECK (origin IN ('legacy', 'plan_step', 'run_local', 'unplanned'));
ALTER TABLE work_items ADD COLUMN plan_run_id TEXT REFERENCES plan_runs(id) ON DELETE RESTRICT;
ALTER TABLE work_items ADD COLUMN origin_plan_step_id TEXT REFERENCES plan_steps(id) ON DELETE RESTRICT;

CREATE INDEX work_items_by_plan_run ON work_items(plan_run_id, execution_status);
