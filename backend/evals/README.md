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

> Model selection lives in `promptfooconfig.report.yaml` or `promptfooconfig.extract.yaml` (`providers[].id`). To test a different model, change the `id:` field there. Adding a new report model only requires editing the providers list in `promptfooconfig.report.yaml`.

## Debugging a single case

```bash
cd backend
make bin/eval-cli

# Build extraction prompt (exec-prompt mode)
./bin/eval-cli '{"vars":{"transcript":"Alice read well today.","classes":[{"name":"Grade 3A","students":["Alice Chen"]}]},"config":{"task":"build-extract-prompt"}}'

# Build report prompt (exec-prompt mode)
./bin/eval-cli '{"vars":{"student_name":"Alice Chen","class_name":"Grade 3A","notes":[{"date":"2026-01-15","summary":"Strong reading fluency."}],"examples":[],"instructions":""},"config":{"task":"build-report-prompt"}}'
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
      examples.json               example report cards (optional)
      instructions.txt            additional instructions (optional)
```

## Adding a fixture

1. Create `fixtures/{extraction,reports}/<descriptive-name>/` with the required files.
2. Add a test entry in the appropriate config file (`promptfooconfig.extract.yaml` or `promptfooconfig.report.yaml`) with flat `vars` (no `body` wrapper).
3. Run `make eval` (or `make eval-extract` / `make eval-report`) to see the score; if correct, run `make eval-baseline`.

## Baseline lifecycle

`baseline.json` is a single committed file overwritten by `make eval-baseline`. The PR diff is the audit trail for deliberate score changes.
