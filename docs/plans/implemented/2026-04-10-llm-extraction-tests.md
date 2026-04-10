# LLM Extraction Integration Tests

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Replace the brittle prompt unit test with behavior-based LLM integration tests that verify extraction actually works correctly.

**Architecture:** Delete `TestBuildExtractionPrompt_ContainsClassConstraint` from `backend/extract_test.go`. Add 4 LLM integration test scenarios to `backend/integration_test.go`, all gated behind `OPENAI_API_KEY`. Each test calls the real `gptExtractor.Extract()` with sample text and asserts on output behavior.

**Tech Stack:** Go test, OpenAI API

---

### Task 1: Delete the brittle prompt unit test

**Files:**
- Modify: `backend/extract_test.go`

**Step 1: Delete the entire file**

`backend/extract_test.go` contains only `TestBuildExtractionPrompt_ContainsClassConstraint`. Delete the whole file.

```bash
rm backend/extract_test.go
```

**Step 2: Run tests to confirm nothing breaks**

```bash
cd backend && go test ./... -count=1 -short 2>&1 | tail -5
```

Expected: All tests pass.

**Step 3: Commit**

```bash
git add -A && git commit -m "test: remove brittle prompt unit test"
```

---

### Task 2: Add LLM integration tests for extraction

**Files:**
- Modify: `backend/integration_test.go` — add 4 new test functions

Add these tests at the end of `backend/integration_test.go`. All share a helper to create the extractor and skip if no API key. Replace the existing `TestIntegration_ExtractionClassMatchesRoster` with these more comprehensive tests.

**Step 1: Remove old test and add new tests**

Delete `TestIntegration_ExtractionClassMatchesRoster` from `backend/integration_test.go` and add the following 4 tests:

```go
// llmExtractor creates a gptExtractor, skipping if OPENAI_API_KEY is not set.
func llmExtractor(t *testing.T) Extractor {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set, skipping LLM integration test")
	}
	e, err := newGPTExtractor()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestLLM_SingleStudentCorrectClass(t *testing.T) {
	ext := llmExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Charlie Brown"}, {Name: "Diana Lee"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Alice Johnson demonstrated excellent problem-solving skills on today's algebra quiz. She scored 95% and helped her classmates understand the quadratic formula.",
		Classes:    classes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Students) != 1 {
		t.Fatalf("expected 1 student, got %d: %+v", len(result.Students), result.Students)
	}
	if result.Students[0].Name != "Alice Johnson" {
		t.Errorf("name = %q, want Alice Johnson", result.Students[0].Name)
	}
	if result.Students[0].Class != "Math 101" {
		t.Errorf("class = %q, want Math 101", result.Students[0].Class)
	}
}

func TestLLM_MultiStudentDifferentClasses(t *testing.T) {
	ext := llmExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Charlie Brown"}, {Name: "Diana Lee"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Today I observed two students. Bob Smith was very engaged during the fractions lesson and volunteered to solve problems on the board. In science class, Diana Lee conducted her chemistry experiment carefully and wrote detailed lab notes.",
		Classes:    classes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Students) < 2 {
		t.Fatalf("expected at least 2 students, got %d: %+v", len(result.Students), result.Students)
	}

	found := map[string]string{}
	for _, s := range result.Students {
		found[s.Name] = s.Class
	}
	if found["Bob Smith"] != "Math 101" {
		t.Errorf("Bob Smith class = %q, want Math 101", found["Bob Smith"])
	}
	if found["Diana Lee"] != "Science 202" {
		t.Errorf("Diana Lee class = %q, want Science 202", found["Diana Lee"])
	}
}

func TestLLM_UnknownClassSkipped(t *testing.T) {
	ext := llmExtractor(t)
	classes := []ClassGroup{
		{Name: "Math 101", Students: []ClassStudent{{Name: "Alice Johnson"}, {Name: "Bob Smith"}}},
		{Name: "Science 202", Students: []ClassStudent{{Name: "Charlie Brown"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Report card for Tommy Wilson, Art 303. Tommy shows great creativity in his paintings and participates actively in class discussions about art history.",
		Classes:    classes,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Tommy Wilson is not in any roster class. The extractor should return no students
	// (or possibly empty results). It must NOT invent a class name.
	validClasses := map[string]bool{"Math 101": true, "Science 202": true}
	for _, s := range result.Students {
		if !validClasses[s.Class] {
			t.Errorf("student %q assigned to invalid class %q", s.Name, s.Class)
		}
	}
}

func TestLLM_PartialNameMatch(t *testing.T) {
	ext := llmExtractor(t)
	classes := []ClassGroup{
		{Name: "English 101", Students: []ClassStudent{{Name: "Alexander Hamilton"}, {Name: "Elizabeth Bennet"}}},
		{Name: "History 201", Students: []ClassStudent{{Name: "Theodore Roosevelt"}}},
	}

	result, err := ext.Extract(t.Context(), ExtractRequest{
		Transcript: "Alex Hamilton wrote an outstanding essay on democracy today. His arguments were well-structured and his writing has improved significantly this semester.",
		Classes:    classes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Students) != 1 {
		t.Fatalf("expected 1 student, got %d: %+v", len(result.Students), result.Students)
	}
	if result.Students[0].Name != "Alexander Hamilton" {
		t.Errorf("name = %q, want Alexander Hamilton", result.Students[0].Name)
	}
	if result.Students[0].Class != "English 101" {
		t.Errorf("class = %q, want English 101", result.Students[0].Class)
	}
}
```

**Step 2: Run the new tests to verify they pass**

```bash
cd backend && go test ./... -run "TestLLM_" -v -count=1 2>&1
```

Expected: All 4 tests pass (or skip if no API key).

**Step 3: Run all tests to verify nothing is broken**

```bash
cd backend && go test ./... -count=1 -short 2>&1 | tail -5
```

**Step 4: Lint**

```bash
cd backend && make lint
```

**Step 5: Commit**

```bash
git add -A && git commit -m "test: add LLM integration tests for extraction behavior"
```
