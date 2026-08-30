# GradeBee LLM Evaluation Harness

Regression tests for extraction and report-generation quality, powered by [promptfoo](https://promptfoo.dev). On-demand only — not CI-gated.

## Why promptfoo drives the LLM

Promptfoo owns the OpenAI call, not eval-cli. This unlocks promptfoo's native response caching (re-runs don't re-hit the model), cost/latency tracking per test, and multi-model comparison by changing the `id:` in `promptfooconfig.report.yaml` or `promptfooconfig.extract.yaml`. Prompt construction stays in Go — eval-cli is a pure prompt builder that outputs a messages array; it has no OpenAI client.

The harness is split into two domain-specific configs:
- **`promptfooconfig.extract.yaml`** — extraction tests with structured output (json_schema)
- **`promptfooconfig.report.yaml`** — report generation tests with llm-rubric scoring

Adding a new report model only requires editing the providers list in `promptfooconfig.report.yaml`; no per-test changes needed.

Previously the harness used `exec:` providers where eval-cli built the prompt **and** called OpenAI itself. That approach bypassed promptfoo's caching and tracking. See `docs/plans/2026-05-25-eval-harness-switch-to-exec.md` for the earlier exec-provider rationale and `docs/plans/2026-05-25-eval-harness-promptfoo-drives-llm.md` for this change.

## How it works

1. `make eval` builds `bin/eval-cli` from `cmd/eval-cli/`.
2. promptfoo reads both config files and, for each test case, calls the exec-prompt function:
   ```
   bin/eval-cli '{"vars":{...},"config":{"task":"build-extract-prompt"}}'
   bin/eval-cli '{"vars":{...},"config":{"task":"build-report-prompt"}}'
   ```
3. eval-cli outputs a JSON messages array (no LLM call): `[{"role":"system","content":"..."},{"role":"user","content":"..."}]`
4. promptfoo sends the messages to the native provider (with structured output schema for extraction) and scores the response against the assertions.
5. Results from both configs are merged into a single combined JSON.
6. `make eval` prints a diff vs the pinned baseline.

## Running

```bash
# Prerequisites: LLM_PROVIDER + the active provider's API key in env
# (OPENAI_API_KEY when LLM_PROVIDER=openai; MISTRAL_API_KEY when LLM_PROVIDER=mistral)

# Run both domains, print diff vs baseline
cd backend && make eval

# Run a single domain
cd backend && make eval-extract   # extraction only
cd backend && make eval-report    # report only

# Update baseline after a deliberate prompt/model change
cd backend && make eval-baseline
# Then commit evals/baseline.json alongside the change
```

## Environment variables

| Variable | Required | Notes |
|---|---|---|
| `OPENAI_API_KEY` | Yes (for OpenAI) | Used by promptfoo's native provider and the judge model |
| `MISTRAL_API_KEY` | Yes (for Mistral) | Required when `LLM_PROVIDER=mistral` |
| `LLM_PROVIDER` | No | `"openai"` (default for evals) or `"mistral"`; selects which API key is required |

> Model selection lives in `promptfooconfig.report.yaml` or `promptfooconfig.extract.yaml` (`providers[].id`). To test a different model, add a provider there — but see "Which model the evals grade" below before touching a canonical one.

## Which model the evals grade

Each config runs a **canonical** provider plus any number of **comparison** providers.

| Config | Canonical label | Model | Tracked by `diff-baseline.js` |
|---|---|---|---|
| `promptfooconfig.extract.yaml` | `gradebee-extract` | `mistral-medium-2508` | yes (★) |
| `promptfooconfig.report.yaml` | `gradebee-report` | `mistral-medium-2508` | yes (★) |

The canonical provider must grade the model **production actually runs** — `defaultModels()` in `backend/llm_provider.go`, which resolves to `mistral-medium-2508` for both extraction and report generation. `diff-baseline.js` counts regressions on canonical rows alone, so a canonical provider pinned to anything else means the regression signal describes a model we do not ship. That is exactly what happened before: the extraction config graded `mistral-small-2603` for its whole life while production ran `mistral-medium-2508`.

