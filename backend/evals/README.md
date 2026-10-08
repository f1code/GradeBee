# GradeBee LLM Evaluation Harness

Regression tests for extraction and report-generation quality, powered by [promptfoo](https://promptfoo.dev). On-demand only — not CI-gated.

## Why promptfoo drives the LLM

Promptfoo owns the OpenAI call, not eval-cli. This unlocks promptfoo's native response caching (re-runs don't re-hit the model), cost/latency tracking per test, and multi-model comparison by changing the `id:` in `promptfooconfig.report.yaml` or `promptfooconfig.extract.yaml`. Prompt construction stays in Go — eval-cli is a pure prompt builder that outputs a messages array; it has no OpenAI client.

The harness is split into two domain-specific configs:
- **`promptfooconfig.extract.yaml`** — extraction tests with structured output (json_schema)
- **`promptfooconfig.report.yaml`** — report generation tests with per-axis llm-rubric scoring (see "Report scoring")

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
5. `make eval` prints one diff per domain against that domain's baseline (see [Baseline lifecycle](#baseline-lifecycle)).

## Running

```bash
# Prerequisites: MISTRAL_API_KEY (extraction, Medium comparison row),
# OPENROUTER_API_KEY (canonical report row, report judge)
# and the local DB at ../data/gradebee.db for report cases (see "Report cases")

# Run both domains, print one diff per domain vs its baseline
cd backend && make eval

# Run a single domain
cd backend && make eval-extract   # extraction only
cd backend && make eval-report    # report only

# Regenerate report cases from the DB (eval/eval-report do it when stale)
cd backend && make eval-fixtures

# Update both baselines after a deliberate prompt/model change
cd backend && make eval-baseline
```

## Environment variables

| Variable | Required | Notes |
|---|---|---|
| `OPENAI_API_KEY` | No | OpenAI comparison rows only |
| `MISTRAL_API_KEY` | Yes | Extraction rows and the Medium report row |
| `OPENROUTER_API_KEY` | Yes (for reports) | Canonical report row (`openrouter:openai/gpt-6-luna` on the EU host) and the report judge |
| `LLM_PROVIDER` | No | `eval-cli gen-report` picks providers like the server (`LLM_PROVIDER`, `LLM_PROVIDER_REPORT`); graded providers come from the config |

> Model selection lives in `promptfooconfig.report.yaml` or `promptfooconfig.extract.yaml` (`providers[].id`). To test a different model, add a provider there — but see "Which model the evals grade" below before touching a canonical one.

## Which model the evals grade

Each config runs a **canonical** provider plus any number of **comparison** providers.

| Config | Canonical label | Model | Tracked by `diff-baseline.js` |
|---|---|---|---|
| `promptfooconfig.extract.yaml` | `gradebee-extract` | `mistral-medium-3-5` | yes (★) |
| `promptfooconfig.report.yaml` | `gradebee-report` | `openrouter:openai/gpt-6-luna` (EU host) | yes (★) |

The canonical provider must grade the model **production actually runs** — `defaultModels()` in `backend/llm_provider.go` for the task's deployed provider: `mistral-medium-3-5` for extraction, `openai/gpt-6-luna` on OpenRouter for reports (ADR 0006). Medium stays a comparison row in the report config. `diff-baseline.js` counts regressions on canonical rows alone, so a canonical provider pinned to anything else means the regression signal describes a model we do not ship. That is exactly what happened before: the extraction config graded `mistral-small-2603` for its whole life while production ran `mistral-medium-2508`.

`TestEvalConfigsTrackProductionModels` in `backend/evals_config_test.go` parses both configs and fails if a canonical provider's `id` drifts from `defaultModels()`. It needs no API key and runs under `make test`. The deployed provider per task (extraction `mistral`, report `openrouter`) is written in the test, since it lives in env the test cannot read; `LLM_MODEL_*` overrides are outside what this guard can see. **If you deliberately change a production model, update `defaultModels()` and the config together, then regenerate the baseline.**

Comparison providers are unconstrained — they exist to measure other models and the test ignores them. Note that extraction providers pin no `temperature`, so comparison scores move a little between runs; treat small deltas on non-canonical rows as noise.

