-- 021_terms.sql
-- Report Instructions vary by Term (#168): a Group owns Terms, each Level
-- holds one text per Term, and a Report belongs to the Term it was generated
-- in, so a regenerate reads that Term's text, not a later one.
--
-- Every Group gets "Term 1" (first note this school year: 2026-08-19) holding
-- its Levels' text and every existing Report. Groups exist only as
-- levels.group_id; a Group without Levels has no Reports either.

CREATE TABLE terms (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  group_id    TEXT NOT NULL,
  name        TEXT NOT NULL,
  start_date  TEXT NOT NULL,
  end_date    TEXT NOT NULL,
  archived_at TEXT,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  CHECK (start_date <= end_date)
);
CREATE INDEX idx_terms_group ON terms(group_id, start_date);

-- Deleting a Level drops its text; a Term with text cannot be deleted.
CREATE TABLE report_instructions (
  level_id INTEGER NOT NULL REFERENCES levels(id) ON DELETE CASCADE,
  term_id  INTEGER NOT NULL REFERENCES terms(id) ON DELETE RESTRICT,
  text     TEXT NOT NULL,
  PRIMARY KEY (level_id, term_id)
);
CREATE INDEX idx_report_instructions_term ON report_instructions(term_id);

INSERT INTO terms (group_id, name, start_date, end_date)
  SELECT DISTINCT group_id, 'Term 1', '2026-08-15', '2026-11-01' FROM levels;

INSERT INTO report_instructions (level_id, term_id, text)
  SELECT l.id, t.id, l.report_instructions
  FROM levels l JOIN terms t ON t.group_id = l.group_id
  WHERE TRIM(l.report_instructions) <> '';

ALTER TABLE levels DROP COLUMN report_instructions;

-- term_id NOT NULL with an FK needs a rebuild (019's pattern). Keep
-- AUTOINCREMENT's high-water mark: feedback result_id outlives deleted reports.
CREATE TABLE reports_new (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    student_id   INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    term_id      INTEGER NOT NULL REFERENCES terms(id) ON DELETE RESTRICT,
    start_date   TEXT NOT NULL,
    end_date     TEXT NOT NULL,
    html         TEXT NOT NULL,
    instructions TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    model_version TEXT,
    prompt_hash    TEXT,
    level_instructions TEXT
);

INSERT INTO reports_new (id, student_id, term_id, start_date, end_date, html, instructions, created_at, model_version, prompt_hash, level_instructions)
    SELECT r.id, r.student_id, t.id, r.start_date, r.end_date, r.html, r.instructions, r.created_at, r.model_version, r.prompt_hash, r.level_instructions
    FROM reports r
    -- LEFT JOINs: a report with no Group to reach fails NOT NULL and aborts
    -- the migration instead of being dropped.
    LEFT JOIN students s ON s.id = r.student_id
    LEFT JOIN classes c ON c.id = s.class_id
    LEFT JOIN levels l ON l.id = c.level_id
    LEFT JOIN terms t ON t.group_id = l.group_id;

DELETE FROM sqlite_sequence WHERE name = 'reports_new';
INSERT INTO sqlite_sequence (name, seq)
    SELECT 'reports_new', seq FROM sqlite_sequence WHERE name = 'reports';

DROP TABLE reports;
ALTER TABLE reports_new RENAME TO reports;
CREATE INDEX idx_reports_student ON reports(student_id);
CREATE INDEX idx_reports_term ON reports(term_id);
