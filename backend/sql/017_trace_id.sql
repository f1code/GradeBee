-- 017_trace_id.sql
-- A key from note to recording.
--
-- Until now nothing on a note named the recording it came from. The assign
-- endpoint's duplicate guard scoped by the recording's day, which is a proxy:
-- two recordings on one day share a scope, and an append's note dated by job
-- dispatch can miss across a UTC midnight. Neither the job (in memory, gone on
-- restart, rebuilt on retry) nor the row id (voice_notes has no AUTOINCREMENT,
-- so SQLite reuses the top id once retention deletes the newest row) can be
-- that key. So: a UUID minted at upload, on the row, copied to every note the
-- pipeline, assemble and assign make. Manual notes leave it null.
--
-- No foreign key: the recording row dies after seven days, the note does not.
-- Existing recordings get a random id so the unique index holds; existing notes
-- get none, as nothing can say which recording they came from.
ALTER TABLE voice_notes ADD COLUMN trace_id TEXT;
UPDATE voice_notes
   SET trace_id = lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-' || hex(randomblob(2))
                     || '-' || hex(randomblob(2)) || '-' || hex(randomblob(6)))
 WHERE trace_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_voice_notes_trace_id ON voice_notes(trace_id);

ALTER TABLE notes ADD COLUMN trace_id TEXT;
CREATE INDEX IF NOT EXISTS idx_notes_trace_id ON notes(trace_id);
