CREATE TABLE objectives (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  desired_outcome TEXT NOT NULL DEFAULT '',
  phase TEXT NOT NULL CHECK (phase IN ('idea', 'discovery', 'planning', 'execution', 'evaluation', 'completed', 'paused', 'cancelled')),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE plans (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL CHECK (revision > 0),
  commitment_state TEXT NOT NULL CHECK (commitment_state IN ('draft', 'proposed', 'approved', 'rejected', 'superseded')),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(objective_id, revision)
);

CREATE TABLE work_items (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  plan_id TEXT REFERENCES plans(id) ON DELETE SET NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  commitment_state TEXT NOT NULL CHECK (commitment_state IN ('proposed', 'accepted', 'rejected', 'superseded')),
  execution_status TEXT NOT NULL CHECK (execution_status IN ('backlog', 'ready', 'in_progress', 'review', 'done', 'cancelled')),
  priority TEXT NOT NULL CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
  estimated_scope TEXT NOT NULL CHECK (estimated_scope IN ('xs', 'small', 'medium', 'large', 'unknown')),
  execution_policy TEXT NOT NULL CHECK (execution_policy IN ('human_only', 'agent_may_propose', 'approval_required', 'autonomous_with_report')),
  required_actor_kind TEXT NOT NULL CHECK (required_actor_kind IN ('any', 'human', 'agent')),
  attention_state TEXT NOT NULL CHECK (attention_state IN ('none', 'needs_human_decision', 'needs_human_review', 'needs_clarification', 'intervention_required')),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE output_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  version INTEGER NOT NULL CHECK (version > 0),
  description TEXT NOT NULL DEFAULT '',
  lifecycle_state TEXT NOT NULL CHECK (lifecycle_state IN ('draft', 'proposed', 'active', 'rejected', 'superseded')),
  structure_json TEXT NOT NULL DEFAULT '{}',
  semantics_json TEXT NOT NULL DEFAULT '{}',
  validation_json TEXT NOT NULL DEFAULT '{}',
  built_in INTEGER NOT NULL DEFAULT 0 CHECK (built_in IN (0, 1)),
  created_at TEXT NOT NULL,
  UNIQUE(name, version),
  CHECK (json_valid(structure_json)),
  CHECK (json_valid(semantics_json)),
  CHECK (json_valid(validation_json))
);

CREATE TABLE expected_outputs (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  output_profile_id TEXT NOT NULL REFERENCES output_profiles(id) ON DELETE RESTRICT,
  contract_json TEXT NOT NULL DEFAULT '{}',
  destination_hint TEXT NOT NULL DEFAULT '',
  required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
  ordinal INTEGER NOT NULL CHECK (ordinal > 0),
  UNIQUE(work_item_id, ordinal),
  CHECK (json_valid(contract_json))
);

CREATE INDEX plans_by_objective_state ON plans(objective_id, commitment_state, revision);
CREATE INDEX work_items_by_objective ON work_items(objective_id, execution_status);
CREATE INDEX output_profiles_by_name_state ON output_profiles(name, lifecycle_state, version);
CREATE INDEX expected_outputs_by_item ON expected_outputs(work_item_id, ordinal);

INSERT INTO output_profiles
  (id, name, version, description, lifecycle_state, structure_json, semantics_json, validation_json, built_in, created_at)
VALUES
  ('0198ce12-1800-7000-8000-000000000001', 'structured_document', 1, 'A structured human-readable document.', 'active',
   '{"required":["title","summary","sections"]}',
   '{"purpose":"communicate a structured result"}',
   '{"required":[{"kind":"structure"}]}', 1, '2026-08-21T00:00:00.000000000Z'),
  ('0198ce12-1800-7000-8000-000000000002', 'research_dossier', 1, 'A source-linked research result with explicit uncertainty.', 'active',
   '{"required":["question","method","findings","sources","uncertainty","conclusion"]}',
   '{"claims_require_provenance":true}',
   '{"required":[{"kind":"structure"},{"kind":"provenance"},{"kind":"human_review"}]}', 1, '2026-08-21T00:00:00.000000000Z'),
  ('0198ce12-1800-7000-8000-000000000003', 'decision_record', 1, 'A durable decision with rationale and consequences.', 'active',
   '{"required":["context","decision","rationale","alternatives","consequences"]}',
   '{"decision_is_explicit":true}',
   '{"required":[{"kind":"structure"},{"kind":"human_review"}]}', 1, '2026-08-21T00:00:00.000000000Z'),
  ('0198ce12-1800-7000-8000-000000000004', 'skill_package', 1, 'A reusable agent skill definition and evaluation contract.', 'active',
   '{"required":["instructions","inputs","outputs","evaluation"]}',
   '{"describes_behavior_without_executing_it":true}',
   '{"required":[{"kind":"structure"},{"kind":"evaluation"},{"kind":"human_review"}]}', 1, '2026-08-21T00:00:00.000000000Z'),
  ('0198ce12-1800-7000-8000-000000000005', 'agent_definition', 1, 'A vendor-neutral agent role definition.', 'active',
   '{"required":["role","capabilities","instructions","constraints"]}',
   '{"capabilities_do_not_grant_authority":true}',
   '{"required":[{"kind":"structure"},{"kind":"human_review"}]}', 1, '2026-08-21T00:00:00.000000000Z'),
  ('0198ce12-1800-7000-8000-000000000006', 'tool_installation', 1, 'A verified description of a tool installation.', 'active',
   '{"required":["target","version","scope","verification","rollback"]}',
   '{"records_external_results_without_executing_actions":true}',
   '{"required":[{"kind":"structure"},{"kind":"probe"}]}', 1, '2026-08-21T00:00:00.000000000Z'),
  ('0198ce12-1800-7000-8000-000000000007', 'workflow_definition', 1, 'A reusable domain-neutral workflow definition.', 'active',
   '{"required":["purpose","steps","roles","inputs","outputs"]}',
   '{"describes_coordination_not_orchestration":true}',
   '{"required":[{"kind":"structure"},{"kind":"human_review"}]}', 1, '2026-08-21T00:00:00.000000000Z'),
  ('0198ce12-1800-7000-8000-000000000008', 'generic_artifact', 1, 'A generic artifact reference and description.', 'active',
   '{"required":["description","artifact_references"]}',
   '{"artifacts_are_external_references":true}',
   '{"required":[{"kind":"structure"}]}', 1, '2026-08-21T00:00:00.000000000Z');
