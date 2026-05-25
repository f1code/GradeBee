# GradeBee LLM Evaluation Harness

Regression tests for extraction and report-generation quality, powered by [promptfoo](https://promptfoo.dev). On-demand only — not CI-gated.

## Why exec, not HTTP

The harness originally used an HTTP provider (server + `EVAL_TOKEN` + middleware). That approach added prod-leak surface, a background-process lifecycle in Make, and a duplicated GPT call path. The exec provider replaced it: `make eval` builds a CLI binary and promptfoo invokes it directly. No port, no token, no race condition. See `docs/plans/2026-05-25-eval-harness-switch-to-exec.md` for the full rationale.

## How it works

1. `make eval` builds `bin/eval-cli` from `cmd/eval-cli/`.
2. promptfoo reads `promptfooconfig.yaml` and, for each test case, invokes:
   ```
   bin/eval-cli extract         <prompt> <options_json> <context_json>
   bin/eval-cli generate-report <prompt> <options_json> <context_json>
   ```
3. `context_json` contains `vars` with the fixture data (transcript, notes, etc.).
4. The binary calls the same Go functions used in production (`BuildReportPrompt`, `GenerateReportHTML`, `NewGPTExtractorWithModel`) and writes `{"output": "..."}` JSON to stdout.
5. promptfoo scores the output against the assertions and writes a result JSON.
6. `make eval` prints a diff vs the pinned baseline.

## Running

```bash
# Prerequisites: OPENAI_API_KEY in env

# Run eval, print diff vs baseline
cd backend && make eval

# Update baseline after a deliberate prompt/model change
cd backend && make eval-baseline
# Then commit evals/baseline.json alongside the change
```

## Environment variables

| Variable | Required | Notes |
|---|---|---|
| `OPENAI_API_KEY` | Yes | Used by both the eval-cli and the judge model |
| `EVAL_MODEL` | No | Override the generation model (default: `ProductionModelName`) |

## Debugging a single case

```bash
cd backend
make bin/eval-cli

# Extract
./bin/eval-cli extract '' '{}' \
  '{"vars":{"transcript":"Alice read well today.","classes":[{"name":"Grade 3A","students":["Alice Chen"]}]}}'

# Generate report
./bin/eval-cli generate-report '' '{}' \
  '{"vars":{"student_name":"Alice Chen","class":"Grade 3A","notes":[{"date":"2026-01-15","summary":"Strong reading fluency."}],"examples":[],"instructions":""}}'
```

## Directory layout

```
evals/
  promptfooconfig.yaml          promptfoo test suite
  baseline.json                 pinned baseline scores (committed)
  scoring/extraction.js         custom JS scorer (precision/recall + voice preservation)
  scripts/diff-baseline.js      baseline diff reporter (Node, always exits 0)
  results/                      per-run result JSONs (git-ignored)
  fixtures/
    extraction/<case>/
      transcript.txt            teacher audio transcript (synthetic)
      classes.json              class roster
      expected.json             expected students + must_quote_substrings
    reports/<case>/
      notes.json                student notes
      examples.json             example report cards (optional)
      instructions.txt          additional instructions (optional)
```

## Adding a fixture

1. Create `fixtures/{extraction,reports}/<descriptive-name>/` with the required files.
2. Add a test entry in `promptfooconfig.yaml` with flat `vars` (no `body` wrapper).
3. Run `make eval` to see the score; if correct, run `make eval-baseline`.

## Baseline lifecycle

`baseline.json` is a single committed file overwritten by `make eval-baseline`. The PR diff is the audit trail for deliberate score changes.
