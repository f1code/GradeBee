-- One row per AI call that got a response, for cost per user (scripts/ai_cost.sql).
-- model is the name we sent, not the one the provider echoed, so it matches env
-- config and the price table. Chat fills the token columns; transcription fills
-- audio_seconds. Kept forever: no account deletion path exists yet.
CREATE TABLE IF NOT EXISTS llm_calls (
  id            INTEGER PRIMARY KEY,
  created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  user_id       TEXT    NOT NULL,
  trace_id      TEXT,
  provider      TEXT    NOT NULL,
  model         TEXT    NOT NULL,
  task          TEXT    NOT NULL,
  input_tokens  INTEGER,
  output_tokens INTEGER,
  audio_seconds INTEGER,
  ok            INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_llm_calls_created ON llm_calls(created_at);
