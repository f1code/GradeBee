-- 005_artifact_feedback.sql: Add artifact_feedback table for capturing
-- explicit thumbs ratings and implicit signals (regenerate/edit/delete).

CREATE TABLE IF NOT EXISTS artifact_feedback (
  id             INTEGER PRIMARY KEY,
  artifact_type  TEXT    NOT NULL CHECK (artifact_type IN ('report', 'note')),
  artifact_id    INTEGER NOT NULL,
  rating         TEXT    NOT NULL CHECK (rating IN ('up', 'down')),
  signal         TEXT    NOT NULL DEFAULT 'explicit'
                         CHECK (signal IN ('explicit', 'regenerated', 'edited', 'deleted')),
  comment        TEXT,
  previous_value TEXT,
  user_id        TEXT    NOT NULL,
  created_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Look up all feedback for a specific artifact.
CREATE INDEX IF NOT EXISTS idx_artifact_feedback_lookup
  ON artifact_feedback(artifact_type, artifact_id);

-- Mine by rating + signal + recency.
CREATE INDEX IF NOT EXISTS idx_artifact_feedback_rating
  ON artifact_feedback(rating, signal, created_at);

-- Look up per user (e.g. cascade delete when user account is removed).
CREATE INDEX IF NOT EXISTS idx_artifact_feedback_user
  ON artifact_feedback(user_id, created_at);
