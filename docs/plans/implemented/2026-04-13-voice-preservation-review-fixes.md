# Voice Preservation Review Fixes

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Fix the broken `contains` helper in extract tests and implement 4 review suggestions: add a unit test with mock extractor, document the Summary→QuotedText semantic shift, strengthen group observation test assertions, and verify frontend compatibility.

**Architecture:** Small targeted fixes across backend tests, architecture docs, and frontend verification. No structural changes.

**Tech Stack:** Go backend tests, Markdown docs, TypeScript frontend (read-only verification)

---

## Task 1: Fix Broken `contains` Helper

**Files:**
- Modify: `backend/extract_test.go:110-112`

**Context:** The `contains` helper always returns `true` for non-empty strings. It never actually checks substring presence, making the "impossibly bad" assertion meaningless.

**Step 1: Fix the helper**

Replace the helper at line 110-112 in `backend/extract_test.go`:

```go
// Helper
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
```

Also add `"strings"` to the import block at the top of the file (line 4):

```go
import (
	"context"
	"strings"
	"testing"
)
```

**Step 2: Run tests to verify it compiles**

```bash
cd backend && go build ./...
```

Expected: Compiles successfully.

**Step 3: Commit**

```bash
cd backend && git add extract_test.go
git commit -m "fix: use strings.Contains in extract_test helper"
```

---

## Task 2: Add Unit Test With Mock Extractor for QuotedText Pipeline

**Files:**
- Modify: `backend/voice_note_process_test.go`

**Context:** All current extraction tests require `OPENAI_API_KEY` and skip in CI. We need a unit test using `stubExtractor` and `stubNoteCreator` (from `testutil_test.go`) that verifies `QuotedText` flows from extraction result through to the `CreateNoteRequest`. The existing `TestProcessJob_HappyPath` test already sets up the full pipeline — we add a new test specifically asserting the `QuotedText` field is passed through.

**Step 1: Add test to voice_note_process_test.go**

Add after the last test function in the file:

```go
// TestProcessJob_QuotedTextPassedToNoteCreator verifies that QuotedText from
// extraction flows through to CreateNoteRequest without modification.
func TestProcessJob_QuotedTextPassedToNoteCreator(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls, err := classRepo.Create(t.Context(), "user1", "Math")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	if err := os.WriteFile(audioPath, []byte("fake audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{results: []*CreateNoteResponse{{NoteID: 1}}}

	rawQuote := "Alice was impossibly good today - she blew my mind with her presentation"

	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "some transcript"},
		roster: &stubRoster{
			classNames: []string{"Math"},
			students:   []ClassGroup{{Name: "Math", Students: []ClassStudent{{Name: "Alice"}}}},
		},
		extractor: &stubExtractor{result: &ExtractResponse{
			Date: "2026-04-13",
			Students: []MatchedStudent{
				{Name: "Alice", Class: "Math", QuotedText: rawQuote, Confidence: 0.95},
			},
		}},
		noteCreator:    nc,
		voiceNoteQueue: queue,
		studentRepo:    studentRepo,
		classRepo:      classRepo,
		voiceNoteRepo:  voiceNoteRepo,
	}

	vn, err := voiceNoteRepo.Create(t.Context(), "user1", audioPath)
	if err != nil {
		t.Fatal(err)
	}
	queue.publish(VoiceNoteJob{VoiceNoteID: vn.ID, UserID: "user1"})

	processVoiceNote(t.Context(), d, queue, vn.Key)

	if len(nc.calls) != 1 {
		t.Fatalf("expected 1 note creation call, got %d", len(nc.calls))
	}
	if nc.calls[0].QuotedText != rawQuote {
		t.Errorf("QuotedText not passed through.\nGot:  %s\nWant: %s", nc.calls[0].QuotedText, rawQuote)
	}
}
```

**Step 2: Run the test**

```bash
cd backend && go test -v -run TestProcessJob_QuotedTextPassedToNoteCreator ./...
```

Expected: PASS

**Step 3: Commit**

