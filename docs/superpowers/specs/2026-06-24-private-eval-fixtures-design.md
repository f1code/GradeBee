# Private Eval Fixtures — Design Spec

**Date:** 2026-06-24
**Status:** Draft (pending user review)
**Related:** `backend/evals/` (promptfoo harness), `.agents/skills/generate-eval-fixtures/SKILL.md`

## Goal

Move eval fixture PII (real student names, voice transcripts, notes, report
HTML) out of the public git repo while (a) preserving curated regression
value and (b) scaling coverage by mechanically generating fixtures from the
local `data/gradebee.db`. Fixtures become **generated artifacts**, not
source. PII source of truth = the DB, which is already local and gitignored.

## Background & problem

- Repo `nicoglr/GradeBee` is **public**.
- `backend/evals/fixtures/` (49 files) is committed: real student names,
  voice transcripts, teacher notes, report HTML — all derived from
  `data/gradebee.db` (which is gitignored).
- Fixtures are a mix of mechanical DB pulls and hand curation:
  - **Extraction:** transcript + roster are mechanical; the curated value is
    `must_quote_substrings` (hand-tuned regex anchored in the transcript) and
    `must_not_extract` (which students must not appear).
  - **Reports:** notes/examples/instructions/reference are mechanical DB
    pulls; the curated value is the per-class rubric prose in
    `promptfooconfig.yaml`. (Report references are taken verbatim from the DB
    report the teacher retained as final/good — no manual de-hallucination.)
- The DB gives ground truth for the hard part of extraction: which students
  should be extracted = the students who got a `notes` row from that session.

## Non-goals

- No CI integration (evals run locally, by the owner, on demand).
- No sharing fixtures with other contributors (single-user access model).
- No secret/bucket infrastructure.
- No name anonymization — `fixtures/` is gitignored and generated locally,
  so real names are kept (improves rubric fidelity vs. the current
  name-swapped references).

## Architecture

The repo keeps three PII-free things; everything human comes from the DB at
generation time.

### 1. Manifest — `backend/evals/fixtures.manifest.json` (committed)

Curated fixtures keyed by stable DB ids, assertions de-named. Contains:

- **Extraction entries:**
  - `id` (slug, matches the current fixture dir name for continuity)
  - `seed_note_id` (a `notes.id` whose transcript identifies the session)
  - `providers` (list of provider labels, e.g. `["gradebee-extract"]`)
  - `metric` (label only, e.g. `"precision_recall"`)
  - `assertions`:
    - `expected_student_ids`: `[notes.student_id ...]` — the students to
      extract. Generator resolves to names when writing `expected.json`.
    - `must_not_extract_student_ids`: student ids whose names must not leak
      into any extracted `quoted_text`. Generator resolves to names.
    - `must_quote_substrings`: regex/snippets of teacher voice lifted from
      the transcript. **No names.** This is the only place real human text
      appears in the manifest; it is transcript-quote fragments, not PII.
- **Report entries:**
  - `id`, `student_id`, `report_id`
  - `providers` (list of provider labels)
  - `rubric`: the full `llm-rubric` `value:` prose (class-level structure
    rules, scoring criteria). PII-free. This relocates the ~250 lines of
    per-fixture rubric text currently inline in `promptfooconfig.yaml`.

No student/teacher names, no transcripts, no notes, no report HTML appear in
the manifest. Only numeric ids, teacher-voice regex snippets, and rubric
prose.

**Example:**
```json
{
  "extraction": [
    {
      "id": "fuzzy_name_matching",
      "seed_note_id": 123,
      "providers": ["gradebee-extract"],
      "metric": "precision_recall",
      "assertions": {
        "expected_student_ids": [10, 11, 12, 13, 14],
        "must_not_extract_student_ids": [15, 16],
        "must_quote_substrings": [
          "very good participation and motivation",
          "/making (?:the )?full structures/"
        ]
      }
    }
  ],
  "reports": [
    {
      "id": "mousy_with_instructions",
      "student_id": 7,
      "report_id": 42,
      "providers": ["gradebee-report", "gradebee-report-1", "gradebee-report-openai", "gradebee-report-3", "gradebee-report-4"],
      "rubric": "The report must follow the three-section structure ... Score 1-5 on: - structure ... - grounding ... - tone ... - length ..."
    }
  ]
}
```

