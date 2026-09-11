-- Questions gain an unsharp state before open, store their attention state
-- rather than a boolean derived from it, and can block work items through
-- explicit links.
--
-- The table is rebuilt because the status CHECK must admit 'unsharp' and
-- the attention column changes type, neither of which ALTER TABLE can do.
-- No table references questions, so renaming the original away first is only
-- for symmetry with the other rebuilds.
ALTER TABLE questions RENAME TO questions_old;

CREATE TABLE questions (
  id TEXT PRIMARY KEY,
  objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE CASCADE,
  work_item_id TEXT REFERENCES work_items(id) ON DELETE CASCADE,
  question TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('unsharp', 'open', 'answered', 'waived')),
  answer TEXT NOT NULL DEFAULT '',
  attention_state TEXT NOT NULL DEFAULT 'none' CHECK (attention_state IN ('none', 'needs_human_decision', 'needs_human_review', 'needs_clarification', 'intervention_required')),
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  created_by TEXT NOT NULL,
  resolved_by TEXT,
  created_at TEXT NOT NULL,
  resolved_at TEXT
);

-- request_attention on a question never persisted its state (the update
-- statement did not write the column), so the last attention.requested
-- activity is the only record of the state that was asked for. Without one,
-- the creation flag meant a human had to decide.
INSERT INTO questions
  (id, objective_id, work_item_id, question, status, answer, attention_state, version, created_by, resolved_by, created_at, resolved_at)
SELECT old.id, old.objective_id, old.work_item_id, old.question, old.status, old.answer,
       COALESCE(
         (SELECT json_extract(activity.payload_json, '$.attention_state')
          FROM activity
          WHERE activity.entity_kind = 'question' AND activity.entity_id = old.id
            AND activity.event_type = 'attention.requested'
          ORDER BY activity.sequence DESC LIMIT 1),
         CASE WHEN old.requires_human_attention = 1 THEN 'needs_human_decision' ELSE 'none' END
       ),
       old.version, old.created_by, old.resolved_by, old.created_at, old.resolved_at
FROM questions_old old;

DROP TABLE questions_old;
CREATE INDEX questions_by_objective_status ON questions(objective_id, status);

CREATE TABLE question_blocks (
  question_id TEXT NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
  work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
  PRIMARY KEY (question_id, work_item_id)
);

CREATE INDEX question_blocks_by_item ON question_blocks(work_item_id, question_id);

-- A question asked on a work item has always blocked that item while open.
-- Recording that as a link keeps the behaviour and leaves one blocking path.
INSERT INTO question_blocks (question_id, work_item_id, created_by, created_at)
SELECT id, work_item_id, created_by, created_at FROM questions WHERE work_item_id IS NOT NULL;
