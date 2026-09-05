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
4. promptfoo sends the messages to the native provider (with structured output schema for extraction), folds the extraction response into notes with `scoring/assemble.js`, and scores the result against the assertions.
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
./bin/eval-cli '{"vars":{"transcript":"Alice read well today.","class_name":"Grade 3A","classes":[{"name":"Grade 3A","students":[{"name":"Alice Chen"}]}]},"config":{"task":"build-extract-prompt"}}'

# Build report prompt (exec-prompt mode)
./bin/eval-cli '{"vars":{"student_name":"Alice Chen","class_name":"Grade 3A","notes":[{"date":"2026-01-15","summary":"Strong reading fluency."}],"report_instructions":"Two sections: Progress, Behaviour. Each with a Comment paragraph.","instructions":""},"config":{"task":"build-report-prompt"}}'
```

## Directory layout

```
evals/
  promptfooconfig.extract.yaml    extraction test suite
  promptfooconfig.report.yaml     report test suite
  baseline.json                   pinned baseline scores (committed, merged extract+report)
  scoring/extraction.js           custom JS scorer (precision/recall + voice preservation + attribution)
  scripts/diff-baseline.js        baseline diff reporter (Node, always exits 0)
  scripts/merge-results.js        merges multiple result JSONs into one
  results/                        per-run result JSONs (git-ignored)
  fixtures/
    extraction/<case>/
      transcript.txt              teacher audio transcript (synthetic)
      classes.json                class roster
      expected.json               expected students + must_quote_substrings / must_not_quote_substrings
  scoring/assemble.js             folds pass-2 passages into per-child notes before scoring
    reports/<case>/
      notes.json                  student notes
      report_instructions.txt     Level's report specification — required for any live test case, drives structure/content
      instructions.txt            ad-hoc per-run instructions (optional; override report_instructions where they conflict)