```bash
cd backend && git add voice_note_process_test.go
git commit -m "test: add unit test verifying QuotedText flows through pipeline"
```

---

## Task 3: Strengthen Group Observation Test

**Files:**
- Modify: `backend/extract_test.go` (TestExtractGroupObservations function)

**Context:** The current test only checks `QuotedText != ""` for Lisa, who isn't individually mentioned. It should verify Lisa's text contains the group observation phrasing.

**Step 1: Update TestExtractGroupObservations assertions**

In `backend/extract_test.go`, replace the final assertion loop (around line 100-105):

```go
	// Both should include the group observation
	for _, s := range result.Students {
		if s.QuotedText == "" {
			t.Errorf("%s has empty QuotedText", s.Name)
		}
		// Group observation should be reflected for all students
		if !contains(s.QuotedText, "too loud") && !contains(s.QuotedText, "unfocused") && !contains(s.QuotedText, "talking over") {
			t.Errorf("%s QuotedText missing group observation. Got: %s", s.Name, s.QuotedText)
		}
	}
```

**Step 2: Verify it compiles**

```bash
cd backend && go build ./...
```

Expected: Compiles (test itself will skip without API key).

**Step 3: Commit**

```bash
cd backend && git add extract_test.go
git commit -m "test: strengthen group observation assertions in extract test"
```

---

## Task 4: Document Summary→QuotedText Semantic Shift in ARCHITECTURE.md

**Files:**
- Modify: `backend/ARCHITECTURE.md:80`

**Context:** Line 80 still references `summary` in the pipeline flow description. The DB column is still called `Summary` but now stores extracted passages, not AI-rewritten summaries. This should be documented to avoid confusion.

**Step 1: Update the pipeline description**

In `backend/ARCHITECTURE.md`, find the line (around line 80):
```
        │    → per-student observations (name, class, summary, confidence)
```

Replace with:
```
        │    → per-student observations (name, class, quoted_text, confidence)
        │    Note: quoted_text contains verbatim passages from the transcript.
        │    Stored in the notes table `summary` column (legacy name, no migration needed).
```

**Step 2: Commit**

```bash
cd backend && git add ARCHITECTURE.md
git commit -m "docs: document QuotedText semantic shift in architecture"
```

---

## Task 5: Verify Frontend Compatibility

**Files:**
- Read: `frontend/src/api-types.gen.ts` (lines around 63 and 214)
- Read: `frontend/src/components/NotesList.tsx`
- Read: `frontend/src/components/NoteEditor.tsx`

**Context:** The generated types now have `quoted_text` on `MatchedStudent` (line 63) but notes still use `summary` (line 214). The frontend reads notes via the API, where the DB `summary` column is returned. Verify no frontend code directly accesses `MatchedStudent.quoted_text` — if it does, it should use the new field name.

**Step 1: Check if frontend uses MatchedStudent type directly**

```bash
cd frontend && grep -rn "MatchedStudent\|quoted_text" src/ --include="*.ts" --include="*.tsx" | grep -v "api-types.gen"
```

Expected: No results (frontend doesn't directly consume extraction results — the backend creates notes from them).

**Step 2: Verify notes display still works**

```bash
cd frontend && grep -n "\.summary" src/components/NotesList.tsx src/components/NoteEditor.tsx src/components/StudentDetail.tsx
```

Expected: References to `note.summary` — these are the Note type's `summary` field, which is still populated from the DB. No change needed.

**Step 3: Assessment and commit**

If no issues found, no code changes needed. Document the finding:

```bash
git commit --allow-empty -m "chore: verify frontend compatibility with QuotedText change (no changes needed)"
```

---

## Task 6: Run Full Lint and Test Suite

**Files:**
- All modified backend files

**Step 1: Run linter**

```bash
cd backend && make lint
```

Expected: No errors.

**Step 2: Run all tests**

```bash
cd backend && go test ./... -v
```

Expected: All tests pass (extract API tests skip without OPENAI_API_KEY).

**Step 3: Build**

```bash
cd backend && go build ./...
```

Expected: Clean build.

---

## Open Questions

None — all items are straightforward fixes.