### Cheaper models, and why none is graded here

Neither config carries a cheaper comparison row. The two Mistral candidates were
measured in #164 and dropped; DeepSeek, measured since, sits commented out.
Re-adding one costs a run per eval, so read this first.

`mistral-small-2603` (a tenth of the canonical price) missed in three ways, over
5 runs per fixture, no cache:

- It files a spoken name that is on no roster under a listed child anyway
  (`wrong_class_group`, 4 runs in 5). That also defeats the suppression in
  `assemblePassages`, which relies on no name reaching a child, so a wrong pick
  writes a note for every child instead of showing the picker.
- A remark about two named children reaches the whole class
  (`fuzzy_name_matching`, 4 in 5).
- It drops the sentence shared by two children (`shared_clause`, 5 in 5).

Two fixes were probed against those runs. A roster-name check — clear `student`
when no spoken label resolves to the chosen child through `MatchStudent` — fixes
the first failure only: 54 to 56 passing runs in 66, and it fired on none of the
canonical model's outputs. A second model pass, the draft handed back with a
checklist of these weak areas, was a wash: it fixed the same first failure, broke
a name it had already matched, left the dropped sentence untouched, and cost a
third more tokens.

Pass 1 is the harder problem. Replayed over the stored recordings three times,
small declined 1, then 3, then 2 of 16 — same inputs, and every decline shows the
teacher a class picker instead of notes. Whether a given decline is right is not
known; the instability is what rules the model out.

`mistral-large-2512` missed `wrong_class_group` and `shared_clause` as well, and
scored below the canonical model on reports: 4.05 against 4.30 over 3 runs. This
account also rate-limits it hard enough that the Go live tests pass only one test
at a time, with pauses between them.

#### DeepSeek via OpenRouter

`deepseek/deepseek-v4.1-flash` through promptfoo's `openrouter:` provider,
commented out in `promptfooconfig.extract.yaml`. $0.30 / $1.20 per M tokens in /
out, half that off-peak.

Extraction, 5 runs per fixture, no cache, reasoning off:

| | `mistral-medium-3-5` | `deepseek-v4.1-flash` |
|---|---|---|
| Passing runs | 70/70 | 70/70 |
| Mean score | 0.989 | 1.000 |
| Median latency | 1.1s | 2.0s |

Mistral lost points on `fuzzy_name_matching` alone (0.84). DeepSeek passed
`wrong_class_group`, `fuzzy_name_matching` and `shared_clause` 5 in 5, the three
that ruled out small. The fixtures sit near ceiling, so this shows DeepSeek no
worse on these cases, not better.

Two config traps:

- Reasoning. promptfoo sends `max_tokens: 1024` by default; reasoning ends most
  calls there (`finish=length`, empty content), and promptfoo hands the reasoning
  text to `assemble.js`, which fails to parse it. A 16k cap still left 2 in 8
  `pronoun_run_bleed` calls reasoning past 14k with no answer.
  `passthrough.reasoning.enabled: false` fixes both.
- Routing. 4 of the 10 OpenRouter endpoints serving this model ignore
  `response_format`; `provider.require_parameters: true` keeps calls off them.

Not measured: pass 1, the class pick that ruled out small (graded only in
`backend/llm_live_test.go`, which has no OpenRouter path), and reports.

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
  promptfooconfig.report.yaml     report suite: judge, five axis rubrics, gold-reference row; tests from tests.report.generated.yaml
  fixtures.manifest.json          curated report cases by DB id (committed, PII-free)
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
      notes.json                  the student's notes in the report's date range
      report_instructions.txt     the Level's Report Instructions
      instructions.txt            the report's ad-hoc instructions (may be empty)
      reference.html              the report itself: an accepted example for the judge, and the gold-reference row's output
