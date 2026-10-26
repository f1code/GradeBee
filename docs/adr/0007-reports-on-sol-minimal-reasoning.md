# Reports on gpt-6.1-sol at minimal reasoning effort

**Status:** accepted (2026-10-26). Amends ADR 0006: model and effort only; provider, host and
conditions stand.

## Context & Decision

Report calls on gpt-6-luna at default effort reasoned ~3.4k tokens for a ~350-token report:
30 s median and 78 s p95 in production, with some calls hitting the 120 s timeout. Luna at low or
minimal effort cut reasoning to ~670 tokens but dropped compliance from ~0.65 to 0.40.

**Reports go to `openai/gpt-6.1-sol` with `reasoning_effort: minimal`**, same OpenRouter EU host
(Azure EU). Report eval, 15 cases, two uncached runs each:

| | luna default | sol minimal |
|---|---|---|
| row score | 0.85 | 0.865 |
| passes | 9.5 / 15 | 9.5 / 15 |
| grounding | 0.91 | 0.87 |
| latency p50 / max | 25-32 s / 83 s | 15 s / 25 s |

## Cost

$2.20 / $11 per 1M tokens vs luna's $0.11 / $0.55. Sol reasons ~170 tokens, so about $0.012 a
report vs luna's $0.0026, plus the Business platform fee.

## Considered Options

- **Luna at low or minimal effort.** Rejected: compliance fell to 0.40.
- **Luna at default effort with a completion-token cap.** Rejected: only trims the tail; the
  median stays at 30 s.
- **Mistral Medium 3.5.** Rejected again: 0 / 15 passes, grounding 0.60.

## Consequences

- Report cost rises about 4.5x.
- Privacy page names the new model.
- Sol's Azure EU endpoint is on OpenRouter's ZDR list (confirmed 2026-10-26).
