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

PRAGMA foreign_keys = OFF;

ALTER TABLE students RENAME TO students_old;

CREATE TABLE students (
    id          INTEGER PRIMARY KEY,
    class_id    INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO students (id, class_id, name, created_at)
    SELECT id, class_id, name, created_at FROM students_old;

DROP TABLE students_old;

CREATE UNIQUE INDEX idx_students_class_name_nocase
    ON students(class_id, name COLLATE NOCASE);

PRAGMA foreign_keys = ON;
