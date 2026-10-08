# Reports on gpt-6-luna via OpenRouter EU; provider per task

**Status:** accepted (2026-10-07, task #193)

## Context & Decision

Mistral Medium 3.5 invents facts in report text: grounding 0.35 on the current Levels.
gpt-6-luna, same production prompt, one call, scored grounding 0.98, compliance 0.49 vs 0.25,
row 0.76 vs 0.57, about 74 words a section. It needs no compiled instructions and no length fix
(task 192, superseded). The judge is OpenAI too, so a hand-read of luna beside Medium gates the
production switch.

**Reports go to `openai/gpt-6-luna` through OpenRouter's EU host** (`eu.openrouter.ai`), which
serves it from Azure EU and fails closed when a model has no in-region endpoint.
**Extraction and transcription stay on Mistral direct.**

**Each task picks its provider by env**: `LLM_PROVIDER_EXTRACTION` / `_REPORT` /
`_TRANSCRIPTION`, each defaulting to `LLM_PROVIDER`. Production: `LLM_PROVIDER=mistral`,
`LLM_PROVIDER_REPORT=openrouter`. `OPENROUTER_BASE_URL` defaults to the EU host, so an unset var
never leaves the EU. No fallback to Medium when OpenRouter fails: the error surfaces.

## Conditions

- OpenRouter Business plan (needed for the EU host).
- Zero data retention enforced on the account; prompt logging off. The Azure EU endpoint is on
  OpenRouter's ZDR list.
- OpenRouter DPA (part of its ToS): SCCs for EEA transfers; EU API payloads processed and stored
  in the EU/EEA.

## Cost

Azure EU on OpenRouter: $0.11 / $0.55 per 1M tokens, reasoning billed as output, plus the 8%
Business platform fee. About $0.0018 a report vs Medium $0.006.

## Considered Options

- **OpenAI direct.** Rejected: report data would leave the EU.
- **Fix Medium's grounding with compiled instructions and a length rule (task 192).** Rejected:
  more pipeline for a weaker result.
- **One provider for every task.** Rejected: OpenRouter has no transcription API, and extraction
  on Mistral is not in question.

## Consequences

- A second vendor in the privacy page and the DPA chain.
- The eval's canonical report row runs on OpenRouter EU; Medium stays as a comparison row.
- Open: confirm Azure on OpenRouter's subprocessor list; keep a copy of the accepted DPA.
- Moving extraction to OpenRouter becomes env plus measurement (task 194).
