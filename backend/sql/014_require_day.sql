-- 014_require_day.sql
-- Add classes.day: a mandatory weekday, parsed out of the existing free-text
-- time_slot. Day is the day of the first class of the week when a Level
-- meets several times; Time slot remains optional free text alongside it.
--
-- Parsing: scan time_slot (case-insensitively) for the first three-letter
-- day abbreviation (mon/tue/wed/thu/fri/sat/sun) that appears in it, by
-- earliest position — this matches both abbreviated ("Wed@14:10") and full
-- ("Group 1 Wednesday") forms, and for "Tuesday & Friday" picks Tuesday
-- (the earlier match) per spec. The matched day name (full or abbreviated)
-- is then stripped from time_slot, along with leftover separators
-- (" ", "@", "-", "&"), leaving the rest as the new time_slot
-- ("Wed@14:10" -> day Wednesday, time_slot "14:10"; "Group 1 Wednesday" ->
-- day Wednesday, time_slot "Group 1").
--
-- Class id=30 (Linda) has no day recorded at all (empty time_slot) and is
-- assigned Saturday per spec, as a one-shot literal exception — not a
-- general "empty text defaults to Saturday" rule.
--
-- Any other class whose day still can't be determined after this aborts the
-- migration by name: that means its text held a day-like word the parser
-- did not recognise, a parser bug worth stopping for, not missing data.
--
-- Uses the table-rebuild pattern (SQLite can't add a CHECK constraint or
-- change a UNIQUE index via ALTER TABLE) — see 006_students_name_nocase.sql
-- and 011_class_level_id.sql for the same pattern. Renaming classes retargets
-- every child table's FK text at "classes_old" (students, student_aliases,
-- and transitively notes, reports via students) — see 011 for the same
-- cascade — so all five tables are rebuilt here, exactly as 011 rebuilt them
-- when classes was the table being replaced. Their own definitions are
-- unchanged; only the referenced table name needs to end up back at
-- "classes".
--
-- Students whose class no longer exists are dropped with their notes,
-- reports, and artifact_feedback — completing the cascade class delete
-- already promises (same pattern as 011's dangling notes).

PRAGMA foreign_keys = OFF;

-- 1. Add day, nullable for now — populated below, then hardened when the
--    table is rebuilt.
ALTER TABLE classes ADD COLUMN day TEXT;

-- 2. Backfill day: earliest-position match among the seven three-letter
--    abbreviations, case-insensitive.
UPDATE classes
SET day = (
    SELECT full_name FROM (
        SELECT 'Monday'    AS full_name, INSTR(LOWER(classes.time_slot), 'mon') AS pos UNION ALL
        SELECT 'Tuesday',   INSTR(LOWER(classes.time_slot), 'tue') UNION ALL
        SELECT 'Wednesday', INSTR(LOWER(classes.time_slot), 'wed') UNION ALL
        SELECT 'Thursday',  INSTR(LOWER(classes.time_slot), 'thu') UNION ALL
        SELECT 'Friday',    INSTR(LOWER(classes.time_slot), 'fri') UNION ALL
        SELECT 'Saturday',  INSTR(LOWER(classes.time_slot), 'sat') UNION ALL
        SELECT 'Sunday',    INSTR(LOWER(classes.time_slot), 'sun')
    )
    WHERE pos > 0
    ORDER BY pos ASC
    LIMIT 1
);

-- 3. One-shot literal exception: Linda (id=30) has no day recorded and is
--    assigned Saturday, per spec.
UPDATE classes SET day = 'Saturday' WHERE id = 30 AND day IS NULL;

-- 4. Guard: abort naming the first class whose day still couldn't be
--    determined — a parser bug (unrecognised day text), not missing data.
CREATE TEMP TABLE _require_day_guard(x);
CREATE TEMP TRIGGER _require_day_guard_check BEFORE INSERT ON _require_day_guard
BEGIN
    SELECT RAISE(ABORT, 'migration 014: class id=' ||
        (SELECT id FROM classes WHERE day IS NULL LIMIT 1) ||
        ' has unrecognised or missing day text: ' ||
        (SELECT time_slot FROM classes WHERE day IS NULL LIMIT 1))
    WHERE (SELECT COUNT(*) FROM classes WHERE day IS NULL) > 0;
END;
INSERT INTO _require_day_guard VALUES (1);
DROP TRIGGER _require_day_guard_check;
DROP TABLE _require_day_guard;

-- 5. Rebuild classes: harden day to NOT NULL with a CHECK over the seven
--    weekday names, and widen uniqueness to (user_id, level_id, day, time_slot)
--    so two classes at the same Level and day are distinguished by time slot.
-- Stripping the day token out of time_slot happens here, in the INSERT,
-- rather than via an in-place UPDATE on the old table: an in-place strip
-- would collide with the old UNIQUE(user_id, level_id, time_slot) constraint
-- whenever two classes at the same Level strip down to the same leftover
-- text (e.g. two bare-day classes both stripping to ''), even though their
-- new (day, time_slot) pairs are distinct.
--
-- Renaming classes retargets students/student_aliases's FK text at
-- "classes_old"; rebuilding students (unchanged schema, just re-pointed at
-- the new "classes") in turn retargets notes/reports at "students_old". All
-- five tables are rebuilt below, exactly as 011_class_level_id.sql did.
ALTER TABLE classes         RENAME TO classes_old;
ALTER TABLE students        RENAME TO students_old;
ALTER TABLE student_aliases RENAME TO student_aliases_old;
ALTER TABLE notes           RENAME TO notes_old;
ALTER TABLE reports         RENAME TO reports_old;

CREATE TABLE classes (
    id            INTEGER PRIMARY KEY,
    user_id       TEXT NOT NULL,
    level_id      INTEGER NOT NULL REFERENCES levels(id) ON DELETE RESTRICT,
    day           TEXT NOT NULL CHECK (day IN ('Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday')),
    time_slot     TEXT NOT NULL DEFAULT '',
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (user_id, level_id, day, time_slot)
);

INSERT INTO classes (id, user_id, level_id, day, time_slot, position, created_at)
    SELECT id, user_id, level_id, day,
        TRIM(TRIM(TRIM(
            REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
            REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
                time_slot,
                'Monday', ''), 'Tuesday', ''), 'Wednesday', ''), 'Thursday', ''), 'Friday', ''), 'Saturday', ''), 'Sunday', ''),
                'Mon', ''), 'Tue', ''), 'Wed', ''), 'Thu', ''), 'Fri', ''), 'Sat', ''), 'Sun', '')
            , ' @-'), '&'), ' @-'),
        position, created_at
    FROM classes_old;

