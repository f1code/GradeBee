# Stricter Class Matching in Extraction Prompt

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Prevent GPT from assigning students to the wrong class by tightening the extraction prompt to require explicit class references instead of fuzzy/partial matching.

**Architecture:** Modify the system prompt in `buildExtractionPrompt` to instruct GPT to only assign a class when the teacher clearly references it. Add a unit test for prompt content. No changes to the processing pipeline or schema.

**Tech Stack:** Go, OpenAI structured outputs

**Problem:** When a teacher says "Thursday Linda" (not a real class), GPT fuzzy-matches to "Thursday Mousy" because they share "Thursday". The extraction assigns the student to the wrong class.

---

### Task 1: Add test for extraction prompt class-matching rules

**Files:**
- Create: `backend/extract_test.go`

**Step 1: Write the test**

```go
package handler

import (
	"strings"
	"testing"
)

func TestBuildExtractionPrompt_ContainsStrictClassRule(t *testing.T) {
	classes := []ClassGroup{
		{Name: "Thursday Mousy", Students: []ClassStudent{{Name: "Alice"}}},
		{Name: "Friday Stars", Students: []ClassStudent{{Name: "Bob"}}},
	}
	prompt := buildExtractionPrompt(classes)

	// Verify roster is included.
	if !strings.Contains(prompt, "Thursday Mousy") {
		t.Error("prompt should contain class name 'Thursday Mousy'")
	}
	if !strings.Contains(prompt, "Friday Stars") {
		t.Error("prompt should contain class name 'Friday Stars'")
	}

	// Verify strict class matching rule is present.
	if !strings.Contains(prompt, "exact class name") {
		t.Error("prompt should contain strict class matching instruction")
	}
	if !strings.Contains(prompt, "Do not infer") {
		t.Error("prompt should instruct not to infer class from partial matches")
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `cd backend && go test -run TestBuildExtractionPrompt_ContainsStrictClassRule -v`
Expected: FAIL — prompt doesn't contain "exact class name" or "Do not infer" yet.

**Step 3: Commit**

```bash
cd backend && git add extract_test.go && git commit -m "test: add extraction prompt strict class matching test"
```

---

### Task 2: Tighten the extraction prompt

**Files:**
- Modify: `backend/extract.go` — the `buildExtractionPrompt` function, specifically the `Rules:` section

**Step 1: Update the prompt rules**

In `backend/extract.go`, replace the current Rules section in `buildExtractionPrompt` (starting at `sb.WriteString("\nRules:\n...")`) with:

```go
	sb.WriteString(`
Rules:
- The "class" field MUST be the exact class name from the roster above, copied verbatim
- Only assign a student to a class if the teacher clearly and explicitly references that class by name
- Do not infer or guess the class from partial word matches (e.g. if the teacher says "Thursday Linda" and the roster has "Thursday Mousy", do NOT match — these are different classes)
- If the teacher does not clearly mention a class name, but a student name uniquely matches exactly one roster entry, use that student's class
- If a student name appears in multiple classes and the class is not clearly mentioned, do not include that student (set confidence below 0.5)
- Match mentioned names against the roster even if pronunciation differs slightly
- Set confidence 0.0-1.0 for each match. Use >= 0.7 for confident matches.
- If confidence < 0.7, include up to 3 closest roster matches in "candidates"
- A transcript may contain observations about multiple classes — treat each class section independently
- If the transcript contains observations about "everyone", "all students", "the class", or similar group references, apply those observations only to students in the most recently mentioned class in the transcript
- If a group reference appears before any class has been mentioned, skip it
- Combine any group-level observations with student-specific observations in each student's summary
- For multi-student transcripts, produce a separate summary per student
- Each summary should be from the teacher's perspective, about that specific student
- If a mentioned student cannot be matched to any roster entry, do not include them in the output
- If no students are clearly mentioned, return an empty students array
`)
```

**Step 2: Run the test to verify it passes**

Run: `cd backend && go test -run TestBuildExtractionPrompt_ContainsStrictClassRule -v`
Expected: PASS

**Step 3: Run all tests to check for regressions**

Run: `cd backend && go test ./... -count=1`
Expected: All pass.

**Step 4: Run lint**

Run: `cd backend && make lint`
Expected: Clean.

**Step 5: Commit**

```bash
cd backend && git add extract.go && git commit -m "fix: tighten extraction prompt to prevent wrong class assignment

