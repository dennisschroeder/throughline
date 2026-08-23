CREATE TABLE idempotency_records_new (
  actor_id TEXT NOT NULL,
  key TEXT NOT NULL,
  operation TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_json TEXT NOT NULL CHECK (json_valid(response_json)),
  created_at TEXT NOT NULL,
  PRIMARY KEY (actor_id, key)
);

INSERT INTO idempotency_records_new (actor_id, key, operation, request_hash, response_json, created_at)
SELECT actor_id, key, operation, request_hash, response_json, created_at
FROM idempotency_records;

DROP TABLE idempotency_records;
ALTER TABLE idempotency_records_new RENAME TO idempotency_records;
