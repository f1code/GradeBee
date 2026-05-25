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

# Report examples keyed by class
sqlite3 data/gradebee.db "
SELECT re.id, re.name, rec.class_name, length(re.content)
FROM report_examples re
LEFT JOIN report_example_classes rec ON rec.example_id = re.id
ORDER BY re.id"
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

## Report Fixtures

### File layout

```
backend/evals/fixtures/reports/<fixture_name>/
  notes.json        ← [{date, summary}, ...] from notes table (the LLM input)
  examples.json     ← [{name, content}, ...] from report_examples (style anchors)
  instructions.txt  ← from reports.instructions (may be empty)
  reference.html    ← from reports.html (cleaned ground-truth output)
```

### Strategy

1. **Pick a student** with ≥ 2 notes and at least one generated report.
   Prefer students whose class has matching `report_examples` rows — this
   exercises the full pipeline including tone-matching.

2. **Build `notes.json`** from all `notes.summary` rows for that student,
   ordered by `date DESC` (newest first) to match production behavior.

3. **Build `examples.json`** by querying `report_examples` joined to
   `report_example_classes` filtered to the student's class name (or a
   parent class pattern). Include 2–4 examples maximum to keep context
   manageable.

4. **Write `instructions.txt`** from `reports.instructions` on the chosen
   report. Leave the file empty if instructions were blank.

5. **Choose the reference report.** When a student has many iterations,
   prefer the one with the **longest `html`** among those with a non-empty
   `instructions` field — this tends to be the most developed version.
   Use the `instructions` of that report as `instructions.txt`.

6. **Write `reference.html`** from `reports.html`. Then **clean it**:
   - Replace the real student name with a generic invented name (e.g. "Lucas",
     "Sophie"). Use the **same name** in `promptfooconfig.yaml`'s `student_name`
     and throughout the HTML.
   - Remove hallucinations: read each sentence and verify it traces back to a
     phrase in `notes.json`. Common patterns: generic praise ("shows respect",
     "with confidence"), foreign-language words, activities not in the notes.

#### Queries — fetch everything for one student

```bash
# Notes (newest first, matching production order)
sqlite3 data/gradebee.db "
SELECT date, summary FROM notes WHERE student_id = <id> ORDER BY date DESC"

# Choose reference: longest html with non-empty instructions
sqlite3 data/gradebee.db "
SELECT id, length(html) as html_len, instructions
FROM reports
WHERE student_id = <id> AND instructions != ''
ORDER BY length(html) DESC LIMIT 3"

# Fallback if no report has instructions
sqlite3 data/gradebee.db "
SELECT id, length(html) as html_len, instructions, html
FROM reports WHERE student_id = <id> ORDER BY id DESC LIMIT 3"

# Matching examples
sqlite3 data/gradebee.db "
SELECT re.id, re.name, re.content
FROM report_examples re
JOIN report_example_classes rec ON rec.example_id = re.id
WHERE rec.class_name LIKE '%<teacher_or_class_prefix>%'"
```

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

### Report entry template

```yaml
- description: "report: <what makes this case interesting>"
  providers:
    - gradebee-report
  vars:
    task: build-report-prompt
    student_name: "<invented name matching reference.html>"
    class: "<class name from DB>"
    notes: "file://fixtures/reports/<name>/notes.json"
    examples: "file://fixtures/reports/<name>/examples.json"
    instructions: "file://fixtures/reports/<name>/instructions.txt"
    reference: "file://fixtures/reports/<name>/reference.html"
  assert:
    - type: llm-rubric
      value: |
        Source notes (ground truth — every statement in the report must be traceable to these):
        {{notes}}

        Style examples (the report's tone and vocabulary should match these):
        {{examples}}

        Instructions given to the model:
        {{instructions}}

        Reference report (a known-good output for the same notes — use as a quality benchmark, student name may differ):
        {{reference}}

        <describe the specific structure/format requirements for this class type>
        Score 1-5 on:
        - structure: <expected sections>
        - grounding: every statement traceable to notes (no invented observations)
        - tone: warm, encouraging, matches examples
        - length: <any character/paragraph constraints from instructions>
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