`TestEvalConfigsTrackProductionModels` in `backend/evals_config_test.go` parses both configs and fails if a canonical provider's `id` drifts from `defaultModels()`. It needs no API key and runs under `make test`. It checks the Mistral defaults only — the configs hardcode `mistral:`-prefixed provider ids, so a deployment overriding `LLM_PROVIDER` or `LLM_MODEL_*` is outside what this guard can see. **If you deliberately change a production model, update `defaultModels()` and the config together, then regenerate the baseline.**

Comparison providers are unconstrained — they exist to measure other models and the test ignores them. `gradebee-extract-small` (`mistral-small-2603`) is kept as the weaker model: a prompt change that outgrows it shows up as a widening gap against the canonical row before it costs anything in production. Note that extraction providers pin no `temperature`, so comparison scores move a little between runs; treat small deltas on non-canonical rows as noise.

## Debugging a single case

```bash
cd backend
make bin/eval-cli

# Build extraction prompt (exec-prompt mode)
./bin/eval-cli '{"vars":{"transcript":"Alice read well today.","classes":[{"name":"Grade 3A","students":["Alice Chen"]}]},"config":{"task":"build-extract-prompt"}}'

# Build report prompt (exec-prompt mode)
./bin/eval-cli '{"vars":{"student_name":"Alice Chen","class_name":"Grade 3A","notes":[{"date":"2026-01-15","summary":"Strong reading fluency."}],"report_instructions":"Two sections: Progress, Behaviour. Each with a Comment paragraph.","instructions":""},"config":{"task":"build-report-prompt"}}'
```

## Directory layout

```
evals/
  promptfooconfig.extract.yaml    extraction test suite
  promptfooconfig.report.yaml     report test suite
  baseline.json                   pinned baseline scores (committed, merged extract+report)
  scoring/extraction.js           custom JS scorer (precision/recall + voice preservation)
  scripts/diff-baseline.js        baseline diff reporter (Node, always exits 0)
  scripts/merge-results.js        merges multiple result JSONs into one
  results/                        per-run result JSONs (git-ignored)
  fixtures/
    extraction/<case>/
      transcript.txt              teacher audio transcript (synthetic)
      classes.json                class roster
      expected.json               expected students + must_quote_substrings
    reports/<case>/
      notes.json                  student notes
      report_instructions.txt     Level's report specification — required for any live test case, drives structure/content
      instructions.txt            ad-hoc per-run instructions (optional; override report_instructions where they conflict)
```

## Adding a fixture

1. Create `fixtures/{extraction,reports}/<descriptive-name>/` with the required files — for a report fixture that means `report_instructions.txt` (the Level's report specification; the hard gate forbids generating from an empty one).
2. Add a test entry in the appropriate config file (`promptfooconfig.extract.yaml` or `promptfooconfig.report.yaml`) with flat `vars` (no `body` wrapper).
3. Run `make eval` (or `make eval-extract` / `make eval-report`) to see the score; if correct, run `make eval-baseline`.

## Baseline lifecycle

`baseline.json` is a single committed file overwritten by `make eval-baseline`. The PR diff is the audit trail for deliberate score changes.

## Report instructions authority

Reports are instruction-driven: a Level's `report_instructions` defines the required
structure, sections, and content, and the model must follow it. Ad-hoc
`instructions` (a teacher's per-run override) outrank `report_instructions` where
they conflict. Grounding-to-notes is graded as a separate, unchanged axis
regardless of instructions.

`promptfooconfig.report.yaml` passes `report_instructions` and `instructions` as
separate vars per test case and grades both with one shared rubric template
(`rubric_template` YAML anchor) rather than a bespoke rubric per fixture — the
rubric never hardcodes a structure or length rule that belongs in
`report_instructions` itself; it says "as instructed" and lets the var carry the
specifics.
