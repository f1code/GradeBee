---
name: generate-eval-fixtures
description: >
  Generate eval fixtures for the GradeBee promptfoo harness from real data
  in data/gradebee.db. Use when adding new extraction or report test cases
  to backend/evals/.
---

# Generating Eval Fixtures from the Database

This skill covers how to create grounded, real-data fixtures for the two eval
task types: **extraction** and **report generation**. All fixture data comes
from `data/gradebee.db`; never invent inputs or expected outputs.

See `docs/evaluation_harness.md` for harness architecture and how to run evals.

---

## Orientation Queries

Run these first to understand what data is available:

```bash
# Tables
sqlite3 data/gradebee.db ".tables"

# Notes with transcripts (shared across students in the same session)
# Only include rows with a non-empty transcript
sqlite3 data/gradebee.db "
SELECT n.id, s.name, c.name as class, n.date,
       length(n.transcript) as t_len, substr(n.transcript,1,120) as preview
FROM notes n
JOIN students s ON s.id = n.student_id
JOIN classes c ON c.id = s.class_id
WHERE n.transcript IS NOT NULL AND n.transcript != ''
ORDER BY n.id LIMIT 40"

# Students with many notes AND a generated report (good report candidates)
sqlite3 data/gradebee.db "
SELECT s.id, s.name, c.name as class,
       count(n.id) as note_count, max(r.id) as latest_report_id
FROM students s
JOIN classes c ON c.id = s.class_id
JOIN notes n ON n.student_id = s.id
JOIN reports r ON r.student_id = s.id
GROUP BY s.id
HAVING note_count >= 2
ORDER BY note_count DESC"

```

---

## Extraction Fixtures

### File layout

```
backend/evals/fixtures/extraction/<fixture_name>/
  transcript.txt   ← raw voice recording transcript (shared by all students in session)
  classes.json     ← teacher's class roster (real student names from DB)
  expected.json    ← which students to extract + key phrases from the transcript
```

### Strategy

1. **Pick a group of notes that share the same transcript.** Notes from the
   same session have identical `transcript` values. Look for interesting
   properties: name misspellings, absent students, students from multiple
   classes, or mixed performance levels.

2. **Pull the transcript** — it is stored in `notes.transcript` and is
   identical for all rows from the same session.

3. **Build `classes.json`** from the actual `classes` + `students` tables for
   the relevant class(es). Use real student names exactly as stored.

4. **Build `expected.json`.** The `must_quote_substrings` are phrases that
   must be traceable to the **transcript** (not the summary). The scorer
   (`scoring/extraction.js`) checks these against `quoted_text` — the passage
   the model copied from the transcript — so a phrase that only exists in
   `notes.summary` (a paraphrase) will permanently fail. Use the summary only
   to identify *which part* of the transcript is relevant for a given student,
   then find the matching wording in the transcript itself.

   **Prefer regex over exact strings.** Voice transcripts are messy: articles
   drop out, punctuation varies, words get run together. An exact-string match
   that passes today may fail tomorrow on a slightly different model output.
   Use `/regex/flags` syntax as your default — e.g.:
   - `"/making (?:the )?full sentences/i"` — optional article
   - `"/yes[,.]? please/i"` — flexible punctuation
   - `"/count(?:ing)? (?:from )?one to ten/i"` — verb form variation

   Only use a plain string when the phrase is short and distinctive enough
   that no variation is plausible.

   **Verify every phrase is anchored in the transcript before committing:**
   ```bash
   # For plain strings: must return > 0
   sqlite3 data/gradebee.db "
   SELECT instr(transcript, '<your phrase>') FROM notes WHERE id = <seed_id>"
   ```

5. **`must_not_extract` is for transcript phrases, not student names.**
   It checks whether a forbidden string appears inside any extracted student's
   `quoted_text`. Use it to catch transcript content that should never appear
   in output (e.g. a student from a different class whose name appears in the
   transcript, or a remark that should be ignored).

   To prevent a specific student from being extracted at all, simply **omit
   them from `expected_students`** — the precision score will catch any false
   positive. Do not put student names in `must_not_extract`.

#### expected.json schema

```json
{
  "expected_students": [
    {
      "name": "Student Name",
      "class": "Class Name",
      "must_quote_substrings": ["verbatim phrase from transcript", "/regex for variable phrasing/i"]
    }
  ],
  "must_not_extract": ["verbatim transcript phrase that must not leak into output"]
}
```

#### Interesting cases to look for

| Pattern | How to find it |
|---|---|
| Name misspelling in transcript | Compare student name in DB vs how teacher said it in transcript |
| Absent students | Transcript says "X was absent"; omit from `expected_students` |
| Multi-class session | Multiple distinct `class_id` values among notes with same transcript |
| Hallucination trap | Student mentioned in transcript but belongs to a different class — put their name as a `must_not_extract` phrase only if their name leaking into `quoted_text` would be the failure mode |

#### Example query — notes sharing a transcript

```bash
# Always guard against NULL transcripts
sqlite3 data/gradebee.db "
SELECT n.id, s.name, c.name, n.summary
FROM notes n
JOIN students s ON s.id = n.student_id
JOIN classes c ON c.id = s.class_id
WHERE n.transcript IS NOT NULL
  AND n.transcript != ''
  AND n.transcript = (SELECT transcript FROM notes WHERE id = <seed_id>)
ORDER BY n.id"
```

