CREATE TABLE actors (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('human', 'agent', 'service')),
  display_name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

ALTER TABLE capabilities ADD COLUMN description TEXT NOT NULL DEFAULT '';

CREATE TABLE actor_capabilities (
  actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
  capability_slug TEXT NOT NULL REFERENCES capabilities(slug) ON DELETE CASCADE,
  PRIMARY KEY (actor_id, capability_slug)
);

CREATE TABLE claims (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  acquired_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  released_at TEXT,
  release_reason TEXT NOT NULL DEFAULT '',
  CHECK (expires_at > acquired_at)
);

CREATE UNIQUE INDEX one_unreleased_claim_per_item ON claims(work_item_id) WHERE released_at IS NULL;
CREATE INDEX claims_by_expiry ON claims(work_item_id, expires_at) WHERE released_at IS NULL;
CREATE INDEX actor_capabilities_by_capability ON actor_capabilities(capability_slug, actor_id);

CREATE TABLE progress_entries (
  id TEXT PRIMARY KEY,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  summary TEXT NOT NULL,
  completed_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(completed_json)),
  remaining_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(remaining_json)),
  discovered_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(discovered_json)),
  blocker_json TEXT NOT NULL DEFAULT 'null' CHECK (json_valid(blocker_json)),
  created_at TEXT NOT NULL
);

CREATE INDEX progress_entries_by_item ON progress_entries(work_item_id, created_at);

CREATE TABLE idempotency_records (
  actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
  key TEXT NOT NULL,
  operation TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_json TEXT NOT NULL CHECK (json_valid(response_json)),
  created_at TEXT NOT NULL,
  PRIMARY KEY (actor_id, key)
);
