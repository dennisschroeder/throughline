ALTER TABLE objectives ADD COLUMN prior_phase TEXT;
ALTER TABLE objectives ADD COLUMN updated_by TEXT;

ALTER TABLE plans ADD COLUMN proposed_by TEXT;
ALTER TABLE plans ADD COLUMN proposed_at TEXT;
ALTER TABLE plans ADD COLUMN resolved_by TEXT;
ALTER TABLE plans ADD COLUMN resolved_at TEXT;
ALTER TABLE plans ADD COLUMN resolution_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE work_items ADD COLUMN parent_id TEXT REFERENCES work_items(id) ON DELETE SET NULL;

ALTER TABLE output_profiles ADD COLUMN supersedes_id TEXT REFERENCES output_profiles(id) ON DELETE RESTRICT;
ALTER TABLE output_profiles ADD COLUMN proposed_by TEXT;
ALTER TABLE output_profiles ADD COLUMN proposed_at TEXT;
ALTER TABLE output_profiles ADD COLUMN resolved_by TEXT;
ALTER TABLE output_profiles ADD COLUMN resolved_at TEXT;
ALTER TABLE output_profiles ADD COLUMN resolution_reason TEXT NOT NULL DEFAULT '';

CREATE TABLE context_records (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('requirement', 'constraint', 'assumption', 'finding', 'risk', 'success_metric')),
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

CREATE TABLE questions (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  question TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('open', 'answered', 'waived')),
  answer TEXT NOT NULL DEFAULT '',
  requires_human_attention INTEGER NOT NULL DEFAULT 0 CHECK (requires_human_attention IN (0, 1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_by TEXT NOT NULL,
  resolved_by TEXT,
  created_at TEXT NOT NULL,
  resolved_at TEXT
);

CREATE TABLE decisions (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  decision TEXT NOT NULL,
  rationale TEXT NOT NULL DEFAULT '',
  alternatives_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL CHECK (status IN ('proposed', 'accepted', 'superseded')),
  supersedes_id TEXT REFERENCES decisions(id) ON DELETE RESTRICT,
  decided_by TEXT NOT NULL,
  decided_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  CHECK (json_valid(alternatives_json))
);

CREATE TABLE approvals (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  plan_id TEXT NOT NULL UNIQUE REFERENCES plans(id) ON DELETE CASCADE,
  request TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('approved', 'rejected')),
  requested_by TEXT NOT NULL,
  requested_at TEXT NOT NULL,
  resolved_by TEXT NOT NULL,
  resolved_at TEXT NOT NULL,
  rationale TEXT NOT NULL
);

CREATE TABLE capabilities (
  slug TEXT PRIMARY KEY
);

CREATE TABLE work_item_capabilities (
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  capability_slug TEXT NOT NULL REFERENCES capabilities(slug) ON DELETE RESTRICT,
  PRIMARY KEY (work_item_id, capability_slug)
);

CREATE INDEX context_by_objective_kind ON context_records(objective_id, kind, status);
CREATE INDEX questions_by_objective_status ON questions(objective_id, status);
CREATE INDEX decisions_by_objective_status ON decisions(objective_id, status);
CREATE INDEX work_items_by_parent ON work_items(parent_id);
CREATE INDEX work_item_capabilities_by_slug ON work_item_capabilities(capability_slug, work_item_id);