---

## Report Cases

Report cases live in the DB, keyed by id in
`backend/evals/fixtures.manifest.json`; `make eval-fixtures` materializes them
(git-ignored). To add one:

1. `make eval-add-report STUDENT_ID=N` (from the repo root or `backend/`)
   lists the student's reports and prints a draft entry for the newest usable
   one; `REPORT_ID=M` picks another.
2. Paste the entry into the manifest. Rewrite the description to say what the
   case tests, without names: the manifest is public.
3. `make eval`, then `make eval-baseline` once the score looks right.

See `backend/evals/README.md`, "Report cases".

### Selecting report cases

Pick cases that widen coverage, then hand the list to the user before any
manifest change.

**Content rule.** Read notes and reports only through
`make eval-dump-report STUDENT_ID=N REPORT_ID=M`. Skip the Orientation
Queries above (they print names and transcripts); never `SELECT` `s.name`,
`n.summary`, `n.transcript`, `r.html` or `r.instructions` with sqlite. The
dump redacts the class roster, misspellings included; names off the roster
(other classes, the teacher) can still appear.
Copy no proper noun into a description.

**Term rule.** Use current-term notes only: every range starts on or after
the term's first day (2026-09-01 for autumn 2026), never before a break.
Drop a case whose range then holds no notes.

**1. Survey students** (metadata only, all Levels with Report Instructions):

```bash
sqlite3 -header -column data/gradebee.db "
SELECT l.name AS level, c.id AS class, s.id AS student,
       count(n.id) AS notes, min(n.date) AS first, max(n.date) AS last,
       min(length(n.summary)) AS min_len,
       CAST(avg(length(n.summary)) AS INT) AS avg_len,
       max(length(n.summary)) AS max_len,
       (SELECT count(*) FROM reports r WHERE r.student_id = s.id) AS reports,
       (SELECT max(length(r.instructions)) FROM reports r
         WHERE r.student_id = s.id) AS max_adhoc_len
FROM students s
JOIN classes c ON c.id = s.class_id
JOIN levels l ON l.id = c.level_id
JOIN notes n ON n.student_id = s.id AND n.date >= '<term_start>'
WHERE l.report_instructions != ''
GROUP BY s.id
ORDER BY l.name, notes DESC"
```

Distinct instruction texts (Levels sharing one text count once):

```bash
sqlite3 data/gradebee.db "
SELECT group_concat(name, ', '), length(report_instructions)
FROM levels WHERE report_instructions != ''
GROUP BY report_instructions"
```

**2. Survey reports** of a candidate student, to find ranges and overlaps:

```bash
sqlite3 -header -column data/gradebee.db "
SELECT r.id AS report, r.start_date, r.end_date, count(n.id) AS notes,
       coalesce(length(r.instructions), 0) AS adhoc_len,
       substr(r.created_at, 1, 10) AS created
FROM reports r
LEFT JOIN notes n ON n.student_id = r.student_id
  AND n.date BETWEEN r.start_date AND r.end_date
WHERE r.student_id = <student_id>
GROUP BY r.id"
```

**3. Rank by coverage.** Cover each distinct instruction text at least once,
then widen along:

- ad-hoc instructions present / absent
- note count: 1-2, typical, many
- long range with sparse notes
- regenerated reports (overlapping ranges for one student)
- very short notes (often absences) / very long notes
- two students in one class (consistency)

**4. Read each pick** through the dump and write a description saying what
the case tests. A student with no report in the range has nothing to dump:
describe from survey metadata only (note count, span, lengths).

**5. Output** per case: student id, start, end, source report id (optional),
ad-hoc instructions to use (if any; propose name-free text when the axis needs
one the DB lacks), description. Save it under `../research/` (local, never
committed). Generate fresh reports from student, range and ad-hoc text under
current Level instructions before pasting entries into the manifest; the user then reviews the manifest diff.

---

## Wiring into promptfooconfig.yaml

After creating files, add a test entry to `backend/evals/promptfooconfig.yaml`.

### Extraction entry template

```yaml
- description: "extraction: <what makes this case interesting>"
  providers:
    - gradebee-extract
  vars:
    task: build-extract-prompt
    transcript: "file://fixtures/extraction/<name>/transcript.txt"
    classes: "file://fixtures/extraction/<name>/classes.json"
  assert:
    - type: is-json
    - type: javascript
      value: file://scoring/extraction.js
      config:
        expected: file://fixtures/extraction/<name>/expected.json
        metric: precision_recall   # label only — has no effect on scoring logic
```

**Critical:** Always pass `file://` paths through `vars:` fields and
interpolate them with `{{var_name}}` in the rubric. Never embed a `file://`
path directly inside the `llm-rubric` value string — promptfoo only resolves
`file://` in `vars:`, not in assertion text, and will hang or silently skip
the file.

---

## Verify

```bash
cd backend && make eval
```

Check the new test appears and passes (or fails for a legitimate reason that
the fixture is designed to catch).
