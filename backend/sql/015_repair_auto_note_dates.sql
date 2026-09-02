-- 015_repair_auto_note_dates.sql
-- Repair auto note dates that the extraction LLM hallucinated.
--
-- Until the change that ships with this migration, notes.date on an auto note was
-- whatever the extraction model returned. The prompt asked for a date but never said
-- what today was, so the model invented one — most often a year. The note dated
-- 1615-01-01 is a teacher speaking the time "16:15" at the top of a recording.
--
-- This matters because report_generator filters WHERE date BETWEEN ? AND ?. A note
-- recorded this term but stamped 2023 is silently absent from the report — no error,
-- just a shorter report.
--
-- Repair source: substr(created_at, 1, 10), the day the note row was inserted. No
-- match against voice_notes is needed. Measured on production data before writing
-- this (260 notes, 161 auto):
--
--   * all 69 bad rows are source='auto'; no manual row is malformed or off-year
--     (manual dates come from an <input type="date">);
--   * of the 92 auto rows whose date is well-formed and whose year matches
--     created_at, 92 of 92 have date exactly equal to substr(created_at,1,10).
--
-- So created_at reproduces every known-good auto date, and the three groups
-- (13 malformed + 56 off-year + 92 exact) account for all 161 auto rows.
--
-- Every auto row, not just the visibly broken ones. Two reasons:
--
--   * It is the only way to catch a hallucination that landed inside the right year
--     (date 2026-05-12 on a row inserted 2026-03-22). Nothing can tell one of those
--     from a real date, so a test on the year would leave it wrong and silent. The
--     census above leaves that group empty here, but not necessarily on another
--     database.
--   * It cannot overwrite a good date. Every auto row that exists when this runs was
--     written by the old code, which took its date from the model. Migrations run at
--     startup before the server accepts requests, so no row from the new code can be
--     present yet.
--
-- That second point stops holding the moment this migration finishes: the new code
-- dates a note from the voice note's upload time, so a job retried the next day writes
-- date < created_at on purpose. Two consequences. Do not copy this blanket UPDATE into
-- a later migration. And this file is single-shot — never re-arm it by deleting its
-- _migrations row on a database the new code has already written to, or it will
-- overwrite every deliberately backdated auto date. (The tests re-arm it, which is
-- safe only because each runs against a throwaway in-memory database.)
--
-- updated_at is deliberately left alone. Nothing computes on it — it is selected into
-- the JSON envelope and never read back, and the "edited" feedback signal is an
-- artifact_feedback row written by handleUpdateNote, not anything keyed on this column.
-- The cost of stamping it would be to a person reading the column later, who would see
-- 161 notes apparently corrected by a teacher on the deploy date. A repair the teacher
-- never saw is not an edit.
UPDATE notes
   SET date = substr(created_at, 1, 10)
 WHERE source = 'auto';

-- Guard: abort naming the first note whose date is still not YYYY-MM-DD. Applies to
-- every source, not just auto — after the UPDATE above no auto row can fail, so this
-- can only fire on a manual row, which would mean the API accepted one.
--
-- Off-year is deliberately not guarded: a teacher may legitimately backdate a manual
-- note into a previous year.
--
-- The API validation shipping alongside this migration only forbids such a row from
-- being written from now on; a hand-seeded dev database or an old restored backup can
-- still hold one, and this aborts server startup rather than serving. That is the
-- intended failure — but the way out is to repair the named row by hand
-- (UPDATE notes SET date = substr(created_at,1,10) WHERE id = <id>) and start again,
-- not to weaken the guard.
--
-- Nothing partial commits, but not because of ABORT on its own: RAISE(ABORT) rolls back
-- only the statement that raised it and leaves the transaction open. What discards the
-- UPDATE above is migrate.go — it runs each file inside a tx and calls tx.Rollback() on
-- the error Exec returns. Run this file through the sqlite3 CLI instead and the UPDATE
-- does commit, because the CLI is in autocommit.
CREATE TEMP TABLE _repair_note_dates_guard(x);
CREATE TEMP TRIGGER _repair_note_dates_guard_check BEFORE INSERT ON _repair_note_dates_guard
BEGIN
    SELECT RAISE(ABORT, 'migration 015: note id=' ||
        (SELECT id FROM notes
          WHERE date NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' LIMIT 1) ||
        ' (source ' ||
        (SELECT source FROM notes
          WHERE date NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' LIMIT 1) ||
        ') still has a malformed date: ' ||
        (SELECT date FROM notes
          WHERE date NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' LIMIT 1))
    WHERE (SELECT COUNT(*) FROM notes
            WHERE date NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]') > 0;
END;
INSERT INTO _repair_note_dates_guard VALUES (1);
DROP TRIGGER _repair_note_dates_guard_check;
DROP TABLE _repair_note_dates_guard;
