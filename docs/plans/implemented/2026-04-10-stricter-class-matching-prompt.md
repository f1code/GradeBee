# Stricter Class Matching in Extraction Prompt

## Goal
Prevent LLM from hallucinating class names during extraction. The prompt should instruct the model to use **only** class names from the roster, and the pipeline should handle mismatches gracefully.

## Proposed Changes

### Task 1: Prompt unit test
**File:** `backend/extract_test.go` (new)
- Add `TestBuildExtractionPrompt_ContainsClassConstraint` that calls `buildExtractionPrompt` with sample classes and asserts the output contains a rule about using only roster class names.
- Run: `go test -run TestBuildExtractionPrompt -v`

### Task 2: Tighten extraction prompt
**File:** `backend/extract.go`
- Add a rule to `buildExtractionPrompt`: "The class field for each student MUST exactly match one of the class names from the roster above. Do not invent or abbreviate class names."
- Existing test from Task 1 should now pass.
- Run: `go test -run TestBuildExtractionPrompt -v`

### Task 3: Wrong-class pipeline test
**File:** `backend/voice_note_process_test.go`
- Add `TestProcessJob_WrongClassSkipped`: extractor returns a student with a class not in the DB. Assert the student is skipped (ErrNotFound path) and job still completes successfully with remaining matches.
- Run: `go test -run TestProcessJob_WrongClassSkipped -v`

### Task 4: LLM integration test
**File:** `backend/integration_test.go`
- Add `TestIntegration_ExtractionClassMatchesRoster` (gated behind `OPENAI_API_KEY`). Provide a 2-class roster, a transcript mentioning a student, and assert that returned `Class` values are all in the roster set.
- Run: `go test -run TestIntegration_ExtractionClassMatchesRoster -v` (skipped in CI without key)

## Open Questions
None — the ErrNotFound path in `processVoiceNote` already logs and skips, so no pipeline code changes needed.
