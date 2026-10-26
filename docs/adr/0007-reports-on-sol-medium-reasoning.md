# Reports on gpt-6.1-sol at medium reasoning effort

**Status:** accepted (2026-10-26). Amends ADR 0006: model and effort only; provider, host and
conditions stand.

## Context & Decision

Report calls on gpt-6-luna at default effort reasoned ~3.4k tokens for a ~350-token report:
30 s median and 78 s p95 in production, with some calls hitting the 120 s timeout. Luna at low or
minimal effort cut reasoning to ~670 tokens but dropped compliance from ~0.65 to 0.40.

**Reports go to `openai/gpt-6.1-sol` with `reasoning_effort: medium`**, same OpenRouter EU host
(Azure EU). Report eval, 15 cases, uncached:

| | luna default (2 runs) | sol minimal (3 runs) | sol medium |
|---|---|---|---|
| row score | 0.85 | 0.86-0.88 | 0.89 |
| passes | 9-10 / 15 | 8-11 / 15 | 12 / 15 |
| compliance | 0.62-0.72 | 0.60-0.65 | 0.77 |
| grounding | 0.90-0.93 | 0.83-0.90 | 0.97 |
| latency p50 / max | 25-32 s / 83 s | 15 s / 25 s | 22 s / 35 s |

Sol at minimal or low reasoned ~170-190 tokens and repeated facts across sections, broke ad-hoc
word limits, and once misattributed a fact. Medium (~600 tokens) fixed the repeats.

## Cost

$2.20 / $11 per 1M tokens vs luna's $0.11 / $0.55. About $0.019 a report vs luna's $0.0026, plus
the Business platform fee.

## Considered Options

- **Luna at low or minimal effort.** Rejected: compliance fell to 0.40.
- **Sol at minimal or low.** Rejected: 7 s faster, but compliance and grounding slips.
- **Luna at default effort with a completion-token cap.** Rejected: only trims the tail; the
  median stays at 30 s.
- **Mistral Medium 3.5.** Rejected again: 0 / 15 passes, grounding 0.60.

## Consequences

- Report cost rises about 7x.
- Privacy page names the new model.
- Sol's Azure EU endpoint is on OpenRouter's ZDR list (confirmed 2026-10-26).
- Sol overran a teacher's ad-hoc word limit (Toddler case: 61-74 words against ~50) at every
  effort tried. The report prompt now says an ad-hoc length wins and the least important facts go.
