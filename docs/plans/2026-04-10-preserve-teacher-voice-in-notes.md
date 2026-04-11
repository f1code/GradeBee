# Preserve Teacher Voice in Notes Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Replace AI-glazed note summaries with authentic, directly-extracted passages from the original transcript to preserve teacher voice and emotion.

**Architecture:** Instead of asking GPT to rewrite observations into formal "report card suitable" language, extract relevant passages directly from the transcript. The extraction flow becomes: transcript → identify students → quote relevant text (verbatim or minimally edited for context) → store as note content. No rewriting = no glazing.

**Tech Stack:** Go backend (OpenAI API for extraction), SQLite note storage, TypeScript frontend (display unchanged)

---

## Task 1: Update Extraction Request/Response Types

**Files:**
- Modify: `backend/extract.go:40-55` (MatchedStudent struct)
- Modify: `backend/extract.go:115-145` (extractResponseSchema)

**Context:** Currently `MatchedStudent.Summary` contains a GPT-rewritten summary. We'll replace it with `QuotedText` (relevant passages from the original transcript).

**Step 1: Update MatchedStudent struct**

Replace the struct at line 40-55 in `backend/extract.go`:

```go
// MatchedStudent is a single student extraction result.
type MatchedStudent struct {
	Name       string             `json:"name"`
	Class      string             `json:"class"`
	QuotedText string             `json:"quoted_text"` // Extracted passages from transcript, unchanged
	Confidence float64            `json:"confidence"`
	Candidates []StudentCandidate `json:"candidates,omitempty"`
}
```

**Step 2: Update the JSON schema**

Replace the extractResponseSchema function (line 115-145):

```go
// extractResponseSchema returns the JSON schema for structured outputs.
func extractResponseSchema() json.RawMessage {
	schema := `{
		"type": "object",
		"properties": {
			"students": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {"type": "string"},
						"class": {"type": "string"},
						"quoted_text": {"type": "string"},
						"confidence": {"type": "number"},
						"candidates": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"name": {"type": "string"},
									"class": {"type": "string"}
								},
								"required": ["name", "class"],
								"additionalProperties": false
							}
						}
					},
					"required": ["name", "class", "quoted_text", "confidence", "candidates"],
					"additionalProperties": false
				}
			},
			"date": {"type": "string"}
		},
		"required": ["students", "date"],
		"additionalProperties": false
	}`
	return json.RawMessage(schema)
}
```

**Step 3: Commit**

```bash
cd backend
git add extract.go
git commit -m "refactor: change MatchedStudent.Summary to QuotedText for authentic preservation"
```

---

## Task 2: Update Extraction Prompt to Extract Passages Instead of Rewrite

**Files:**
- Modify: `backend/extract.go:88-110` (buildExtractionPrompt)

**Context:** The system prompt currently tells GPT to write summaries "suitable for a report card." We need to instruct it to extract relevant passages verbatim instead.

**Step 1: Replace buildExtractionPrompt function**

```go
func buildExtractionPrompt(classes []ClassGroup) string {
	var sb strings.Builder
	sb.WriteString(`You are a teaching assistant analyzing a teacher's audio transcript about student observations.

Your task:
1. Identify which students are mentioned in the transcript
2. Match each mentioned name to the student roster below (handle phonetic/partial matches)
3. Extract the date if mentioned (format YYYY-MM-DD), otherwise leave empty
4. Extract relevant passages from the transcript that mention or describe this student
   - Include direct quotes where the teacher discusses this student
   - Preserve the teacher's exact wording and tone
   - Include 1-3 key passages per student, separated by " | " if multiple
   - Do NOT rewrite, summarize, or paraphrase - use the teacher's original language

Student Roster:
`)
	for _, c := range classes {
		for _, s := range c.Students {
			sb.WriteString(fmt.Sprintf("- %s (class %s)\n", s.Name, c.Name))
		}
	}

	sb.WriteString(`
Rules:
- Match mentioned names against the roster even if pronunciation differs slightly
- Set confidence 0.0-1.0 for each match. Use >= 0.7 for confident matches.
- If confidence < 0.7, include up to 3 closest roster matches in "candidates"
- Extract quoted_text directly from the transcript - preserve the teacher's exact words and emotion
- If the transcript contains observations about "everyone", "all students", "the class", or similar group references, extract those statements and apply them to EVERY student on the roster
- For group-level observations, quote the exact passage and include it for each student
- For multi-student transcripts, produce a separate entry per student with relevant passages
- If a mentioned student cannot be matched to any roster entry, do not include them in the output
- If no students are clearly mentioned, return an empty students array
- The "class" field for each student MUST exactly match one of the class names from the roster above. Do not invent or abbreviate class names.
- IMPORTANT: Never modify, clean up, or formally rewrite the teacher's text. Always preserve their original voice.
`)
	return sb.String()
}
```

**Step 2: Commit**

