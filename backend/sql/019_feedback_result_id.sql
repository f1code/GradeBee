-- 019_feedback_result_id.sql
-- A regenerate sends every earlier round's feedback (#214); without it the model
-- brings back facts an earlier round removed.
--
-- The text lives once, in artifact_feedback. For signal='regenerated',
-- artifact_id is the source report and result_id the report it produced: a
-- Teacher can regenerate one source twice from History, so the source alone
-- cannot say which child a row belongs to.
--
-- No FK: rows whose reports were deleted are expected and carry the chain
-- across a deleted draft. Never orphan-clean 'regenerated' rows (014 once did).
ALTER TABLE artifact_feedback ADD COLUMN result_id INTEGER;
CREATE UNIQUE INDEX idx_artifact_feedback_result
  ON artifact_feedback(result_id) WHERE result_id IS NOT NULL;

-- result_id outlives deleted reports, so a reused reports.id would collide
-- with, or inherit, another report's chain. Plain INTEGER PRIMARY KEY reuses
-- the top id after a delete (see 017); rebuild with AUTOINCREMENT. Nothing
-- references reports, so the swap needs no FK dance.
CREATE TABLE reports_new (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    student_id   INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    start_date   TEXT NOT NULL,
    end_date     TEXT NOT NULL,
    html         TEXT NOT NULL,
    instructions TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    model_version TEXT,
    prompt_hash    TEXT
);

INSERT INTO reports_new (id, student_id, start_date, end_date, html, instructions, created_at, model_version, prompt_hash)
    SELECT id, student_id, start_date, end_date, html, instructions, created_at, model_version, prompt_hash
    FROM reports;

DROP TABLE reports;
ALTER TABLE reports_new RENAME TO reports;
CREATE INDEX idx_reports_student ON reports(student_id);

-- Start above every id a feedback row may still name, deleted reports included.
-- result_id is not read: the column is new in this file, so it is all NULL here.
DELETE FROM sqlite_sequence WHERE name = 'reports';
INSERT INTO sqlite_sequence (name, seq)
    SELECT 'reports', MAX(
        COALESCE((SELECT MAX(id) FROM reports), 0),
        COALESCE((SELECT MAX(artifact_id) FROM artifact_feedback WHERE artifact_type = 'report'), 0));
