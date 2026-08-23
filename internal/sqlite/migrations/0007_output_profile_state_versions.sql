ALTER TABLE output_profiles ADD COLUMN state_version INTEGER NOT NULL DEFAULT 1 CHECK (state_version > 0);
