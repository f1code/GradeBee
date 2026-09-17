-- AI cost per user for one day, ballpark: the provider invoice stays the source
-- of truth for totals. See docs/ai-usage.md.
--
--   sqlite3 -readonly -header -column data/gradebee.db < scripts/ai_cost.sql
WITH
-- USD. A price change reprices history. A model missing here costs $0 and
-- counts in unpriced_calls.
prices (model, per_1m_input, per_1m_output, per_minute) AS (
  VALUES
    -- https://mistral.ai/pricing/api, 2026-09-17. mistral-medium-2508 is no
    -- longer listed there; price from third-party trackers.
    ('mistral-medium-2508', 0.40, 2.00, NULL),
    ('voxtral-mini-latest', NULL, NULL, 0.003),
    -- https://developers.openai.com/api/docs/pricing, 2026-09-17
    ('gpt-5.4-mini', 0.75, 4.50, NULL),
    ('whisper-1', NULL, NULL, 0.006)
),
-- Yesterday, UTC. For another day: SELECT '2026-09-16'.
day (d) AS (SELECT date('now', '-1 day'))
SELECT
  c.user_id,
  count(*) AS calls,
  coalesce(sum(c.input_tokens), 0) AS input_tokens,
  coalesce(sum(c.output_tokens), 0) AS output_tokens,
  round(coalesce(sum(c.audio_seconds), 0) / 60.0, 1) AS audio_minutes,
  round(sum(
      coalesce(c.input_tokens * p.per_1m_input, 0) / 1e6
    + coalesce(c.output_tokens * p.per_1m_output, 0) / 1e6
    + coalesce(c.audio_seconds * p.per_minute, 0) / 60.0
  ), 4) AS usd,
  count(*) - count(p.model) AS unpriced_calls
FROM llm_calls c
-- A range on the raw column, so idx_llm_calls_created applies.
JOIN day ON c.created_at >= day.d AND c.created_at < date(day.d, '+1 day')
LEFT JOIN prices p ON p.model = c.model
GROUP BY c.user_id
ORDER BY usd DESC;