```

## Report cases

Report cases hold real names, notes and report text, so the repo keeps only
`fixtures.manifest.json`: per case an `id` (directory name), a `description`
(test description, no names), a `student_id` and a `report_id`.
Why, and what agents may read: `docs/adr/0005-report-eval-data-stays-local.md`.

`make eval-fixtures` runs `eval-cli gen-report-cases`, which reads each case
from the local DB (`../data/gradebee.db`; override with `EVAL_DB=`) through the
resolver regenerating that report uses: the student's name, the class display
name, the notes in the report's date range, the Level's Report Instructions and
the report's ad-hoc instructions. It rewrites `fixtures/reports/` and
`tests.report.generated.yaml`, and fails naming the case for an unknown id, a
report with no notes in its range, or a Level without Report Instructions.

Notes come from today's DB. If a note in the range was created or edited after
the report, the reference was written from other notes, so generation fails
naming the case, the report and the notes; pick a newer report. A note deleted
since the report goes unseen.

`make eval` and `make eval-report` regenerate first when the manifest, the DB or
its WAL is newer than `tests.report.generated.yaml`. Every generated test gets
its task and rubric from the report config's `defaultTest`.

### Adding a report case

0. No report yet, or only one written under older Level instructions:
   `make eval-gen-report STUDENT_ID=N START=YYYY-MM-DD END=YYYY-MM-DD [INSTRUCTIONS='...']`
   generates one through production `Generate` (needs the provider's API key)
   and prints its id only. `START` must fall in the current term (on or after
   2026-09-01). Read it through `make eval-dump-report` and check it against
   the Level's Kids&Us sources before trusting it as a reference.
1. `make eval-add-report STUDENT_ID=N` lists the student's reports, newest
   first: id, date range, created date, Level, whether the Level has Report
   Instructions, whether the report has ad-hoc instructions, notes in range.
   It prints a draft entry for the newest report not yet in the manifest that
   `make eval-fixtures` would accept; pick another with `REPORT_ID=M`. Output goes to the terminal only.
2. Optional: `make eval-dump-report STUDENT_ID=N REPORT_ID=M` prints the
   report's notes and reference HTML with class roster names and aliases
   replaced by `STUDENT` / `CLASSMATE_n`, misspellings within an edit or two
   included (`CLASSMATE_?` when two children are equally close), for reading
   the case before describing it. Terminal only: health and family details
   remain, and names off the roster (other classes, the teacher) still print.
3. Paste the entry into `fixtures.manifest.json`; rewrite the description to
   say what the case tests, without names.
4. `make eval` to see the score.
5. `make eval-baseline` once the score looks right.

## Report scoring

The judge is `openrouter:google/gemini-3.8-flash`, pinned to Google Vertex with
fallbacks off (#195). It needs `OPENROUTER_API_KEY`. No vendor under test may
judge: not OpenAI (luna, the canonical report model), Anthropic (edited the
references) or Mistral (the Medium row). OpenRouter's EU host does not serve it;
ADR-0005 records the exception. `omitDefaults` drops promptfoo's `max_tokens`
1024, which reasoning would exhaust, and `max_tokens: 32768` bounds a runaway.
No `temperature`. `showThinking: false` keeps the reasoning, which drafts its
own JSON, out of the graded answer.

#195 measured the candidates on the same cached luna and Medium outputs, plus
three `--no-cache` runs each of the gold rows and of nine fixed luna outputs
(an `echo` row like gold, since gold sat at the top of the scale):

| Judge | Judge cost a full run | Luna fixed-output row swing | Luna − Medium row |
| --- | --- | --- | --- |
| gpt-6.1-sol (before) | $1.09 | not measured | 0.817 − 0.618 (#190) |
| deepseek-v4.1-flash (Together) | $0.48 | 0.25 | 0.811 − 0.628 |
| deepseek-v4-pro-0813 (StreamLake) | $1.03, 4x flash's time | 0.25 | 0.817 − 0.661 |
| gemini-3.8-flash (Vertex), with the anchor below | $0.69 | 0.10 | 0.811 − 0.672 |

At `temperature: 0` DeepSeek flash looped to the token cap in 5 of 135 calls and
scored "No output" as 0. DeepSeek's own host is off: the account's data policy
blocks a host that trains on prompts. Gemini first passed invented behaviours
("waits her turn") as general statements, grounding Medium at 0.75; the
grounding anchor below brought it to 0.53. The non-OpenAI judges score luna as
sol did (0.81–0.82), so sol showed no sign of self-preference.

Five `llm-rubric` assertions, one per axis, each a promptfoo `metric`, so the
results JSON carries them in `namedScores`: structure, grounding, completeness,
tone, compliance. Each axis gives a 1-5 anchor table; the judge returns the
anchor as 0-1 (1 = 0, 2 = 0.25, 3 = 0.5, 4 = 0.75, 5 = 1). The custom
`rubricPrompt` asks for `{reason, score}` only, so the per-assertion
`threshold` decides pass, not the judge: grounding needs 0.75, the others 0.5. A
row passes when every axis clears its floor. The row score is the axis mean, for
trends only; one axis step moves it 0.05.

The axes do not grade each other: compliance leaves out grounding, fact
coverage, falling short of a length, sections and tone. Falling short of a
length is completeness; going over a cap is compliance. Compliance flags a note
fact in two sections or in a section that does not cover it (#188), and reads
the Level rules together: a section with no fact of its own passes with general
statements and its `[MISSING]` marker, a required example is a minimum, and
"each fact once" covers note facts, not general statements. Grounding checks
every time-ordered claim against the note dates: a change needs an earlier and
a later note that differ on the same skill, or a note stating it, so a trend or before/after story the
notes do not show is an invented fact. A behaviour, routine or interaction no
note shows ("waits her turn", "gets on well with the other children") is an
invented fact too, not a general statement (#195): a parent who knows the class
can tell it never happened.

The judge cannot count words (it said 63 for a section of 83), so code
measures: `scoring/report-wordcounts.js`, an assertion `transform`, appends
per-section word counts to the judge's copy of the output. The rubric says to
use them and read the length rule from the instructions; no length rule lives in
code. A section holding a `[MISSING` marker is tagged in the block, since a
section with no facts of its own may fall under the Level's minimum. A transform, not a `nunjucksFilters` filter: promptfoo renders
`rubricPrompt` with bare nunjucks, so config filters never reach it.

The grading prompt carries the same note-filing statement as the report prompt:
the teacher filed every note to this student, and the name a note opens with,
however spelled, is this student.

### Gold-reference row

The `gold-reference` provider (`echo`, prompt `{{reference}}`) grades each
case's reference report. Its output never changes, so its movement is judge
noise. Non-canonical: it never counts as a regression.

Three `--no-cache` runs on 2026-10-07 (#186): gold means 0.939, 0.928 and
0.911; every gold row passed in two runs, one row failed compliance in the
third. The largest row spread was 0.10 (one axis moving two steps), so `make
eval` counts a report move as a regression or improvement only beyond ±0.15
(`REPORT_DIFF_FLAGS`); extraction keeps ±0.05. The report diff also lists the
axes that moved.

#188 rewrote all nine references to its rules (no unsupported trajectory, each
fact in one section, `[MISSING]` marker for a section with no fact of its own).
Three `--no-cache` gold runs: means 0.917, 0.928 and 0.922; `tweens_pair_a`
failed grounding in all three on a change its one note states, which led to
the "or a note states it" clause; `very_short_last_note` failed compliance once.
The baseline re-pinned after that clause passes every gold row.

#195 swapped the judge and added the behaviour clause, which failed
`very_short_last_note` and `single_note` on "gets on well with the other
children"; both references dropped the phrase, in `data/gradebee.db`. Three
`--no-cache` gold runs then: every row passed, means 0.989 each, spread 0.00.
Nine fixed luna outputs, three runs: largest row spread 0.10, so the ±0.15 band
stands.

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

Since #155 pass 1 also returns the spoken header, and production cuts it before
pass 2. Each fixture with a header names it in `vars.header`, and eval-cli cuts
it with the same `CutHeader` production calls; a header that cuts nothing fails
the row. The values are pass 1's modal answers from
`research/2026-09-13-152-header-strip`; `TestLLM_PassOneReturnsTheHeaderToCut`
pins the live cut on `date_drill`.

`scoring/assemble.js` sits between the model and the scorer. Pass 2 returns
passages; `expected.json` and the four scoring axes describe notes. The
transform folds one into the other, applying the same pronoun guard and
assembly rules as production — it is the JavaScript twin of `guardPassages`
(`backend/extract.go`) and `assemblePassages`
(`backend/voice_note_passages.go`). Change one, change both, or the eval stops
grading what ships.

Scores are `gradebee-extract` (`mistral-medium-3-5`), the extraction baseline
pinned on 2026-09-17 by #164, with pass 1 cutting the header before
pass 2 (#155). `mistral-medium-2508` scored the same on every row.

| Fixture | Score | State |
| --- | --- | --- |
| `voice_preservation` | 1.000 | green |
| `cross_student_bleed` | 1.000 | green |
| `group_observation` | 1.000 | green — the group remark reaches the whole pinned roster, never the sibling class. |
| `shared_clause` | 1.000 | green — 6 runs in 7 on `mistral-medium-3-5`, no cache (#164); the miss gave the shared "colours" sentence to Bruno alone. |
| `full_name_roster` | 1.000 | green |
| `numbered_roster` | 1.000 | green |
| `pronoun_run_bleed` | 1.000 | green — was 0.333. Two blocks are owned by nobody; passages are the unit that lets them reach no note. 5 runs in 5. |
| `date_drill` | 1.000 | green — was 0.000. A group passage reaches every child. 5 runs in 5. |
| `roster_phantom` | 1.000 | green — new. Note 694's shape at the roster order that produces the phantom. 5 runs in 5. |
| `absent_child` | 1.000 | green — new. A child named absent keeps their own note. |
| `absent_phrasing` | 1.000 | green — new. Absence in wording the prompt does not spell out. |
| `absent_group` | 1.000 | green — new. A group remark skips the absent child, reaches everyone else. |
| `wrong_class_group` | 1.000 | green — a wrong pick on a declined card. Names off the roster suppress the group remark; no note. Pass 1 declines this transcript; the row is pass 2 after the pick. |
| `fuzzy_name_matching` | 0.800 | green — was 0.600 under #155's cut; every hard axis passes. See below. |

`multi_class` is no longer a row here. #127 gave pass 1 a `""` to return, so the
fixture's right answer is a decline — and a decline is pass 1's, while every row
in this config is a pass-2 row: `build-extract-prompt` is the pass-2 builder and
takes a `class_name` var, which is the very thing that fixture withholds. It
moved to `backend/llm_live_test.go`
(`TestLLM_DeclinesWhenNoHeaderPinsOneClass`), which builds the real
`classPickSchema` — the one function #127 changed — where a promptfoo row would
score a re-implementation of it. The fixture files stay where they are; that
test reads them.

### `fuzzy_name_matching`, the shared passage that was split

"Liana and Lucie did well. They did well with Marcia's playing, Marcia's
jumping." should come back once per child with the same summary. Under #125
the model split it instead: Lucie got both sentences and Lina only "Liana did
well", so Lina's note lost her half. 2 runs in 8 green, then 0 in 6 after
#155's header cut; the row sat pinned red at 0.600.

The cause was the contract sentence, not the per-child rule: "contiguous
passages … together covering the whole transcript" reads as a partition, and
the model honoured it by cutting the name list in two. #128 added three
sentences to the per-child rule: the shared passage runs to where the teacher
moves on, pronoun sentences included, and a copy shorter than another is
wrong. On cut transcripts, `mistral-medium-2508`, 10 runs per cell: 2/10 →
10/10, with date_drill, shared_clause, roster_phantom and pronoun_run_bleed
all holding 10/10. The rule's roster-phantom measurement stands: same text
plus an extent clause. Write-up: `research/2026-09-14-128-residue`.

The same failure in production, before the fix: 1 of 22 real recordings on
one run and 0 on the next, cut or uncut. The fixture is the hard case.

The row scores 0.800, not 1.000: every hard axis is green, and the soft
`should_quote_substrings` drill ("Yes, they can") lands in a `none` passage on
every run, before and after the fix.

### `wrong_class_group` was a coin toss

Re-running the suite for #128 turned this row red (0.625) with no change near
it, so it was re-measured uncut on `mistral-medium-2508`: 10 passes in 30 on
the prompt before #128, 4 in 30 with the fuzzy fix alone. The pass condition
(no note) holds only when the model returns Inès and Nathan as `child`
passages with a spoken label and an empty `student`, so the no-names rule can
suppress the group remark. The `unknown` bullet told it the opposite: "a name
that matches nobody listed" was listed as `unknown`, which carries no label.
#128 cut that clause. 30 runs in 30 afterwards; the other five measured rows
held 10/10. The then-untracked `mistral-small-2603` comparison row went the other way on this
row in the same run, 1.000 to 0.500; not measured further.

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

1. Extraction: create `fixtures/extraction/<descriptive-name>/` with the files above and add a test entry to `promptfooconfig.extract.yaml` with flat `vars` (no `body` wrapper).
2. Report: append an entry to `fixtures.manifest.json` naming a report whose Level has Report Instructions, then run `make eval-fixtures`.
3. Run `make eval` (or `make eval-extract` / `make eval-report`) to see the score; if correct, run `make eval-baseline`.

## Baseline lifecycle

One baseline per domain, both overwritten by `make eval-baseline`:

- **Extraction** — `data/eval-baseline-extract.json` at the repo root, local only. Outputs quote real teacher notes, so it stays out of the public repo.
- **Report** — `data/eval-baseline-report.json` at the repo root, local only. Report outputs hold real student names, so the file sits under the gitignored `data/` next to `gradebee.db`; copy both when setting up a worktree. On a fresh clone `make eval` skips the report diff with a message until `make eval-baseline` pins one. Losing it means re-pinning from a fresh run.

Both files keep only what `diff-baseline.js` reads; `scripts/pin-baseline.js` drops the per-run eval id, share URL and config, which change every run.

`make eval-baseline` pins the run its own `make eval` just made, and writes neither file unless both results are clean. It stops if a promptfoo run wrote no output, or if any row errored (`failureReason` 2: API outage, transform failure), on any provider, canonical or comparison: an errored row scores 0 and would read as a regression on every later run. Re-run once the errors clear.

The report cases came from #177 (2026-10-04): 9 cases, references generated by the production model under the Level instructions as revised after a teacher's review (see "What grounding means" below), then edited by Claude Opus to that standard and checked by a second reviewer. #186 re-pinned the baseline on 2026-10-07 from a `--no-cache` run under per-axis scoring (see "Report scoring"); scores before it used a different judge and scale and do not compare.

Both started out as one combined `baseline.json`. #172 split it by provider label, by hand, instead of regenerating: extraction rows unchanged and committed. The report rows were dropped, not moved under `data/`: they grade the old `fixtures/reports/` cases, which #173 replaces with cases generated from the DB, so they would never match a future run.

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
they conflict. Grounding is graded as a separate axis regardless of
instructions.

### What grounding means (teacher review, #177)

A teacher reviewed the first corrected references and rejected strict
grounding: references cut down to note-traceable sentences read as thin and
condensed, and the model's general statements ("settled in well", "understood
the instructions") were wanted. Since then:

- A report may complete a section with general statements about attitude,
  progress and time in class that fit the notes.
- A defect is a statement that contradicts a note, an invented specific fact
  (activity, incident, example, quote, mark, rating, date) or an invented
  weakness or problem.
- Completeness is graded: dropping note facts or falling short of the Level's
  length is a defect.
- Reports name "the teacher", never "we" or "us"; carry no dates; and add no
  marker questioning a note, because the teacher vets notes when writing them.
- Since #188: a section the notes give no fact of its own gets one or two
  general statements and a `[MISSING: example of …]` marker, never another
  section's facts; a change over time needs notes on different dates showing it.

The Level instructions, the report prompt and the rubric all follow these rules.

`promptfooconfig.report.yaml` passes `report_instructions` and `instructions` as
separate vars per test case and grades both with the shared axis rubrics in
`defaultTest` rather than a bespoke rubric per case — the
rubric never hardcodes a structure or length rule that belongs in
`report_instructions` itself; it says "as instructed" and lets the var carry the
specifics.
