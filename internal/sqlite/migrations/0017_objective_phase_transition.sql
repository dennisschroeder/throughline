-- An objective transition has always required a reason and then discarded
-- it. The latest transition's edge, reason, actor and time are now kept on
-- the objective, where the phase they explain is read; every transition also
-- carries its reason in the activity feed.
--
-- Objectives transitioned before this migration are left without one. Their
-- reasons were never stored, and an edge recovered without its reason would
-- read as a complete record.
ALTER TABLE objectives ADD COLUMN phase_transition_from TEXT;
ALTER TABLE objectives ADD COLUMN phase_transition_to TEXT;
ALTER TABLE objectives ADD COLUMN phase_transition_reason TEXT;
ALTER TABLE objectives ADD COLUMN phase_transition_by TEXT;
ALTER TABLE objectives ADD COLUMN phase_transition_at TEXT;
