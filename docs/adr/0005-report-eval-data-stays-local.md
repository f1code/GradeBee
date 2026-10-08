# Report eval data stays local; agents read it redacted

**Status:** accepted (2026-10-07, epic #32)

## Context & Decision

The repo is public. Report eval fixtures held real student names, teacher notes and report HTML,
and the Level Report Instructions they need are a school's private text (Kids&Us protocol).

Report eval cases are **ids, not data**. The repo commits `backend/evals/fixtures.manifest.json`:
per case a slug, a name-free description, `student_id` and `report_id`. `make eval-fixtures`
materializes each case from the local `data/gradebee.db` into gitignored files, through the same
`ReportInputResolver` production regenerate uses: names, class, notes in range, the Level's Report
Instructions, ad-hoc instructions, reference HTML. Both eval baselines live under `data/` too,
since their outputs carry names and note text. Extraction fixtures stay committed: they are synthetic or anonymized
by hand, and their transcripts are not in the DB.

**Agents read student content only through the redacted dump** (`make eval-dump-report`):
roster names and aliases become `STUDENT` / `CLASSMATE_n`, misspellings within the fuzzy limit
included. Content that holds names for a human to check goes to a file the agent does not read
(`-replacements`). This extends ADR-0003 to the authoring loop: a coding agent is a third-party
LLM call the teacher never chose.

Names still flow where they are the product, as in ADR-0003: the eval's report-generation and
judge calls see the same inputs production does.

**The report judge runs outside the EU** (#195): `google/gemini-3.8-flash` on Google Vertex
through OpenRouter, whose EU host does not serve it. Production report data stays EU-only
(ADR-0006); the eval judge is the exception. The OpenRouter account's data policy holds: zero
retention, no host that trains on prompts. The judge before it, gpt-6.1-sol on OpenAI direct,
also ran outside the EU.

References are fresh reports, generated under the Level's current instructions from
current-term notes, then reviewed against the Kids&Us sources, not reports a teacher once kept.

## Considered Options

- **Private submodule or cloud store for fixtures.** Rejected: a second access model for one
  local user, while the DB already holds every input.
- **Anonymized committed report fixtures.** Rejected: name swaps lower judge fidelity, and notes
  and reports still carry family and health detail no name swap removes.
- **Bulk cases generated from every student with a report.** Rejected: DB references are model
  output, and about five distinct instruction texts exist. A curated set covering each text
  replaces bulk.
- **Redacting every capitalized word mid-sentence.** Tried and dropped: common words never written
  lowercase ("Recently") vanished. The roster is the right list; the gap is misspelling, so fuzzy
  matching against it replaced the guess.

## Consequences

- A fresh clone has no report cases; `make eval` skips the report diff until a DB and a pinned
  baseline exist. Worktrees copy `data/`; DB-writing eval steps target the main tree's DB.
- Old fixtures stay in git history until the scrub (#171).
- Known redaction gaps: names off the class roster (other classes, the teacher), and
  transcriptions spelled past the fuzzy limit, notably the name a note opens with (#189).
  Print only the part you need when a dump's notes may hold one.
- Case inputs follow the DB: a teacher editing a Level's instructions changes the next run's
  inputs while the reference stays fixed. Snapshotting instructions per report is #185.
- Current-term rule (`gen-report` refuses a start before 2026-09-01) is hard-coded until report
  periods exist.