### 2. Generator — `backend/evals/scripts/gen-fixtures.js` (committed)

Node script, run via `make eval-fixtures`. Reads the manifest + the local
DB and writes the gitignored `fixtures/` tree plus a generated test list.

**Curated fixtures:** for each manifest entry, resolve ids →
names/transcript/notes/reference from the DB and write the fixture dir:
- Extraction: `transcript.txt` (session transcript), `classes.json`
  (roster), `expected.json` (names filled in from
  `expected_student_ids`/`must_not_extract_student_ids` +
  `must_quote_substrings` copied through).
- Reports: `notes.json` (`notes.summary`, newest first),
  `examples.json` (matching `report_examples`), `instructions.txt`
  (`reports.instructions`), `reference.html` (`reports.html` verbatim,
  no name swap). `student_name` and `class` are read from the DB and
  emitted into the test list.

**Bulk fixtures (mechanical, no manifest entry):**
- Selection (inclusive defaults, tunable later):
  - Extraction: every distinct transcript session with ≥ 2 students
    noted, excluding sessions already covered by a manifest entry.
  - Reports: every student with ≥ 2 notes and ≥ 1 report, excluding
    students already in the manifest.
  - Cap at 50 fixtures per type to keep eval runs cheap.
- Assertions (coarse, derived from the DB — no curation):
  - Extraction: student-set precision/recall (expected set = students
    with a `notes` row in the session) + fabricated-quote guard (every
    extracted `quoted_text` must be a verbatim substring of the
    transcript). No `must_quote_substrings`.
  - Reports: generic rubric (structure + grounding + tone) vs. the
    name-verbatim DB reference.

**Test-list emission:** the generator emits
`backend/evals/tests.generated.yaml` containing the full `tests:` array —
resolved `student_name`/`class`, `file://` paths into `fixtures/`, rubric
prose inlined from the manifest, provider lists per test. This is what
promptfoo actually runs.

### 3. Harness config — `backend/evals/promptfooconfig.yaml` (committed)

Shrinks to shared boilerplate + a pointer to the generated tests:

```yaml
description: GradeBee LLM evaluation harness
prompts: [...]
providers: [...]
defaultTest: {...}
# Per-fixture tests are generated from fixtures.manifest.json + the DB
# by `make eval-fixtures` into tests.generated.yaml.
tests: file://tests.generated.yaml
```

All ~250 lines of per-fixture `tests:` blocks move out: rubric prose →
manifest; names + resolved paths → generated. `scoring/extraction.js`,
`scoring/`, `scripts/diff-baseline.js`, and `baseline.json` stay committed
unchanged in spirit.

**promptfoo external-tests loading mechanism:** the exact syntax for
pointing `tests:` at an external file (`file://tests.generated.yaml` vs.
`--config` merge vs. an `extends`-style include) is to be confirmed against
the installed promptfoo version during implementation. The intent is that
the committed base config remains the single source of boilerplate and the
generated file supplies only the `tests:` array.

### What is gitignored (generated, local-only)

- `backend/evals/fixtures/` (full materialized tree)
- `backend/evals/tests.generated.yaml`
- (already) `backend/evals/results/`

`.gitignore` gains:
```
backend/evals/fixtures/
backend/evals/tests.generated.yaml
```

## Scoring changes

`scoring/extraction.js` already consumes `expected.json` with resolved
names — no change for curated fixtures. Add a **coarse mode** so bulk
fixtures (which carry no `must_quote_substrings`) score on set
precision/recall + substring-hallucination guard only. Detection: **no
`must_quote_substrings` on any student** → coarse mode. This is the clean
signal: `must_quote_substrings` is exactly the hand-curated artifact that
bulk fixtures lack. (`must_not_extract` is not a reliable signal — a
curated fixture may legitimately have an empty `must_not_extract` list.)

The fabricated-quote guard (bulk) checks that every extracted
`quoted_text` is a verbatim substring of the session transcript. The
generator writes the transcript path into `expected.json` (a
`transcript_path` field) so the scorer can load it; curated fixtures
omit this field and the guard is skipped for them (their
`must_quote_substrings` already anchors quotes to the transcript).

## Make targets

- `make eval-fixtures` — runs `gen-fixtures.js`; writes `fixtures/` +
  `tests.generated.yaml`; refreshes a `.stamp` file recording manifest +
  DB mtime for staleness checks.
