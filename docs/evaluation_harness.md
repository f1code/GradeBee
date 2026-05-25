# Eval Harness Implementation Summary

**Status:** Implemented (2026-05-25)

## Overview

Evolved the evaluation harness from an HTTP-based endpoint (dual LLM calls, code duplication, auth overhead) to a streamlined exec-based CLI pipeline where promptfoo owns all LLM calls and eval-cli focuses purely on prompt construction.

## Architecture

```
Promptfoo → exec:../bin/eval-cli <subcommand> <prompt> <options> <context>
                        ↓
                  [Parse context.vars]
                  [Build prompt in Go]
                  ↓
                  [eval-cli outputs messages JSON]
                  ↓
Promptfoo (native OpenAI provider) → OpenAI API → JSON response
```

## Adding New Fixtures

Use the **`generate-eval-fixtures`** skill (`.agents/skills/generate-eval-fixtures/SKILL.md`).
It covers the full workflow for both extraction and report fixtures: orientation queries,
file layout, how to derive `expected.json` / `reference.html` from real DB data,
hallucination detection, and the `promptfooconfig.yaml` entry templates.

## How to Use

### Run Full Eval Suite
```bash
cd backend
make eval
```

### Run Baseline Capture
```bash
cd backend
make eval-baseline
```

### Debug Single Extraction Case
```bash
cd backend
./bin/eval-cli '{"vars":{"transcript":"Alice read well.","classes":[]},"config":{"task":"build-extract-prompt"}}'
```

### Debug Single Report Case
```bash
cd backend
./bin/eval-cli '{"vars":{"student_name":"Alice","class":"Grade 3A","notes":[],"examples":[],"instructions":""},"config":{"task":"build-report-prompt"}}'
```

## Incorporating User Feedback into Evaluation

### Feedback Collection Architecture (Production)

**Frontend & API:**
- **Report Viewer** (`frontend/src/components/ReportViewer.tsx`) — Thumbs-up/down buttons with optional comment textarea for reports
- **Note Cards** — Thumbs buttons on auto-extracted notes only (manual notes excluded)
- **Endpoint:** `POST /api/feedback` (handler: `backend/feedback_handler.go`)
- **Validation:** User must own the student; artifact must exist
- **Sentry Integration:** Thumbs-down also dual-writes to Sentry User Feedback widget for qualitative signals

**Database Storage:**
- **Table:** `artifact_feedback` (schema: `backend/sql/005_artifact_feedback.sql`)
- **Primary Data:** artifact type (report/note), artifact ID, rating (up/down), signal type (explicit/regenerated/edited/deleted)
- **Metadata:** user ID, timestamp, optional comment, previous value (for edits/deletes)
- **Indexes:** By artifact (fast feedback lookup), by rating+signal+time (analytics), by user (cascade delete)

**Repository Access:**
- `repo_feedback.go` provides:
  - `Insert()` — append-only (no updates; each event creates new row to preserve trajectory)
  - `ListByArtifact()` — all feedback for one report/note
  - `ListByUser()` — user's feedback since timestamp (with limit)
  - `CountSignals()` — lightweight aggregation for dashboards (returns `{"signal:rating": count}`)

**Implicit Signal Generation:**
- **Edit** (`backend/notes.go`): When teacher edits auto-extracted note, original value captured as `signal: 'edited'` with `rating: 'down'` iff content changed
- **Delete** (`backend/notes.go`): Note deletion stored as `signal: 'deleted'` with full note text in `previous_value`
- **Regenerate** (`backend/reports_handler.go`): Report regeneration stores user's feedback comment as `signal: 'regenerated'` with `rating: 'down'`
- **Manual notes:** No signals generated on edit/delete (only auto-extracted notes tracked)

### Feedback Collection (Production)

GradeBee collects two categories of feedback on LLM-generated content:

1. **Explicit Feedback** (thumbs ratings on reports and auto-extracted notes)
   - `POST /api/feedback` with `rating: 'up' | 'down'` and optional `comment`
   - Stored in `artifact_feedback` table with `signal: 'explicit'`
   - Includes teacher's optional text description of what went wrong

2. **Implicit Signals** (behavioral indicators of dissatisfaction)
   - `edited` — Teacher modifies an auto-extracted note (implies AI output was incomplete/inaccurate)
   - `regenerated` — Teacher requests report regeneration with feedback comment
   - `deleted` — Teacher removes an auto-extracted note entirely
   - Each signal stored with `rating: 'down'` + previous content preserved

### Workflow: From Production Feedback to Test Cases

**High-level process:**

1. **Mine Feedback Database** (off-band analytics job)
   - Query `artifact_feedback` table filtered by signal type and rating
   - Group by prompt version (extract vs. report) using `PromptVersionTag`
   - Collect failing cases: notes with high `edited`/`deleted` volume, reports with `thumbs_down` comments

2. **Convert to Test Data**
   - Extract original inputs (transcript for extraction; notes for report generation)
   - Store expected outcomes: what teachers corrected to, what they complained about
   - Tag with metadata (feedback signal type, user count, timestamps)

3. **Integrate into Eval Suite**
   - Add as new test cases in `backend/evals/` config or a separate feedback-driven test dataset
   - Run against baseline model and candidate models
   - Track improvement: do code/prompt changes reduce feedback signals on real-world data?

4. **Measure Impact**
   - Compare before/after: rate of edits, deletes, negative ratings on same cohort of data
   - Use `CountSignals()` repo method for lightweight dashboard metrics
   - Iterate: feedback loops inform both prompt engineering and model selection

**Why This Matters:**
- Synthetic eval data may miss real-world failure modes
- Production feedback reveals what teachers actually care about (tone, specificity, completeness)
- Implicit signals (edits/deletes) are stronger indicators of dissatisfaction than explicit ratings alone

## Environment Variables

| Var | Used By | Purpose |
|-----|---------|---------|
| `OPENAI_API_KEY` | eval-cli, production | OpenAI authentication |
| `EVAL_MODEL` | eval-cli only | Override model for evaluation (default: `ProductionModelName`) |

## Verification

- All existing unit tests pass (`cd backend && go test ./...`)
- All existing integration tests pass
- Eval baseline parity confirmed (same prompts + same default model)
- Production behavior unchanged (same model, same prompt construction)
