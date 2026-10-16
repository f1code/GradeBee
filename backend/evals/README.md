# GradeBee LLM Evaluation Harness

Regression tests for extraction and report-generation quality, powered by [promptfoo](https://promptfoo.dev). On-demand only, not CI-gated.

## Design

Promptfoo owns the model call: response caching, per-test cost/latency, multi-model comparison by changing a provider `id:`. Prompt construction stays in Go: eval-cli builds a messages array and has no LLM client.

Two configs:
- **`promptfooconfig.extract.yaml`**: extraction, structured output (json_schema)
- **`promptfooconfig.report.yaml`**: report generation, per-axis llm-rubric scoring (see "Report scoring")

Adding a report model means editing the providers list only. Report rows wrap their model in `providers/regen-chain.js` (`config.inner`), which runs regenerate cases' feedback rounds.

## How it works

1. `make eval` builds `bin/eval-cli` from `cmd/eval-cli/`.
2. promptfoo calls eval-cli per test case:
   ```
   bin/eval-cli '{"vars":{...},"config":{"task":"build-extract-prompt"}}'
   bin/eval-cli '{"vars":{...},"config":{"task":"build-report-prompt"}}'
   ```
3. eval-cli outputs a messages array: `[{"role":"system","content":"..."},{"role":"user","content":"..."}]`
4. promptfoo sends it to the provider, folds extraction responses into notes with `scoring/assemble.js`, and scores against the assertions.
5. `make eval` prints one diff per domain against its baseline (see [Baseline lifecycle](#baseline-lifecycle)).

## Running

```bash
# Prerequisites: MISTRAL_API_KEY (extraction),
# OPENROUTER_API_KEY (canonical report row),
# ANTHROPIC_API_KEY or a Claude Code login (report judge)
# the local DB at ../data/gradebee.db and the main tree's data/eval-references/
# for report cases (see "Report cases")

cd backend && make eval            # both domains, diff vs baseline
cd backend && make eval-extract    # extraction only
cd backend && make eval-report     # report only
cd backend && make eval-fixtures   # regenerate report cases (eval/eval-report do it when stale)
cd backend && make eval-baseline   # re-pin both baselines after a deliberate change
```

**Empty output?** `make eval` exports `.env`, whose `LOG_LEVEL=DEBUG` is not a promptfoo level; promptfoo then prints nothing and exits 0. Run `LOG_LEVEL= make eval`.

## Environment variables

| Variable | Required | Notes |
|---|---|---|
| `OPENAI_API_KEY` | No | Only for an OpenAI comparison row you uncomment |
| `MISTRAL_API_KEY` | Yes | Extraction rows (and the Medium report row, when uncommented) |
| `OPENROUTER_API_KEY` | Yes (for reports) | Canonical report row (`openrouter:openai/gpt-6-luna` on the EU host) |
| `ANTHROPIC_API_KEY` | Yes (for reports) | Report judge (Claude Agent SDK); a Claude Code login works instead |
| `LLM_PROVIDER` | No | `eval-cli gen-report` picks providers like the server (`LLM_PROVIDER`, `LLM_PROVIDER_REPORT`); graded providers come from the config |

## Which model the evals grade

Each config runs one **canonical** provider plus optional **comparison** providers.

| Config | Canonical label | Model |
|---|---|---|
| `promptfooconfig.extract.yaml` | `gradebee-extract` | `mistral-medium-3-5` |
| `promptfooconfig.report.yaml` | `gradebee-report` | `openrouter:openai/gpt-6-luna` (EU host) |

The canonical provider must match the model production runs (`defaultModels()` in `backend/llm_provider.go`, ADR 0006). `diff-baseline.js` counts regressions on canonical rows only. `TestEvalConfigsTrackProductionModels` (`backend/evals_config_test.go`, runs under `make test`, no API key) fails if a canonical `id` drifts from `defaultModels()`; it hardcodes the deployed provider per task and cannot see `LLM_MODEL_*` overrides. **Changing a production model: update `defaultModels()` and the config together, then re-pin the baseline.**

Comparison providers are unconstrained. Extraction pins no `temperature`, so treat small deltas on comparison rows as noise.

### Cheaper models

None is graded. Measured and rejected:

- `mistral-small-2603`: files off-roster names under a listed child (`wrong_class_group`), spreads a two-child remark to the whole class (`fuzzy_name_matching`), drops shared sentences (`shared_clause`), and its pass-1 class pick is unstable across identical replays.
- `mistral-large-2512`: same `wrong_class_group` / `shared_clause` misses, lower report score, heavy rate limits.
- `deepseek/deepseek-v4.1-flash` (OpenRouter, commented out in the extract config): matched Medium on extraction (70/70), slower; pass 1 and reports unmeasured. Config needs `passthrough.reasoning.enabled: false` (reasoning exhausts `max_tokens` and `assemble.js` fails to parse it) and `provider.require_parameters: true` (some endpoints ignore `response_format`).

## Debugging a single case

```bash
cd backend
make bin/eval-cli

# Build extraction prompt
./bin/eval-cli '{"vars":{"transcript":"Alice read well today.","class_name":"Grade 3A","classes":[{"name":"Grade 3A","students":[{"name":"Alice Chen"}]}]},"config":{"task":"build-extract-prompt"}}'

# Build report prompt
./bin/eval-cli '{"vars":{"student_name":"Alice Chen","class_name":"Grade 3A","notes":[{"date":"2026-01-15","summary":"Strong reading fluency."}],"report_instructions":"Two sections: Progress, Behaviour. Each with a Comment paragraph.","instructions":""},"config":{"task":"build-report-prompt"}}'
```

## Directory layout

```
evals/
  promptfooconfig.extract.yaml    extraction test suite
  promptfooconfig.report.yaml     report suite: judge, five axis rubrics, source-mention regex, gold-reference row; tests from tests.report.generated.yaml
  providers/regen-chain.js        wraps a report model: runs a regenerate case's feedback rounds, last output graded
  fixtures.manifest.json          curated report cases by id (committed, PII-free)
  tests.report.generated.yaml     report test list (generated, git-ignored)
  scoring/extraction.js           custom JS scorer (precision/recall + voice preservation + attribution)
  scoring/assemble.js             folds pass-2 passages into per-child notes before scoring
  scoring/report-wordcounts.js    appends per-section word counts to the report judge's copy of the output
  scripts/diff-baseline.js        baseline diff reporter (Node, always exits 0)
  scripts/pin-baseline.js         copies a result JSON to a baseline, scores only
  results/                        per-run result JSONs (git-ignored)
  fixtures/
    extraction/<case>/            committed, anonymized by hand
      transcript.txt              teacher audio transcript (synthetic)
      classes.json                class roster
      expected.json               expected students + must_quote_substrings / must_not_quote_substrings
    reports/<case>/               generated, git-ignored
      notes.json                  the student's notes in the case's date range
      report_instructions.txt     the Level's Report Instructions
      instructions.txt            the case's ad-hoc instructions (may be empty)
      reference.html              the reference: an accepted example for the judge, and the gold-reference row's output; for a regenerate case, the draft revised
      feedback.json               regenerate cases only: feedback comments, oldest first
```

## Report cases

Report cases hold real names, notes and report text, so the repo keeps only
`fixtures.manifest.json`: per case an `id` (directory name), a `description`
(no names), a `student_id` and optional `expect`: name-free `llm-rubric` lines
(metric `expect`, threshold 0.75) on top of the shared axes. A regenerate case
adds `report_id` and `feedback`. What agents may read:
`docs/adr/0005-report-eval-data-stays-local.md`.

A fresh case's reference lives in the main tree's reference store
`data/eval-references/<id>/` (override with `EVAL_REFS=`), outside the DB, so
swapping in a prod DB copy keeps it. Worktrees use the main tree's store.

```
data/eval-references/<id>/
  reference.html    the reference report
  meta.json         student_id, start_date, end_date, instructions (ad-hoc),
                    model, created_at, notes: [{id, updated_at}] it was written from
```

`make eval-fixtures` runs `eval-cli gen-report-cases`. For a fresh case it reads
the reference and `meta.json` from the store, and the student's name, class
display name, notes in range and Report Instructions from the local DB
(`../data/gradebee.db`; override with `EVAL_DB=`), through the resolver
`Generate` uses. It rewrites `fixtures/reports/` and
`tests.report.generated.yaml`, and fails naming the case for a missing
reference, a mismatched `student_id`, a range with no notes, or a Level
without Report Instructions.

When notes in range differ from those in `meta.json`, generation fails naming
the case and each missing, extra or edited note id. Write a new reference
(delete its directory, then `make eval-gen-report`) or drop the case.

`make eval` and `make eval-report` regenerate first when the manifest, the DB,
its WAL or a reference file is newer than `tests.report.generated.yaml`. Every
generated test takes task and rubric from the report config's `defaultTest`.

### Adding a report case

1. `make eval-gen-report CASE=id STUDENT_ID=N START=YYYY-MM-DD END=YYYY-MM-DD [INSTRUCTIONS='...']`
   drafts a reference through production `Generate`'s prompt (needs the
   provider's API key) and writes it to the store; refuses an existing id.
   Prints case, range, note count and a draft manifest entry, never the
   report. `START` must fall in the current term (on or after 2026-08-24).
2. `make eval-dump-report CASE=id` prints the reference and notes with roster
   names and aliases replaced by `STUDENT` / `CLASSMATE_n` (near misspellings
   included; `CLASSMATE_?` when ambiguous). Check it against the Level's
   Kids&Us sources. Terminal only: health and family details remain, and
   off-roster names still print. `STUDENT_ID=N REPORT_ID=M` dumps a DB report
   instead.
3. Paste the entry into `fixtures.manifest.json`; rewrite the description to
   say what the case tests, without names.
4. `make eval` to see the score.
5. `make eval-baseline` once it looks right.

### Regenerate cases

A regenerate case scores what `Regenerate` writes after Teacher feedback on a
draft. Its manifest entry adds:

- `report_id`: the draft round 1 revises. Written as `reference.html` and
  passed as `vars.draft`.
- `feedback`: `artifact_feedback` ids, oldest first, one per round. Each
  comment must be non-empty and on a report of the case's student. Text stays
  in the DB; `feedback.json` holds it.
- `expect` (optional): name-free rubric lines, e.g. "No cushions appear
  anywhere in the report."

Notes come from today's DB, as in production: a note changed after the draft is
a Teacher correction, not staleness, so generation does not fail on it. Such
notes keep `updatedAt` in `notes.json`, and `vars.draft_created_at` lets round
1's prompt tag them `(new)`, as production does.

`providers/regen-chain.js` runs the rounds on each row's model. Round 1 uses
promptfoo's prompt; each later round calls `eval-cli` with `vars.draft` set to
the previous output and `vars.round` to the round index. Round N's prompt lists
rounds 1..N-1 as earlier feedback, as in production. promptfoo grades the last
output; earlier ones sit in `response.metadata.earlierRounds`. The wrapper
caches through the CLI's `context.getCache` (its own promptfoo copy's cache
gets overwritten), keyed by inner model, repeat index and prompt; `--no-cache`
applies.

Besides the shared axes, each regenerate test gets `llm-rubric` lines, threshold
0.75, written by `gen-report-cases` with the case's text inline:

| Metric | One line per | Asks |
|---|---|---|
| `feedback` | round | the output does what that round asked |
| `kept` | case | facts of the draft no feedback touches survive |
| `new-notes` | case with notes changed since the draft | the output uses them |
| `expect` | `expect` entry | the line holds |

Lines sharing a metric average in `namedScores`. They carry no word-count
transform: promptfoo inlines `file://` strings in an external tests file, so
the transform would arrive as source text. The judge sees the draft as its
reference. Regenerate tests run on `gradebee-prompt` only: the gold-reference
row would echo the draft.

## Report scoring

The judge is `anthropic:claude-agent-sdk` on `claude-opus-5-5`, one turn, no
tools, no user settings, a one-line system prompt. It uses `ANTHROPIC_API_KEY`
if set, else the Claude Code login (`apiKeyRequired: false`), and needs
`@anthropic-ai/claude-agent-sdk` (pinned) from the root `pnpm install`. About
$0.05–0.06 per grading call plus a claude-haiku-5-5 housekeeping call (about
$0.0008). It runs outside the EU; ADR-0005 records the exception.

Anthropic wrote the reference reports, so the judge may favour them
(self-preference). Read gold-row and luna-row moves with that in mind.

Five `llm-rubric` assertions, one per axis, each a promptfoo `metric` reported
in `namedScores`: structure, grounding, completeness, tone, compliance. Each
axis gives a 1-5 anchor table; the judge returns it as 0-1 (1 = 0, 2 = 0.25,
3 = 0.5, 4 = 0.75, 5 = 1). The custom `rubricPrompt` asks for `{reason, score}`
only, so the per-assertion `threshold` decides pass: grounding needs 0.75, the
others 0.5. A row passes when every axis clears its floor. The row score is the
mean over the axes and the regex below, for trends only; one axis step moves it
about 0.04.

A sixth assertion, `not-regex` with metric `source-mention`, fails a report
that names its source ("the notes", "as recorded", "was described as") instead
of telling what happened in class. The canonical provider sets
`showThinking: false` so gpt-6-luna's reasoning stays out of the graded output.

Axis boundaries:

- **Compliance** excludes grounding, fact coverage, short length, sections and
  tone. It flags going over a length cap, a note fact in two sections or in a
  section that does not cover it. It reads Level rules together: a section with
  no fact of its own passes with general statements and its `[MISSING]` marker,
  a required example is a minimum, "each fact once" covers note facts only. A
  difficulty a later note shows resolved needs no what-we-do or marker.
- **Completeness** covers dropped note facts and falling short of a length.
- **Grounding** checks every time-ordered claim against note dates: a change
  needs an earlier and later note differing on the same skill, or a note
  stating it. A behaviour, routine or interaction no note shows ("waits her
  turn", "gets on well with the other children") is an invented fact, not a
  general statement.

The judge cannot count words, so `scoring/report-wordcounts.js`, an assertion
`transform`, appends per-section word counts (and tags sections holding a
`[MISSING` marker) to the judge's copy of the output. The rubric reads the
length rule from the instructions; none lives in code. A transform, not a
`nunjucksFilters` filter: promptfoo renders `rubricPrompt` with bare nunjucks,
so config filters never reach it.

The grading prompt carries the report prompt's note-filing statement: the
teacher filed every note to this student, and the name a note opens with,
however spelled, is this student.

### Gold-reference row

The `gold-reference` provider (`echo`, prompt `{{reference}}`) grades each
case's reference report. Its output never changes, so its movement is judge
noise. Non-canonical: never a regression. Measured row spread reaches 0.10, so
`make eval` counts a report move as a regression or improvement only beyond
±0.15 (`REPORT_DIFF_FLAGS`); extraction uses ±0.05. The report diff also lists
the axes that moved.

## Extraction scoring axes

`scoring/extraction.js` grades five hard axes plus one soft one; the assertion
passes only if the hard five do and nothing forbidden leaked.

| Axis | Fixture field | What it catches |
| --- | --- | --- |
| precision / recall | `expected_students[].name` | the wrong set of students was extracted |
| voice_preservation | `must_quote_substrings` | a student's own observation was dropped or paraphrased away |
| attribution | `must_not_quote_substrings` | cross-student bleed — another student's observation landed in this entry |
| (global) | `must_not_extract` | forbidden content leaked into any entry |
| (global) | `no_note_students` | a roster child who must get nothing got a note, whatever it says |
| (global) | `must_keep_substrings` | an observation was dropped as `none` instead of reaching a note or the unattributed list |
| preference (soft) | `should_quote_substrings` | text that makes a note better and whose absence is not a defect |

`should_quote_substrings` scores as the fraction matched and stays out of the
pass decision; skipped for fixtures that define none. It exists for taught
vocabulary the model carries into notes only some runs (e.g. the
`fuzzy_name_matching` drill lands about one run in three). The result also
carries a `hard` named score without the soft axis; `scripts/diff-baseline.js`
reads `hard` so a flaky preferred phrase does not trip the ±0.05 band.

`must_not_quote_substrings` is the per-student counterpart of
`must_not_extract`. Precision/recall compare only the *set* of names, so a run
copying the whole transcript into every student's `quoted_text` scores 1.00 on
them. Every multi-student fixture should list the other named students here.

Both substring fields accept a plain substring or `/pattern/flags` regex.

## What extraction grades

Production extraction is two calls: pass 1 picks the class (and returns the
spoken header), pass 2 reads the transcript against that class's roster and
returns passages. **This harness grades pass 2 only.** Each fixture names the
pinned class in `vars.class_name`, and its header, if any, in `vars.header`;
eval-cli cuts it with production's `CutHeader`, and a header that cuts nothing
fails the row. Pass 1 is graded in Go (`backend/llm_live_test.go`):
`TestLLM_DeclinesWhenNoHeaderPinsOneClass` (the `multi_class` fixture) and
`TestLLM_PassOneReturnsTheHeaderToCut`.

`scoring/assemble.js` folds pass-2 passages into notes with the same pronoun
guard and assembly rules as production: it is the JavaScript twin of
`guardPassages` (`backend/extract.go`) and `assemblePassages`
(`backend/voice_note_passages.go`). Change one, change both.

All fixtures score 1.000 on `gradebee-extract` except `fuzzy_name_matching`
(0.800: every hard axis passes; the soft drill lands in a `none` passage).

| Fixture | What it covers |
| --- | --- |
| `voice_preservation` | |
| `cross_student_bleed` | |
| `group_observation` | group remark reaches the whole pinned roster, never the sibling class |
| `shared_clause` | shared sentence reaches both children |
| `full_name_roster` | |
| `numbered_roster` | |
| `pronoun_run_bleed` | blocks owned by nobody reach no note |
| `date_drill` | group passage reaches every child |
| `roster_phantom` | unnamed block not filed under a listed child (negative measured in `research/2026-09-05-123-summaries-vs-spans/RESULTS.md`) |
| `absent_child` | child named absent keeps their own note |
| `absent_phrasing` | absence in wording the prompt does not spell out |
| `absent_group` | group remark skips the absent child |
| `wrong_class_group` | pass 2 after a wrong class pick: off-roster names suppress the group remark; no note |
| `placeholder_name` | pasted `STUDENT` placeholder reaches nobody; nothing dropped |
| `off_roster_name` | off-roster name reaches nobody; nothing dropped |
| `fuzzy_name_matching` | "Liana and Lucie did well. They…" comes back whole for each child, not split |

## Adding a fixture

1. Extraction: create `fixtures/extraction/<descriptive-name>/` with the files above and add a test entry to `promptfooconfig.extract.yaml` with flat `vars` (no `body` wrapper).
2. Report: see "Adding a report case".
3. Run `make eval` (or `make eval-extract` / `make eval-report`); if correct, `make eval-baseline`.

## Baseline lifecycle

One baseline per domain, both local only under the gitignored `data/`, both overwritten by `make eval-baseline`:

- **Extraction**: `data/eval-baseline-extract.json`. Outputs quote real teacher notes.
- **Report**: `data/eval-baseline-report.json`. Outputs hold real student names. On a fresh clone `make eval` skips the report diff until `make eval-baseline` pins one.

Copy both when setting up a worktree. `scripts/pin-baseline.js` keeps only what `diff-baseline.js` reads.

`make eval-baseline` pins the run its own `make eval` just made, and writes neither file unless both results are clean: it stops if a promptfoo run wrote no output or any row on any provider errored (`failureReason` 2), since an errored row scores 0 and would read as a regression later. Re-run once errors clear. Prefer re-pinning over hand-editing; `diff-baseline.js` keys on description plus provider, so dropping a row by hand is safe if you must.

## Report instructions and grounding

A Level's `report_instructions` defines structure, sections and content; ad-hoc
`instructions` (a teacher's per-run override) win where they conflict. The
shared axis rubrics in `defaultTest` never hardcode structure or length; they
say "as instructed" and the vars carry the specifics. Grounding is graded
regardless of instructions.

Grounding rules, from teacher review; the Level instructions, report prompt
and rubric all follow them:

- A report may complete a section with general statements about attitude,
  progress and time in class that fit the notes.
- A defect is a statement that contradicts a note, an invented specific fact
  (activity, incident, example, quote, mark, rating, date) or an invented
  weakness or problem.
- Dropping note facts or falling short of the Level's length is a
  completeness defect.
- Reports name "the teacher", never "we" or "us"; carry no dates; add no marker
  questioning a note.
- A section with no fact of its own gets one or two general statements and a
  `[MISSING: example of …]` marker, never another section's facts. A change
  over time needs notes on different dates showing it.