- `make eval` — gains a prerequisite: if `fixtures/` is missing or
  `.stamp` is stale vs. manifest/DB mtime, run `eval-fixtures` first;
  then run promptfoo as today.
- `make eval-add-fixture SEED_NOTE_ID=N` — scaffold helper for a new
  curated extraction fixture. Queries the DB for the session transcript,
  all students noted in the session (ids + names), and the class roster;
  prints a draft manifest entry + the full transcript to stdout for the
  user to fill in `must_quote_substrings` and pick
  `must_not_extract_student_ids`. The user appends the finalized entry
  to `fixtures.manifest.json` and commits.
- `make eval-add-report STUDENT_ID=N` — scaffold helper for a new
  curated report fixture. Queries the DB for the student's notes,
  candidate `report_id`s (longest html first), and matching
  `report_examples`; prints them for the user to pick a `report_id`,
  write the `rubric`, and append to the manifest.

## Migration (one-time)

A migration script reads the currently-committed `fixtures/` tree and
reverse-resolves it against the DB to emit the initial
`fixtures.manifest.json`:

- Extraction: for each existing fixture dir, find the `seed_note_id`
  by matching its `transcript.txt` to `notes.transcript`; resolve
  `expected_students[].name` → `students.id` via class + name;
  resolve `must_not_extract` literal names → `students.id`; copy
  `must_quote_substrings` regexes through verbatim; carry `metric`
  and provider list from the current `promptfooconfig.yaml`.
- Reports: for each existing dir, read `gold_report_id.txt` to get
  `report_id`; resolve `student_name` + `class` → `students.id`;
  carry the provider list and the full rubric `value:` text from
  the current `promptfooconfig.yaml` into `rubric`.

Result: a manifest that reproduces the current curated set exactly,
minus PII. After verifying `make eval` still reproduces the current
baseline, delete the committed `fixtures/` dir and the per-fixture
`tests:` blocks from `promptfooconfig.yaml`.

## History scrub (required, separate step)

The current fixtures are in the **public** git history. Adding a
`.gitignore` entry and deleting the files in a new commit does **not**
remove them from history — old commits remain fetchable. Required:

1. After the cutover (fixtures deleted, manifest in place, evals green),
   run `git filter-repo` (or BFG) to purge `backend/evals/fixtures/`
   from all history.
2. Force-push (`git push --force`).
3. Anyone with an existing clone must re-clone or `git pull --rebase`
   with history replacement.
4. Because the data was already public, consider whether affected
   transcripts/reports need rotation in the DB itself (out of scope for
   this design; flagged as a residual risk).

This step is deliberately separate from the code/manifest migration so
it can be done with care and is not entangled with the feature work.

## Risks & open questions

1. **Residual manifest exposure:** the manifest contains teacher-voice
   regex snippets (e.g. `"very good participation and motivation"`) and
   numeric ids. No names. Accepted by the user as safe for the public
   repo. If a snippet itself is deemed sensitive for a given fixture,
   that fixture can be moved to bulk-only (drop its manifest entry).
2. **promptfoo external-tests syntax:** to be confirmed against the
   installed version during implementation (see §Harness config).
3. **Bulk cap / thresholds:** 50/type and ≥2-students/≥2-notes are
   inclusive defaults chosen for breadth; expected to be tuned after a
   first run. Not a correctness risk.
4. **Report reference quality:** references are the DB report the
   teacher retained (assumed good). No manual de-hallucination. If a
   retained report contains a hallucination the model is scored against,
   it could mask a real regression. Mitigation: the rubric's grounding
   criterion independently penalizes ungrounded output, so a bad
   reference does not auto-pass the model.
5. **Staleness:** `make eval` regenerates when the manifest or DB mtime
   changes, but if the DB is edited without an mtime change (sqlite
   can preserve mtime on some operations), the user may need to
   `make eval-fixtures` manually. Low risk for a local single-user
   workflow.
6. **History scrub is destructive:** force-push rewrites public
   history. Must coordinate with any other clones. Separate step,
   explicit user gate.

## Out of scope

- CI integration / automated eval runs.
- Private submodule or cloud-store alternatives (Option B/C from
  brainstorming, rejected in favor of Option A: regenerate from DB).
- LLM-authored assertions (Approach 3, rejected in favor of curated core
  + coarse bulk).
