-- 006_artifact_versioning.sql: Add model_version and prompt_hash columns to
-- reports and notes for correlating quality regressions to prompt/model changes.
-- Existing rows get NULL (meaning "pre-instrumentation").

ALTER TABLE reports ADD COLUMN model_version TEXT;
ALTER TABLE reports ADD COLUMN prompt_hash    TEXT;

ALTER TABLE notes   ADD COLUMN model_version TEXT;
ALTER TABLE notes   ADD COLUMN prompt_hash    TEXT;

-- Index to correlate production quality drops to specific prompt/model versions.
CREATE INDEX IF NOT EXISTS idx_reports_versioning ON reports(prompt_hash, model_version);
CREATE INDEX IF NOT EXISTS idx_notes_versioning   ON notes(prompt_hash, model_version);
