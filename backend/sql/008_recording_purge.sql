-- 008_recording_purge.sql
-- Add purged_at to track when the audio file was deleted post-transcription.
-- One-off: mark already-processed recordings as purged at the same time they
-- were processed (audio is cleaned up by the retention job anyway).
ALTER TABLE voice_notes ADD COLUMN purged_at TEXT;
UPDATE voice_notes
   SET purged_at = processed_at
 WHERE processed_at IS NOT NULL;