-- 5b. Guard: abort naming the first class whose stripped time_slot still
--     contains a day token case-insensitively (e.g. original text was
--     lowercase/uppercase, so the case-sensitive REPLACE chain above missed
--     it) — would otherwise leave a doubled display name like
--     "Marcia · Wed · wed@14:10".
CREATE TEMP TABLE _require_day_leftover_guard(x);
CREATE TEMP TRIGGER _require_day_leftover_guard_check BEFORE INSERT ON _require_day_leftover_guard
BEGIN
    SELECT RAISE(ABORT, 'migration 014: class id=' ||
        (SELECT id FROM classes WHERE
            INSTR(LOWER(time_slot), 'mon') > 0 OR INSTR(LOWER(time_slot), 'tue') > 0 OR
            INSTR(LOWER(time_slot), 'wed') > 0 OR INSTR(LOWER(time_slot), 'thu') > 0 OR
            INSTR(LOWER(time_slot), 'fri') > 0 OR INSTR(LOWER(time_slot), 'sat') > 0 OR
            INSTR(LOWER(time_slot), 'sun') > 0
            LIMIT 1) ||
        ' still has a day token left in time_slot after stripping: ' ||
        (SELECT time_slot FROM classes WHERE
            INSTR(LOWER(time_slot), 'mon') > 0 OR INSTR(LOWER(time_slot), 'tue') > 0 OR
            INSTR(LOWER(time_slot), 'wed') > 0 OR INSTR(LOWER(time_slot), 'thu') > 0 OR
            INSTR(LOWER(time_slot), 'fri') > 0 OR INSTR(LOWER(time_slot), 'sat') > 0 OR
            INSTR(LOWER(time_slot), 'sun') > 0
            LIMIT 1))
    WHERE (SELECT COUNT(*) FROM classes WHERE
            INSTR(LOWER(time_slot), 'mon') > 0 OR INSTR(LOWER(time_slot), 'tue') > 0 OR
            INSTR(LOWER(time_slot), 'wed') > 0 OR INSTR(LOWER(time_slot), 'thu') > 0 OR
            INSTR(LOWER(time_slot), 'fri') > 0 OR INSTR(LOWER(time_slot), 'sat') > 0 OR
            INSTR(LOWER(time_slot), 'sun') > 0
          ) > 0;
END;
INSERT INTO _require_day_leftover_guard VALUES (1);
DROP TRIGGER _require_day_leftover_guard_check;
DROP TABLE _require_day_leftover_guard;

CREATE TABLE students (
    id          INTEGER PRIMARY KEY,
    class_id    INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO students (id, class_id, name, created_at)
    SELECT id, class_id, name, created_at FROM students_old
    WHERE EXISTS (SELECT 1 FROM classes WHERE classes.id = students_old.class_id);

CREATE TABLE student_aliases (
    id         INTEGER PRIMARY KEY,
    student_id INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    class_id   INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    alias      TEXT    NOT NULL COLLATE NOCASE,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(class_id, alias)
);

INSERT INTO student_aliases (id, student_id, class_id, alias, created_at)
    SELECT id, student_id, class_id, alias, created_at FROM student_aliases_old
    WHERE EXISTS (SELECT 1 FROM students WHERE students.id = student_aliases_old.student_id)
      AND EXISTS (SELECT 1 FROM classes WHERE classes.id = student_aliases_old.class_id);

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

-- artifact_feedback has no FK.
DELETE FROM artifact_feedback
 WHERE (artifact_type = 'note'   AND NOT EXISTS (SELECT 1 FROM notes   WHERE notes.id   = artifact_feedback.artifact_id))
    OR (artifact_type = 'report' AND NOT EXISTS (SELECT 1 FROM reports WHERE reports.id = artifact_feedback.artifact_id));

DROP TABLE reports_old;
DROP TABLE notes_old;
DROP TABLE student_aliases_old;
DROP TABLE students_old;
DROP TABLE classes_old;

CREATE INDEX idx_classes_user  ON classes(user_id);
CREATE INDEX idx_classes_level ON classes(level_id);
CREATE UNIQUE INDEX idx_students_class_name_nocase ON students(class_id, name COLLATE NOCASE);
CREATE INDEX idx_student_aliases_class_alias ON student_aliases(class_id, alias);
CREATE INDEX idx_student_aliases_student     ON student_aliases(student_id);
CREATE INDEX idx_notes_student    ON notes(student_id);
CREATE INDEX idx_notes_date       ON notes(student_id, date);
CREATE INDEX idx_reports_student  ON reports(student_id);

PRAGMA foreign_keys = ON;