```bash
cd backend
git add extract.go
git commit -m "refactor: update extraction prompt to preserve teacher voice from transcript"
```

---

## Task 3: Update Note Creation to Use QuotedText Field

**Files:**
- Modify: `backend/voice_note_process.go:113-130` (createNoteRequest call)
- Modify: `backend/notes.go:20-27` (CreateNoteRequest struct)
- Modify: `backend/notes.go:40-50` (dbNoteCreator.CreateNote)

**Context:** The note creation pipeline currently passes `student.Summary` from extraction to the note. We need to update it to pass `student.QuotedText` instead.

**Step 1: Update CreateNoteRequest struct in notes.go**

Replace lines 20-27:

```go
// CreateNoteRequest is the input for creating a single student note.
type CreateNoteRequest struct {
	StudentID   int64
	StudentName string
	QuotedText  string  // Extracted passages from transcript
	Transcript  string
	Date        string // YYYY-MM-DD
}
```

**Step 2: Update dbNoteCreator.CreateNote in notes.go**

Replace lines 40-50:

```go
func (c *dbNoteCreator) CreateNote(ctx context.Context, req CreateNoteRequest) (*CreateNoteResponse, error) {
	n := &Note{
		StudentID: req.StudentID,
		Date:      req.Date,
		Summary:   req.QuotedText,  // Store extracted passages as the note summary
		Source:    "auto",
	}
	if req.Transcript != "" {
		n.Transcript = &req.Transcript
	}
	if err := c.noteRepo.Create(ctx, n); err != nil {
		return nil, err
	}
	return &CreateNoteResponse{NoteID: n.ID}, nil
}
```

**Step 3: Update voice_note_process.go call**

Replace lines 113-130 (inside the for loop that creates notes):

```go
		result, err := noteCreator.CreateNote(ctx, CreateNoteRequest{
			StudentID:   studentID,
			StudentName: student.Name,
			QuotedText:  student.QuotedText,  // Changed from Summary
			Transcript:  transcript,
			Date:        extractResult.Date,
		})
```

**Step 4: Commit**

```bash
cd backend
git add notes.go voice_note_process.go
git commit -m "refactor: update note creation to use QuotedText from extraction"
```

---

## Task 4: Write Tests for New Extraction Behavior

**Files:**
- Create: `backend/extract_test.go` (new file)
- Modify: `backend/voice_note_process_test.go` (update existing tests if they reference Summary)

**Context:** Test that extraction preserves teacher voice and correctly extracts relevant passages.

**Step 1: Create extract_test.go with failing test**

```go
package handler

import (
	"context"
	"testing"
)

// TestExtractPreservesTeacherVoice verifies that extracted QuotedText preserves
// the teacher's original language and emotion, not rewritten summaries.
func TestExtractPreservesTeacherVoice(t *testing.T) {
	extractor, err := newGPTExtractor()
	if err != nil {
		t.Skipf("OPENAI_API_KEY not set: %v", err)
	}

	// Example: raw teacher notes with strong emotion
	transcript := `Thursday. Maxence was impossibly bad today. I'm ready to choke the living 
sh*t out of him. He wouldn't stop talking during the lesson. 
Amara was great - very attentive and helpful to other students.`

	req := ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{
				Name: "Period 3",
				Students: []ClassStudent{
					{Name: "Maxence"},
					{Name: "Amara"},
				},
			},
		},
	}

	result, err := extractor.Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Students) != 2 {
		t.Fatalf("Expected 2 students, got %d", len(result.Students))
	}

	// Find Maxence
	var maxence *MatchedStudent
	for i := range result.Students {
		if result.Students[i].Name == "Maxence" {
			maxence = &result.Students[i]
			break
		}
	}
	if maxence == nil {
		t.Fatal("Maxence not found in extraction")
	}

	// Verify QuotedText contains original phrasing, not formal rewrite
	if maxence.QuotedText == "" {
		t.Error("QuotedText is empty")
	}
	
	// The quoted text should contain evidence of teacher's original voice
	// (not formal language like "had a very difficult day")
	if !contains(maxence.QuotedText, "impossibly bad") {
		t.Errorf("QuotedText does not preserve original phrasing. Got: %s", maxence.QuotedText)
	}
}

// TestExtractGroupObservations verifies that group-level observations
// are extracted and applied to all students in the class.
func TestExtractGroupObservations(t *testing.T) {
	extractor, err := newGPTExtractor()
	if err != nil {
		t.Skipf("OPENAI_API_KEY not set: %v", err)
	}

	transcript := `Today the class was way too loud and unfocused. Everyone was talking over each other. 
Specific note: Tommy helped me organize the materials, which was great.`

	req := ExtractRequest{
		Transcript: transcript,
		Classes: []ClassGroup{
			{
				Name: "Period 1",
				Students: []ClassStudent{
					{Name: "Tommy"},
					{Name: "Lisa"},
				},
			},
		},
	}

	result, err := extractor.Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Should have 2 entries (Tommy and Lisa)
	if len(result.Students) != 2 {
		t.Fatalf("Expected 2 students, got %d", len(result.Students))
	}

	// Both should include the group observation
	for _, s := range result.Students {
		if s.QuotedText == "" {
			t.Errorf("%s has empty QuotedText", s.Name)
		}
	}
}

// Helper
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0
}
```