GPT was fuzzy-matching class names (e.g. 'Thursday Linda' -> 'Thursday Mousy')
when they shared partial words. Now the prompt requires explicit class references
and forbids inferring class from partial matches."
```

---

### Task 3: Add test for wrong-class scenario in process pipeline

This verifies that if extraction returns a class that doesn't exist in DB, the student is skipped (existing behavior, but worth having an explicit test).

**Files:**
- Modify: `backend/voice_note_process_test.go`

**Step 1: Write the test**

Add to `voice_note_process_test.go`:

```go
func TestProcessJob_WrongClassSkipped(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	// Create "Thursday Mousy" class with Alice.
	cls, err := classRepo.Create(t.Context(), "u1", "Thursday Mousy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := studentRepo.Create(t.Context(), cls.ID, "Alice"); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test.m4a")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "Thursday Linda Alice did well"},
		roster: &stubRoster{
			classNames: []string{"Thursday Mousy"},
			students:   []ClassGroup{{Name: "Thursday Mousy", Students: []ClassStudent{{Name: "Alice"}}}},
		},
		// Simulate GPT incorrectly matching to a non-existent class.
		extractor: &stubExtractor{result: &ExtractResponse{
			Date: "2026-01-01",
			Students: []MatchedStudent{
				{Name: "Alice", Class: "Thursday Linda", Summary: "Did well", Confidence: 0.8},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	ctx := context.Background()
	if err := queue.Publish(ctx, VoiceNoteJob{
		UserID: "u1", UploadID: 1, FilePath: audioPath,
		Status: JobStatusQueued, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := processVoiceNote(ctx, d, queue, voiceNoteKey("u1", 1)); err != nil {
		t.Fatal(err)
	}

	// Student should be skipped because "Thursday Linda" is not a real class.
	if len(nc.calls) != 0 {
		t.Errorf("note creator calls = %d, want 0 (wrong class should be skipped)", len(nc.calls))
	}

	got, err := queue.GetJob(ctx, voiceNoteKey("u1", 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusDone {
		t.Errorf("status = %q, want %q", got.Status, JobStatusDone)
	}
}
```

**Step 2: Run the test**

Run: `cd backend && go test -run TestProcessJob_WrongClassSkipped -v`
Expected: PASS — `FindByNameAndClass` already returns `ErrNotFound` for non-existent class, and the pipeline skips with a warning.

**Step 3: Commit**

```bash
cd backend && git add voice_note_process_test.go && git commit -m "test: verify wrong class extraction is skipped in pipeline"
```

---

### Task 4: Add LLM integration test for class matching quality

This test calls the real OpenAI API to verify the prompt produces correct class assignments. Skipped when `OPENAI_API_KEY` is not set.

**Files:**
- Create: `backend/extract_integration_test.go`

**Step 1: Write the test**

```go
package handler

import (
	"context"
	"os"
	"testing"
)

func TestExtract_LLM_ClassMatchingQuality(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set, skipping LLM integration test")
	}

	extractor, err := newGPTExtractor()
	if err != nil {
		t.Fatal(err)
	}

	classes := []ClassGroup{
		{Name: "Thursday Mousy", Students: []ClassStudent{{Name: "Alice"}, {Name: "Luis"}}},
		{Name: "Thursday Linda", Students: []ClassStudent{{Name: "Bob"}, {Name: "Louise"}}},
		{Name: "Friday Stars", Students: []ClassStudent{{Name: "Charlie"}}},
	}

	tests := []struct {
		name       string
		transcript string
		// wantStudents maps expected student name -> expected class
		wantStudents map[string]string
		// wantAbsent are student names that should NOT appear in results
		wantAbsent []string
	}{
		{
			name:       "correct class assignment",
			transcript: "Thursday Mousy class: Alice did great today, she really improved her reading.",
			wantStudents: map[string]string{"Alice": "Thursday Mousy"},
			wantAbsent:   []string{"Bob", "Charlie", "Luis", "Louise"},
		},
		{
			name:       "no cross-class contamination",
			transcript: "Thursday Linda class: Bob was very engaged in the lesson today.",
			wantStudents: map[string]string{"Bob": "Thursday Linda"},
			wantAbsent:   []string{"Alice", "Charlie"},
		},
		{
			name:       "similar names in different classes",
			transcript: "Thursday Mousy: Luis has been doing well. Thursday Linda: Louise needs extra help.",
			wantStudents: map[string]string{"Luis": "Thursday Mousy", "Louise": "Thursday Linda"},
		},
		{
			name:       "everyone applies to mentioned class only",
			transcript: "Friday Stars: everyone did an amazing job on the project today.",
			wantStudents: map[string]string{"Charlie": "Friday Stars"},
			wantAbsent:   []string{"Alice", "Bob", "Luis", "Louise"},
		},
		{
			name:       "unique student without class mention",
			transcript: "Charlie was excellent in art class today.",
			wantStudents: map[string]string{"Charlie": "Friday Stars"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := extractor.Extract(context.Background(), ExtractRequest{
				Transcript: tt.transcript,
				Classes:    classes,
			})
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}

			// Build result map.
			got := make(map[string]string)
			for _, s := range resp.Students {
				if s.Confidence >= 0.5 {
					got[s.Name] = s.Class
				}
			}

			// Check expected students are present with correct class.
			for wantName, wantClass := range tt.wantStudents {
				gotClass, ok := got[wantName]
				if !ok {
					t.Errorf("expected student %q in results, got: %v", wantName, got)
					continue
				}
				if gotClass != wantClass {
					t.Errorf("student %q: got class %q, want %q", wantName, gotClass, wantClass)
				}
			}

			// Check absent students are not present.
			for _, name := range tt.wantAbsent {
				if _, ok := got[name]; ok {
					t.Errorf("student %q should not be in results, got class %q", name, got[name])
				}
			}
		})
	}
}
```

**Step 2: Run the test (requires OPENAI_API_KEY)**

Run: `cd backend && go test -run TestExtract_LLM_ClassMatchingQuality -v -timeout 60s`
Expected: All subtests PASS. Without API key, test is skipped.

**Step 3: Commit**

```bash
cd backend && git add extract_integration_test.go && git commit -m "test: add LLM integration test for class matching quality"
```

---

### Open Questions

1. **Monitoring** — should we add a metric/log for how often extracted class names don't match DB classes? Would help us know if the prompt fix is working or if we need the LLM verification fallback.
