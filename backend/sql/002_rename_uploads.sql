ALTER TABLE uploads RENAME TO voice_notes;

DROP INDEX IF EXISTS idx_uploads_user;
DROP INDEX IF EXISTS idx_uploads_cleanup;

CREATE INDEX IF NOT EXISTS idx_voice_notes_user ON voice_notes(user_id);
CREATE INDEX IF NOT EXISTS idx_voice_notes_cleanup ON voice_notes(processed_at)
    WHERE processed_at IS NOT NULL;
