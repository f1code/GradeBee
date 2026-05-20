-- 006_students_name_nocase.sql
-- Replace the UNIQUE(class_id, name) table constraint with an explicit unique
-- index that uses COLLATE NOCASE, so duplicate student names differing only in
-- case are rejected at the database level.
--
-- SQLite does not support ALTER TABLE ... DROP CONSTRAINT, so we use the
-- recommended table-rebuild pattern:
--   1. Rename original table
--   2. Re-create with new definition (no inline UNIQUE constraint)
--   3. Copy data
--   4. Drop old table
--   5. Create the case-insensitive unique index
--
-- SQLite automatically updates FK references in child tables when the parent is
-- renamed, so notes, reports, and student_aliases all end up referencing
-- "students_old" after the rename.  Those tables must also be rebuilt to point
-- at the new "students" table before we drop "students_old".

PRAGMA foreign_keys = OFF;

-- Rename everything that references students
ALTER TABLE student_aliases RENAME TO student_aliases_old;
ALTER TABLE notes           RENAME TO notes_old;
ALTER TABLE reports         RENAME TO reports_old;
ALTER TABLE students        RENAME TO students_old;

-- Rebuild students (drop the inline UNIQUE constraint)
CREATE TABLE students (
    id          INTEGER PRIMARY KEY,
    class_id    INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO students (id, class_id, name, created_at)
    SELECT id, class_id, name, created_at FROM students_old;

-- Rebuild notes referencing the new students table
CREATE TABLE notes (
    id          INTEGER PRIMARY KEY,
    student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    date        TEXT NOT NULL,
    summary     TEXT NOT NULL,
    transcript  TEXT,
    source      TEXT NOT NULL DEFAULT 'auto',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO notes (id, student_id, date, summary, transcript, source, created_at, updated_at)
    SELECT id, student_id, date, summary, transcript, source, created_at, updated_at FROM notes_old;

-- Rebuild reports referencing the new students table
CREATE TABLE reports (
    id           INTEGER PRIMARY KEY,
    student_id   INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    start_date   TEXT NOT NULL,
    end_date     TEXT NOT NULL,
    html         TEXT NOT NULL,
    instructions TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO reports (id, student_id, start_date, end_date, html, instructions, created_at)
    SELECT id, student_id, start_date, end_date, html, instructions, created_at FROM reports_old;

-- Rebuild student_aliases referencing the new students table
CREATE TABLE student_aliases (
    id         INTEGER PRIMARY KEY,
    student_id INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    class_id   INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    alias      TEXT    NOT NULL COLLATE NOCASE,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(class_id, alias)
);

INSERT INTO student_aliases (id, student_id, class_id, alias, created_at)
    SELECT id, student_id, class_id, alias, created_at FROM student_aliases_old;

DROP TABLE student_aliases_old;
DROP TABLE reports_old;
DROP TABLE notes_old;
DROP TABLE students_old;

CREATE UNIQUE INDEX idx_students_class_name_nocase
    ON students(class_id, name COLLATE NOCASE);

CREATE INDEX IF NOT EXISTS idx_notes_student    ON notes(student_id);
CREATE INDEX IF NOT EXISTS idx_notes_date       ON notes(student_id, date);
CREATE INDEX IF NOT EXISTS idx_reports_student  ON reports(student_id);
CREATE INDEX IF NOT EXISTS idx_student_aliases_class_alias ON student_aliases(class_id, alias);
CREATE INDEX IF NOT EXISTS idx_student_aliases_student     ON student_aliases(student_id);

PRAGMA foreign_keys = ON;