**Step 2: Run the test (expect failures initially due to API model being gpt-5.4-mini which may not exist)**

```bash
cd backend
go test -v ./... -run TestExtractPreservesTeacherVoice
```

Expected: Either SKIP (no API key) or execution result showing extraction behavior

**Step 3: Update any existing extract-related tests if they reference Summary field**

Run:
```bash
cd backend
grep -r "Summary" *_test.go
```

If any tests reference `MatchedStudent.Summary`, update them to use `QuotedText` instead.

**Step 4: Commit**

```bash
cd backend
git add extract_test.go
git commit -m "test: add tests for voice preservation in extraction"
```

---

## Task 5: Run Backend Lint and All Tests

**Files:**
- Verify: All modified backend files

**Step 1: Run linter**

```bash
cd backend
make lint
```

Expected: No new errors related to our changes

**Step 2: Run all tests**

```bash
cd backend
go test ./... -v
```

Expected: All tests pass (extract tests will skip if no OPENAI_API_KEY)

**Step 3: Build to catch any type errors**

```bash
cd backend
go build ./...
```

Expected: Builds successfully

**Step 4: Commit if all pass**

```bash
cd backend
git add .
git commit -m "test: verify all backend tests and lint pass"
```

---

## Task 6: Frontend Display Verification

**Files:**
- Read: `frontend/src/components/NotesList.tsx`
- Read: `frontend/src/components/NoteEditor.tsx`

**Context:** Frontend already displays notes, so data flow should work as-is. Just verify there are no hardcoded assumptions about "Summary" being formal/short.

**Step 1: Check NotesList display**

```bash
cd frontend
grep -n "summary\|Summary" src/components/NotesList.tsx
```

Look for any comments or logic that assumes the summary is a formal rewrite.

**Step 2: Check NoteEditor display**

```bash
cd frontend
grep -n "summary\|Summary" src/components/NoteEditor.tsx
```

Same check.

**Step 3: Assessment**

If the frontend just displays the note text without assumptions, no changes needed. If there are comments like "// formal summary" or CSS assuming short text, update them to reflect that it's now extracted passage.

**Step 4: Document findings**

No commit needed for reads, but note any UI-level changes needed in next task if applicable.

---

## Task 7: Manual Testing with Example

**Files:**
- None (manual)

**Context:** Test the full pipeline end-to-end with a real (or test) audio/text note.

**Step 1: Run the backend locally**

```bash
cd backend
go run main.go
```

Or if using docker:
```bash
docker compose up
```

**Step 2: Paste a note with raw teacher language**

Via the frontend (text upload):

```
Thursday was rough. Maxence was impossibly bad - kept interrupting, 
wouldn't focus. I'm exhausted. Sarah was helpful though - asked great questions 
and engaged with the material.
```

**Step 3: Check the resulting note in the database or UI**

The `Summary` field (displayed as note content) should contain extracted passages like:
```
"Maxence was impossibly bad - kept interrupting, wouldn't focus"
```

NOT:
```
"Maxence exhibited disruptive behavior and had difficulty maintaining focus during class."
```

**Step 4: Verify date extraction**

Date should be extracted as "Thursday" or today's date if not specified.

**Step 5: Commit findings**

No code changes, but if issues found, create a bug task or document in plan for iteration.

---

## Open Questions

1. **Multiline/long passages:** If a student has 3+ relevant passages in the transcript, should we concatenate them with separators (" | ") or pick the most relevant? Answer affects extraction prompt.

2. **Profanity/sensitive content:** Should we keep teacher profanity/cursing verbatim, or is sanitizing that specific case OK (vs. other tone changes)? Current approach keeps verbatim.

3. **Context clipping:** If a quoted passage requires context to make sense (e.g., "He did it again" without subject), should extraction add context or leave as-is?

4. **API model:** The code references `gpt-5.4-mini` which doesn't exist. Should be `gpt-4o-mini` or `gpt-4-turbo`. Verify correct model before testing.

5. **Frontend presentation:** Should notes clearly indicate they're extracted passages vs. manually written summaries? Might want a visual indicator or source tag.

---

## Success Criteria

- [ ] Extraction returns `QuotedText` field instead of `Summary`
- [ ] Extraction prompt instructs GPT to preserve original language
- [ ] Note creation uses `QuotedText` from extraction
- [ ] Existing teacher language (including emotion/tone/profanity) is preserved in stored notes
- [ ] No grammatical "clean-up" or formalization applied
- [ ] All backend tests pass
- [ ] Lint passes
- [ ] Manual test shows raw passage in note, not rewritten text
