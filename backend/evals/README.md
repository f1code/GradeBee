# GradeBee LLM Evaluation Harness

Regression tests for extraction and report-generation quality, powered by [promptfoo](https://promptfoo.dev). On-demand only, not CI-gated.

This file says what lives where. Models, rubrics and thresholds live in the configs; report rules live in each Level's Report Instructions. Read those, not a copy here.

## Design

Promptfoo owns the model call (caching, cost/latency, multi-model rows). Prompt construction stays in Go: `bin/eval-cli` (`cmd/eval-cli/`) builds a messages array and has no LLM client.

- **`promptfooconfig.extract.yaml`**: extraction, structured output, scored by `scoring/extraction.js`.
- **`promptfooconfig.report.yaml`**: report generation, scored per axis by an LLM judge.

`make eval` prints one diff per domain against its baseline.

## Running

```bash
cd backend
make eval            # both domains, diff vs baseline
make eval-extract    # extraction only
make eval-report     # report only
make eval-fixtures   # regenerate report cases (eval/eval-report do it when stale)
make eval-baseline   # re-pin both baselines after a deliberate change
```

Needs the keys for the providers in the configs (see `.env.example`); the report judge takes `ANTHROPIC_API_KEY` or a Claude Code login. Report cases need `../data/gradebee.db` and the main tree's `data/eval-references/`.

**Empty output?** `.env` sets `LOG_LEVEL=DEBUG`, which promptfoo rejects silently. Run `LOG_LEVEL= make eval`.

## Canonical providers

Each config has one **canonical** provider (★ in the diff) plus optional comparison rows. Only canonical rows count as regressions. The canonical model must be what production runs: `TestEvalConfigsTrackProductionModels` (`backend/evals_config_test.go`, under `make test`) fails if it drifts from `defaultModels()` in `backend/llm_provider.go`. Changing a production model: update both, then re-pin.

## Directory layout

```
evals/
  promptfooconfig.extract.yaml    extraction suite
  promptfooconfig.report.yaml     report suite: judge, axis rubrics, gold-reference row
  providers/regen-chain.js        runs a regenerate case's feedback rounds; last output graded
  fixtures.manifest.json          report cases by id (committed, PII-free)
  tests.report.generated.yaml     report tests (generated, git-ignored)
  scoring/extraction.js           extraction scorer
  scoring/assemble.js             folds pass-2 passages into notes, as production does
  scoring/report-wordcounts.js    gives the judge per-section word counts
  scripts/diff-baseline.js        baseline diff
  scripts/confirm-regressions.js  reruns flagged report cases to confirm them
  scripts/pin-baseline.js         writes a baseline from a result
  results/                        per-run results (git-ignored)
  fixtures/extraction/<case>/     committed, synthetic or anonymized
  fixtures/reports/<case>/        generated, git-ignored
```

## Report cases

Report cases hold real names and notes, so the repo keeps only `fixtures.manifest.json` (id, name-free description, `student_id`, optional `expect` rubric lines; regenerate cases add `report_id` and `feedback` ids, tweak cases a `tweak` block). What agents may read: `docs/adr/0005-report-eval-data-stays-local.md`.

A fresh case's reference lives in `data/eval-references/<id>/` of the main tree (`reference.html` + `meta.json` with range, ad-hoc instructions and the note ids it was written from). `make eval-fixtures` joins it with the DB's notes and Level instructions; it fails by name when the notes in range no longer match the reference. Fix: delete the case's directory and `make eval-gen-report` again, or drop the case.

### Adding a report case

1. `make eval-gen-report CASE=id STUDENT_ID=N START=YYYY-MM-DD END=YYYY-MM-DD [INSTRUCTIONS='...']` drafts a reference with production's prompt and prints a manifest entry.
2. `make eval-dump-report CASE=id [RESULT=evals/results/<run>-report.json]` prints notes, reference and (with `RESULT`) the gold row's judge reasons, roster names redacted. Terminal only. Review the reference against the Level text.
3. Paste the entry into `fixtures.manifest.json` with a name-free description.
4. `make eval`, then `make eval-baseline` once it looks right.

### Regenerate cases

Score what `Regenerate` writes after Teacher feedback. `report_id` is the draft; each `feedback` id is one round, oldest first. `regen-chain.js` replays the rounds like production. Extra rubric lines per test: `feedback` (each round done), `kept` (untouched draft facts survive), `new-notes` (notes changed since the draft used), `expect`. No gold row: it would echo the draft.

### Tweak cases

Score a paragraph tweak. `tweak.of` names a fresh case; its reference is the draft. `paragraph` numbers the `p`/`li` blocks as the server does, `ask` is the Teacher's ask (written in the manifest, so name-free), `chars` the text length the ask sets, `strip_level_examples` drops the Level's quoted example sentences. The output is the paragraph alone, so the default axes are off. Lines: `one-paragraph` and `length` (code), `tweak` (ask done, facts kept), `no-invention` (added sentences are notes, Level examples or class-wide statements), `expect`.

## Report scoring

The judge and its per-axis rubrics (structure, grounding, completeness, tone, compliance) are in `promptfooconfig.report.yaml` `defaultTest`; that file is the authority on what each axis grades and its pass threshold. A row passes when every axis clears its floor. A `not-regex` (`source-mention`) fails reports that cite "the notes". The judge runs outside the EU (ADR-0005).

The **gold-reference** row echoes each case's reference. Its movement is judge noise, and it never counts as a regression. Report diffs flag moves beyond ±0.15 (`REPORT_DIFF_FLAGS` in the Makefile), extraction beyond ±0.05. One report run is noisy (the model writes a new report each time), so `make eval` reruns each flagged report case 3 times, cache off, and confirms it only when 2 reruns regress too (`scripts/confirm-regressions.js`, output `results/<stamp>-report-confirm.json`). Pinning ignores reruns. If gold rows fail, read the judge reasons (`RESULT=` above): either the reference breaks the current Level text, or the rubric needs a clarification.

## Extraction scoring

`scoring/extraction.js` documents its axes and fixture fields (`expected.json`). The diff reads the `hard` score, which leaves out the soft `should_quote_substrings` preference.

The harness grades pass 2 only (transcript → passages for one pinned class). Pass 1 (class pick, header) is graded in `backend/llm_live_test.go`. `scoring/assemble.js` mirrors `guardPassages` (`backend/extract.go`) and `assemblePassages` (`backend/voice_note_passages.go`): change one, change both.

New extraction fixture: add `fixtures/extraction/<name>/` (`transcript.txt`, `classes.json`, `expected.json`) and a test entry in `promptfooconfig.extract.yaml`. Every multi-student fixture should list the other students in `must_not_quote_substrings`.

## Baselines

`data/eval-baseline-extract.json` and `data/eval-baseline-report.json`, local only (outputs carry real notes and names); copy them into worktrees. `make eval-baseline` pins its own fresh run and refuses if any row errored. Prefer re-pinning to hand-editing.