```

## Extraction scoring axes

`scoring/extraction.js` grades four hard axes plus one soft one; the assertion
passes only if the hard four do and nothing forbidden leaked.

| Axis | Fixture field | What it catches |
| --- | --- | --- |
| precision / recall | `expected_students[].name` | the wrong set of students was extracted |
| voice_preservation | `must_quote_substrings` | a student's own observation was dropped or paraphrased away |
| attribution | `must_not_quote_substrings` | cross-student bleed — another student's observation landed in this entry |
| (global) | `must_not_extract` | forbidden content leaked into any entry |
| preference (soft) | `should_quote_substrings` | text that makes a note better and whose absence is not a defect |

`should_quote_substrings` scores as the fraction matched and is deliberately kept
out of the pass decision, so a run that drops it scores lower and still passes.
Taught vocabulary is what it exists for: "He was doing good with making the full
sentences. Yes, I can. No, I can't." names the structure, where the first
sentence alone does not — but the model carries the drill into the notes only
some runs, and the run that misses it is not a bad recording. The axis is
skipped entirely for a fixture that defines none, so adding it moved no other
row's score.

The result also carries a `hard` named score: the same score with the soft axis
taken out. `scripts/diff-baseline.js` reads `hard` as the regression signal,
because a preferred phrase the model reaches only some runs swings a row by more
than the differ's ±0.05 band and would announce a regression nobody caused.

**This file owns the measured rate.** 15 runs of `fuzzy_name_matching`,
`mistral-medium-2508`, no cache: 10 runs carried no drill at all, 5 carried the
class drill to all five children, and **none** carried Théo's own drill into his
note. So the class drill lands about one run in three and his own never does —
worth knowing before anyone tunes the prompt for it.

`must_not_quote_substrings` is the per-student counterpart of `must_not_extract`.
It exists because precision/recall compare only the *set* of extracted names: a
run that copies the whole transcript into every student's `quoted_text` scores a
perfect 1.00 on them. That is exactly how the cross-student bleed regression
reached production unnoticed, so every new multi-student fixture should carry
`must_not_quote_substrings` listing the other named students.

Both substring fields accept a plain substring or `/pattern/flags` regex syntax.

## What extraction grades

Extraction is two model calls in production (#125): pass 1 names the class
from the class list alone, pass 2 reads the transcript against that one class's
roster and returns passages. **This harness grades pass 2 only.** promptfoo
makes one call per test, and pass 1 is a different prompt against a different
schema, so each fixture names the class pass 1 is taken to have pinned, in
`vars.class_name`. Pass 1 measured 93/93 on `mistral-medium-2508` over 31
samples. The case it exists for — declining a recording it cannot place (#127)
— is graded in Go, not here: see `multi_class` below.

`scoring/assemble.js` sits between the model and the scorer. Pass 2 returns
passages; `expected.json` and the four scoring axes describe notes. The
transform folds one into the other, applying the same pronoun guard and
assembly rules as production — it is the JavaScript twin of `guardPassages`
(`backend/extract.go`) and `assemblePassages`
(`backend/voice_note_passages.go`). Change one, change both, or the eval stops
grading what ships.

Scores are `gradebee-extract` (`mistral-medium-2508`), the run pinned in
`baseline.json` on 2026-09-05.

| Fixture | Score | State |
| --- | --- | --- |
| `voice_preservation` | 1.000 | green |
| `cross_student_bleed` | 1.000 | green |
| `group_observation` | 1.000 | green |
| `shared_clause` | 1.000 | green |
| `full_name_roster` | 1.000 | green |
| `numbered_roster` | 1.000 | green |
| `pronoun_run_bleed` | 1.000 | green — was 0.333. Two blocks are owned by nobody; passages are the unit that lets them reach no note. 5 runs in 5. |
| `date_drill` | 1.000 | green — was 0.000. A group passage reaches every child the recording named. 5 runs in 5. |
| `roster_phantom` | 1.000 | green — new. Note 694's shape at the roster order that produces the phantom. 5 runs in 5. |
| `fuzzy_name_matching` | 0.800 | **red — was 1.000.** See below. |

`multi_class` is no longer a row here. #127 gave pass 1 a `""` to return, so the
fixture's right answer is a decline — and a decline is pass 1's, while every row
in this config is a pass-2 row: `build-extract-prompt` is the pass-2 builder and
takes a `class_name` var, which is the very thing that fixture withholds. It
moved to `backend/llm_live_test.go`
(`TestLLM_DeclinesWhenNoHeaderPinsOneClass`), which builds the real
`classPickSchema` — the one function #127 changed — where a promptfoo row would
score a re-implementation of it. The fixture files stay where they are; that
test reads them.

### The one regression: `fuzzy_name_matching`

"Liana and Lucie did well. They did well with Marcia's playing, Marcia's
jumping." should come back once per child with the same summary. The model
splits it instead: Lucie gets both sentences and Lina gets only "Liana did
well", so Lina's note loses her half. Measured 2 runs in 8 green on
`mistral-medium-2508`; every other axis of that fixture, including resolving
`Inaia`→`Inaya` and `Liana`→`Lina`, is unaffected.

It is a cost of the contract, not of the wording: the per-child rule is the
text measured at 0/10 roster phantoms, and re-tuning it re-opens #99. The row
is pinned at 0.800 in `baseline.json`, so `diff-baseline` will not raise it
again — **#128 owns it**, together with the rest of what #125 left behind.

### `roster_phantom` and the negative it is paired with

`roster_phantom` is green with the prompt's no-elimination rules and the Go
guard. The other half — red without the rules and with the guard off — is not a
row here: a promptfoo row that must fail is a trap for the next reader, and the
rules-off prompt text does not exist in the shipped code to point a row at. The
measurement lives in
`research/2026-09-05-123-summaries-vs-spans/RESULTS.md`: on note 694's real
transcript, the unnamed block was filed under a listed child **8 runs in 10**
without the rules and **0 in 10** with them, and the guard removed 100% of what
was left across 280 runs with no false positive.

### Known trap: `LOG_LEVEL`

`make eval` exports the repo's `.env`, and promptfoo reads `LOG_LEVEL`. The
project sets `LOG_LEVEL=DEBUG`, which is not one of promptfoo's levels, and it
then prints nothing at all — no table, no summary, exit 0. Run
`LOG_LEVEL= make eval`, or unset it, if the output is empty.

## Adding a fixture

1. Create `fixtures/{extraction,reports}/<descriptive-name>/` with the required files — for a report fixture that means `report_instructions.txt` (the Level's report specification; the hard gate forbids generating from an empty one).
2. Add a test entry in the appropriate config file (`promptfooconfig.extract.yaml` or `promptfooconfig.report.yaml`) with flat `vars` (no `body` wrapper).
3. Run `make eval` (or `make eval-extract` / `make eval-report`) to see the score; if correct, run `make eval-baseline`.

## Baseline lifecycle

`baseline.json` is a single committed file overwritten by `make eval-baseline`. The PR diff is the audit trail for deliberate score changes.

**One exception on record.** #127 removed the `multi_class` row rather than changing a
score, and it was cut out of `baseline.json` by hand instead of regenerating. A full
`make eval-baseline` was run first and rejected: the extraction rows came back identical,
while four `report:` rows dropped 1.0–1.5 on the canonical provider — movement #127 cannot
cause, since it changed one const and some comments in `prompts_version.go` and the report
templates are byte-identical. A `--no-cache` re-run of the report eval came back at
baseline, confirming judge variance, and pinning the low run would have hidden the next
real report regression. `diff-baseline.js` keys on description plus provider, so dropping
two rows is a safe edit; the `prompt.raw` the file stores is pass 2's, which #127 does not
touch. Prefer regenerating. If you hand-edit, say so here.

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
