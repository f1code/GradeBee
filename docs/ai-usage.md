# AI usage per user

Ballpark AI cost per user. The provider invoice stays the source of truth for totals.

## What we record

`instrumentedProvider` (`backend/llm_provider.go`) writes one `llm_calls` row per provider call that got a response: extraction (both passes, and the class picker's pass 2), transcription, reports.

- `user_id`: the Clerk user. HTTP requests take it from the session; queue jobs from the job.
- `trace_id`: the recording, for queue jobs only. `NULL` for the class picker and reports.
- `model`: the name we sent, not the one the provider echoed.
- Chat fills `input_tokens` / `output_tokens`; transcription fills `audio_seconds`.
- `ok = 0`: a response came back but failed to decode or had no choices. Still billed.

Not recorded:

- Timeouts, network errors, non-2xx responses: no response to read usage from. Sentry metrics and logs have them.
- Retries: the SDK does none.
- Duration and error text: Sentry and logs.
- A call with no user on the context: logged as a warning, no row.

Rows stay forever; no account deletion path exists.

## Running the cost script

Copy a prod DB backup to `data/gradebee.db` (S3 bucket, `gradebee/db/<timestamp>.db`), then:

```bash
sqlite3 -readonly -header -column data/gradebee.db < scripts/ai_cost.sql
```

It reports yesterday (UTC), one row per user: calls, tokens, audio minutes, USD, `unpriced_calls`. For another day, edit the `day` CTE.

## Prices

The `prices` CTE in `scripts/ai_cost.sql` holds USD per 1M input tokens, per 1M output tokens and per audio minute. When a model or provider changes, add or update its row from the provider's pricing page.

- No effective dates: a price change reprices history.
- `unpriced_calls` counts calls whose model has no price row; they cost $0 in `usd`. Non-zero means a row is missing.
- `-latest` aliases (`voxtral-mini-latest`) move to new models, which may bill at a different price. Recheck the price when the alias moves.
