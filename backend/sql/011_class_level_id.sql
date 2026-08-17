-- 011_class_level_id.sql
-- Wire classes to the levels table by contains-match, move leftover text into
-- empty schedules, then rebuild classes with level_id NOT NULL and drop the
-- free-text level_name / derived name columns.
--
-- Uses the table-rebuild pattern (SQLite has no ALTER TABLE ... DROP
-- CONSTRAINT / cannot drop a column referenced by a UNIQUE index without a
-- rebuild): see 006_students_name_nocase.sql for the same pattern. RunMigrations
-- wraps every migration in one transaction, so "PRAGMA foreign_keys = OFF"
-- below is a documented no-op (matches 006's note) — it does not disable FK
-- enforcement, but it also isn't needed to: renaming a table only rewrites
-- child FK text (SQLite auto-retargets it at the new name), it never cascades
-- deletes. Renaming classes retargets students/student_aliases at
-- "classes_old"; renaming students (needed because its schema is unchanged
-- but it must be rebuilt to point students/student_aliases' children back at
-- the new "classes") in turn retargets notes/reports at "students_old". All
-- five tables are therefore rebuilt here, exactly as 006 rebuilt notes/reports
-- when students was the table being replaced.
--
-- Two `notes` rows in production (ids 293, 407) already carry a dangling
-- student_id from before FK enforcement existed on this column — orphaned,
-- unreachable via the app (every read joins through students), and pre-dating
-- this migration. The rebuilt notes table enforces the FK for the first time,
-- so those two rows are dropped here rather than left to abort the migration
-- over data this ticket didn't create and doesn't touch.

PRAGMA foreign_keys = OFF;

-- 1. Add level_id, nullable for now — populated below, then hardened to
--    NOT NULL when the table is rebuilt.
ALTER TABLE classes ADD COLUMN level_id INTEGER REFERENCES levels(id);

-- 2. Backfill by contains-match: the Level whose name appears as a substring
--    of the class's old level_name, case-insensitively. Longest-name-first
--    breaks ties if a shorter Level name is itself a substring of a longer
--    one (not the case in production today, but the query stays correct if
--    it ever is).
UPDATE classes
SET level_id = (
    SELECT l.id FROM levels l
    WHERE INSTR(LOWER(classes.level_name), LOWER(l.name)) > 0
    ORDER BY LENGTH(l.name) DESC
    LIMIT 1
);

-- 3. Move leftover text into schedule_name when it's empty. Only meaningful
--    once level_id is set — the matched Level's name is subtracted from the
--    old level_name and whatever remains (trimmed) becomes the schedule.
UPDATE classes
SET schedule_name = TRIM(
    SUBSTR(level_name, 1, INSTR(LOWER(level_name), LOWER((SELECT name FROM levels WHERE id = classes.level_id))) - 1)
    || SUBSTR(level_name, INSTR(LOWER(level_name), LOWER((SELECT name FROM levels WHERE id = classes.level_id))) + LENGTH((SELECT name FROM levels WHERE id = classes.level_id)))
)
WHERE schedule_name = '' AND level_id IS NOT NULL;

-- 4. Guard: abort naming the first class that failed to map. The rebuild
--    below makes level_id NOT NULL anyway (which would abort the whole
--    migration), but a bare constraint violation doesn't say which row.
CREATE TEMP TABLE _class_level_id_guard(x);
CREATE TEMP TRIGGER _class_level_id_guard_check BEFORE INSERT ON _class_level_id_guard
BEGIN
    SELECT RAISE(ABORT, 'migration 011: class id=' ||
        (SELECT id FROM classes WHERE level_id IS NULL LIMIT 1) ||
        ' has no matching Level')
    WHERE (SELECT COUNT(*) FROM classes WHERE level_id IS NULL) > 0;
END;
INSERT INTO _class_level_id_guard VALUES (1);
DROP TRIGGER _class_level_id_guard_check;
DROP TABLE _class_level_id_guard;

-- 5. Rebuild classes (drop `name` and `level_name`, harden level_id to
--    NOT NULL with ON DELETE RESTRICT, move uniqueness to
--    (user_id, level_id, schedule_name)) and every table that transitively
--    references it by FK: renaming a referenced table retargets child FK
--    text at the renamed name, cascading the rebuild down to notes/reports.
ALTER TABLE classes         RENAME TO classes_old;
ALTER TABLE students        RENAME TO students_old;
ALTER TABLE student_aliases RENAME TO student_aliases_old;
ALTER TABLE notes           RENAME TO notes_old;
ALTER TABLE reports         RENAME TO reports_old;

CREATE TABLE classes (
    id            INTEGER PRIMARY KEY,
    user_id       TEXT NOT NULL,
    level_id      INTEGER NOT NULL REFERENCES levels(id) ON DELETE RESTRICT,
    schedule_name TEXT NOT NULL DEFAULT '',
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (user_id, level_id, schedule_name)
);

INSERT INTO classes (id, user_id, level_id, schedule_name, position, created_at)
    SELECT id, user_id, level_id, schedule_name, position, created_at FROM classes_old;

CREATE TABLE students (
    id          INTEGER PRIMARY KEY,
    class_id    INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO students (id, class_id, name, created_at)
    SELECT id, class_id, name, created_at FROM students_old;

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

CREATE TABLE notes (
    id          INTEGER PRIMARY KEY,
    student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    date        TEXT NOT NULL,
    summary     TEXT NOT NULL,
    transcript  TEXT,
    source      TEXT NOT NULL DEFAULT 'auto',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    model_version TEXT,
    prompt_hash    TEXT
);

INSERT INTO notes (id, student_id, date, summary, transcript, source, created_at, updated_at, model_version, prompt_hash)
    SELECT id, student_id, date, summary, transcript, source, created_at, updated_at, model_version, prompt_hash
    FROM notes_old
    WHERE EXISTS (SELECT 1 FROM students WHERE students.id = notes_old.student_id);

CREATE TABLE reports (
    id           INTEGER PRIMARY KEY,
    student_id   INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    start_date   TEXT NOT NULL,
    end_date     TEXT NOT NULL,
    html         TEXT NOT NULL,
    instructions TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    model_version TEXT,
    prompt_hash    TEXT
);

INSERT INTO reports (id, student_id, start_date, end_date, html, instructions, created_at, model_version, prompt_hash)
    SELECT id, student_id, start_date, end_date, html, instructions, created_at, model_version, prompt_hash
    FROM reports_old
    WHERE EXISTS (SELECT 1 FROM students WHERE students.id = reports_old.student_id);

DROP TABLE reports_old;
DROP TABLE notes_old;
DROP TABLE student_aliases_old;
DROP TABLE students_old;
DROP TABLE classes_old;

CREATE INDEX idx_classes_user ON classes(user_id);
CREATE INDEX idx_classes_level ON classes(level_id);
CREATE UNIQUE INDEX idx_students_class_name_nocase ON students(class_id, name COLLATE NOCASE);
CREATE INDEX idx_student_aliases_class_alias ON student_aliases(class_id, alias);
CREATE INDEX idx_student_aliases_student     ON student_aliases(student_id);
CREATE INDEX idx_notes_student    ON notes(student_id);
CREATE INDEX idx_notes_date       ON notes(student_id, date);
CREATE INDEX idx_reports_student  ON reports(student_id);

PRAGMA foreign_keys = ON;
