# Report Example Categories Implementation Plan

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Split class names into "Class" + "Group", add class-name tags to report examples, and filter examples by class during report generation.

**Architecture:** Add `class_name` and `group_name` columns to `classes` table (migrate existing data by splitting on first `" - "`). Add `report_example_classes` join table. Drop existing report examples. Update API contracts for class CRUD and example CRUD. Update frontend forms.

**Tech Stack:** Go (backend), SQLite (DB), React/TypeScript (frontend), tygo (type gen)

---

### Task 1: SQL Migration — classes split + example classes join table

**Files:**
- Create: `backend/sql/004_class_categories.sql`

**Step 1: Write the migration**

```sql
-- 004_class_categories.sql

-- Add class_name and group_name columns to classes table.
ALTER TABLE classes ADD COLUMN class_name TEXT NOT NULL DEFAULT '';
ALTER TABLE classes ADD COLUMN group_name TEXT NOT NULL DEFAULT '';

-- Populate from existing name: split on first " - ", trim both parts.
-- If no dash, class_name = name, group_name = ''.
UPDATE classes SET
  class_name = TRIM(CASE
    WHEN INSTR(name, ' - ') > 0 THEN SUBSTR(name, 1, INSTR(name, ' - ') - 1)
    ELSE name
  END),
  group_name = TRIM(CASE
    WHEN INSTR(name, ' - ') > 0 THEN SUBSTR(name, INSTR(name, ' - ') + 3)
    ELSE ''
  END);

-- Drop the unique constraint on (user_id, name) by recreating.
-- SQLite doesn't support DROP CONSTRAINT, so we need to keep the old column
-- but the unique index will be replaced.
-- Actually, the UNIQUE is part of the CREATE TABLE. We'll add a new unique
-- index on (user_id, class_name, group_name) and leave the old one
-- (it won't hurt, names are still unique).
CREATE UNIQUE INDEX IF NOT EXISTS idx_classes_user_class_group
  ON classes(user_id, class_name, group_name);

-- Create join table for report example <-> class name.
CREATE TABLE IF NOT EXISTS report_example_classes (
    example_id INTEGER NOT NULL REFERENCES report_examples(id) ON DELETE CASCADE,
    class_name TEXT NOT NULL,
    PRIMARY KEY (example_id, class_name)
);

-- Drop all existing report examples (users will re-upload with class tags).
DELETE FROM report_examples;
```

**Step 2: Verify migration runs**

Run: `cd backend && go test -run TestMigration -v ./... 2>&1 | head -20`

If no specific migration test, verify by running:
```bash
cd backend && go test -run TestSetupDB -v ./... 2>&1 | head -20
```

Or simply run all tests to make sure nothing breaks:
```bash
cd backend && make test
```

**Step 3: Commit**

```bash
git add backend/sql/004_class_categories.sql
git commit -m "feat: add migration for class_name/group_name split and example_classes join table"
```

---

### Task 2: Update Class struct and ClassRepo

**Files:**
- Modify: `backend/repo_class.go`

**Step 1: Write failing test**

Create `backend/repo_class_test.go`:

```go
package handler

import (
	"testing"
)

func TestClassRepo_CreateWithClassNameGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	c, err := repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	if err != nil {
		t.Fatal(err)
	}
	if c.ClassName != "Mousy" {
		t.Errorf("ClassName = %q, want Mousy", c.ClassName)
	}
	if c.GroupName != "Thursday" {
		t.Errorf("GroupName = %q, want Thursday", c.GroupName)
	}
	if c.Name != "Mousy Thursday" {
		t.Errorf("Name = %q, want 'Mousy Thursday'", c.Name)
	}
}

func TestClassRepo_CreateNoGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	c, err := repo.Create(t.Context(), "user1", "Mousy", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Mousy" {
		t.Errorf("Name = %q, want 'Mousy'", c.Name)
	}
}

func TestClassRepo_ListDistinctClassNames(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	repo.Create(t.Context(), "user1", "Mousy", "Wednesday")
	repo.Create(t.Context(), "user1", "Emma", "Monday")

	names, err := repo.ListDistinctClassNames(t.Context(), "user1")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
	// Should be sorted
	if names[0] != "Emma" || names[1] != "Mousy" {
		t.Errorf("names = %v, want [Emma Mousy]", names)
	}
}

func TestClassRepo_DuplicateClassGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	_, err := repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd backend && go test -run TestClassRepo_ -v
```

Expected: compilation errors (ClassName, GroupName fields don't exist yet).

**Step 3: Update Class struct and repo methods**

In `backend/repo_class.go`:

- Add `ClassName` and `GroupName` fields to `Class` struct.
- The `Name` field becomes computed: `ClassName + " " + GroupName` (trimmed).
- Update `Create` to accept `className, groupName` instead of `name`. Compute `name` from them.
- Update `List` to scan `class_name` and `group_name`.
- Update `Rename` → `Update` to accept `className, groupName`.
- Add `ListDistinctClassNames(ctx, userID) ([]string, error)`.
- Update `GetByID` to scan new columns.

The `Name` field should still be populated (computed as `className + " " + groupName`, trimmed) for backward compat — it's used in the roster, extractor, display, etc.

Updated `Class` struct:
```go
type Class struct {
	ID        int64  `json:"id"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	ClassName string `json:"className"`
	GroupName string `json:"groupName"`
	Position  int    `json:"position"`
	CreatedAt string `json:"createdAt"`
}
```

Updated `Create`:
```go
func (r *ClassRepo) Create(ctx context.Context, userID, className, groupName string) (Class, error) {
	name := className
	if groupName != "" {
		name = className + " " + groupName
	}
	var c Class
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO classes (user_id, name, class_name, group_name, position)
		VALUES (?, ?, ?, ?, COALESCE((SELECT MAX(position) FROM classes WHERE user_id = ?), 0) + 1)
		RETURNING id, user_id, name, class_name, group_name, position, created_at`,
		userID, name, className, groupName, userID,
	).Scan(&c.ID, &c.UserID, &c.Name, &c.ClassName, &c.GroupName, &c.Position, &c.CreatedAt)
	if err != nil {
		if isDuplicateErr(err) {
			return Class{}, fmt.Errorf("create class %q: %w", name, ErrDuplicate)
		}
		return Class{}, fmt.Errorf("create class: %w", err)
	}
	return c, nil
}
```

Updated `List` scan + query to include `class_name, group_name`.

Updated `Rename` → `Update`:
```go
func (r *ClassRepo) Update(ctx context.Context, userID string, id int64, className, groupName string) error {
	name := className
	if groupName != "" {
		name = className + " " + groupName
	}
	res, err := r.db.ExecContext(ctx,
		"UPDATE classes SET name = ?, class_name = ?, group_name = ? WHERE id = ? AND user_id = ?",
		name, className, groupName, id, userID)
	if err != nil {
		if isDuplicateErr(err) {
			return fmt.Errorf("update class: %w", ErrDuplicate)
		}
		return fmt.Errorf("update class: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
```

New method:
```go
func (r *ClassRepo) ListDistinctClassNames(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT DISTINCT class_name FROM classes WHERE user_id = ? ORDER BY class_name", userID)
	if err != nil {
		return nil, fmt.Errorf("list distinct class names: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan class name: %w", err)
		}
		names = append(names, n)
	}
	return names, rows.Err()
}
```

**Step 4: Run tests**

```bash
cd backend && go test -run TestClassRepo_ -v
```

Expected: PASS

**Step 5: Fix all compilation errors from `Rename` → `Update` and `Create` signature change**

Update callers:
- `backend/students.go` — `handleCreateClass`, `handleUpdateClass`: parse `className` + `group` from request body, call new signatures.
- Any other callers of `ClassRepo.Create` or `ClassRepo.Rename`.

Run:
```bash
cd backend && make lint
```

Fix until clean.

**Step 6: Run full test suite**

```bash
cd backend && make test
```

**Step 7: Commit**

```bash
git add backend/repo_class.go backend/repo_class_test.go backend/students.go
git commit -m "feat: split class into className + groupName with repo support"
```

---

### Task 3: Update class CRUD handlers + add class-names endpoint

**Files:**
- Modify: `backend/students.go`
- Modify: `backend/handler.go` (add route for GET /classes/class-names)

**Step 1: Write failing test for new endpoint**

Add to `backend/students_test.go`:

```go
func TestListClassNames(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}
	withDeps(t, &mockDepsAll{classRepo: repo, db: db})

	repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	repo.Create(t.Context(), "user1", "Mousy", "Wednesday")
	repo.Create(t.Context(), "user1", "Emma", "Monday")

	r := httptest.NewRequest(http.MethodGet, "/classes/class-names", nil)
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleListClassNames(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ClassNames []string `json:"classNames"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.ClassNames) != 2 {
		t.Fatalf("got %d names, want 2", len(resp.ClassNames))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd backend && go test -run TestListClassNames -v
```

**Step 3: Implement handler**

In `backend/students.go`, add `handleListClassNames`:
```go
func handleListClassNames(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	names, err := serviceDeps.GetClassRepo().ListDistinctClassNames(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if names == nil {
		names = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"classNames": names})
}
```

Update `handleCreateClass` to parse `className` + `group`:
```go
var req struct {
	ClassName string `json:"className"`
	Group     string `json:"group"`
}
// ... decode ...
if req.ClassName == "" {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "className is required"})
	return
}
c, err := serviceDeps.GetClassRepo().Create(r.Context(), userID, req.ClassName, req.Group)
```

Update `handleUpdateClass` similarly to parse `className` + `group` and call `Update`.

Add route in `backend/handler.go`:
```go
case path == "classes/class-names" && r.Method == http.MethodGet:
	authHandler(handleListClassNames).ServeHTTP(rec, r)
```

**Note:** This route must come BEFORE the `strings.HasPrefix(path, "classes/")` catch for `classes/{id}` routes. Check the routing order in `handler.go`.

**Step 4: Run tests**

```bash
cd backend && go test -run TestListClassNames -v
```

**Step 5: Update existing class CRUD tests**

Update any tests in `backend/students_test.go` that call `createClass` with `name` to use the new `className` + `group` format.

**Step 6: Run full test suite + lint**

```bash
cd backend && make test && make lint
```

**Step 7: Commit**

```bash
git add backend/students.go backend/students_test.go backend/handler.go
git commit -m "feat: update class CRUD for className/group and add class-names endpoint"
```

---

### Task 4: Update ReportExampleRepo + ExampleStore for class names

**Files:**
- Modify: `backend/repo_example.go`
- Modify: `backend/report_examples.go`

**Step 1: Write failing test**

Add `backend/repo_example_test.go`:

```go
package handler

import (
	"testing"
)

func TestReportExampleRepo_CreateWithClassNames(t *testing.T) {
	db := setupTestDB(t)
	repo := &ReportExampleRepo{db: db}

	ex, err := repo.Create(t.Context(), "user1", "Math Report", "content here")
	if err != nil {
		t.Fatal(err)
	}

	err = repo.SetClassNames(t.Context(), ex.ID, []string{"Mousy", "Emma"})
	if err != nil {
		t.Fatal(err)
	}

	names, err := repo.GetClassNames(t.Context(), ex.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
}

func TestReportExampleRepo_ListReadyByClassName(t *testing.T) {
	db := setupTestDB(t)
	repo := &ReportExampleRepo{db: db}

	ex1, _ := repo.Create(t.Context(), "user1", "Math Report", "math content")
	repo.SetClassNames(t.Context(), ex1.ID, []string{"Mousy"})

	ex2, _ := repo.Create(t.Context(), "user1", "Reading Report", "reading content")
	repo.SetClassNames(t.Context(), ex2.ID, []string{"Emma"})

	ex3, _ := repo.Create(t.Context(), "user1", "General Report", "general content")
	repo.SetClassNames(t.Context(), ex3.ID, []string{"Mousy", "Emma"})

	// Query for Mousy — should get ex1 and ex3
	examples, err := repo.ListReadyByClassName(t.Context(), "user1", "Mousy")
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) != 2 {
		t.Fatalf("got %d examples for Mousy, want 2", len(examples))
	}

	// Query for Emma — should get ex2 and ex3
	examples, err = repo.ListReadyByClassName(t.Context(), "user1", "Emma")
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) != 2 {
		t.Fatalf("got %d examples for Emma, want 2", len(examples))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd backend && go test -run TestReportExampleRepo_ -v
```

**Step 3: Implement**

Add to `backend/repo_example.go`:

```go
// SetClassNames replaces the class name associations for an example.
func (r *ReportExampleRepo) SetClassNames(ctx context.Context, exampleID int64, classNames []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set class names: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM report_example_classes WHERE example_id = ?", exampleID); err != nil {
		return fmt.Errorf("set class names: delete: %w", err)
	}
	for _, name := range classNames {
		if _, err := tx.ExecContext(ctx, "INSERT INTO report_example_classes (example_id, class_name) VALUES (?, ?)", exampleID, name); err != nil {
			return fmt.Errorf("set class names: insert %q: %w", name, err)
		}
	}
	return tx.Commit()
}

// GetClassNames returns the class names associated with an example.
func (r *ReportExampleRepo) GetClassNames(ctx context.Context, exampleID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT class_name FROM report_example_classes WHERE example_id = ? ORDER BY class_name", exampleID)
	if err != nil {
		return nil, fmt.Errorf("get class names: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan class name: %w", err)
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// ListReadyByClassName returns ready examples tagged with the given class name.
func (r *ReportExampleRepo) ListReadyByClassName(ctx context.Context, userID, className string) ([]DBReportExample, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.id, e.user_id, e.name, e.content, e.status, e.file_path, e.created_at
		FROM report_examples e
		JOIN report_example_classes ec ON ec.example_id = e.id
		WHERE e.user_id = ? AND e.status = 'ready' AND ec.class_name = ?
		ORDER BY e.created_at DESC`, userID, className)
	if err != nil {
		return nil, fmt.Errorf("list ready by class: %w", err)
	}
	defer rows.Close()
	var result []DBReportExample
	for rows.Next() {
		var e DBReportExample
		if err := rows.Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.Status, &e.FilePath, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
```

Update `ReportExample` struct in `backend/report_examples.go` to include `ClassNames`:
```go
type ReportExample struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Content    string   `json:"content"`
	Status     string   `json:"status"`
	ClassNames []string `json:"classNames"`
}
```

Update `ExampleStore` interface to add class name handling:
```go
type ExampleStore interface {
	ListExamples(ctx context.Context, userID string) ([]ReportExample, error)
	UploadExample(ctx context.Context, userID, name, content string, classNames []string) (*ReportExample, error)
	CreatePendingExample(ctx context.Context, userID, name, filePath string, classNames []string) (*ReportExample, error)
	UpdateExampleStatus(ctx context.Context, id int64, status, content string) error
	UpdateExample(ctx context.Context, userID string, id int64, name, content string, classNames []string) (*ReportExample, error)
	DeleteExample(ctx context.Context, userID string, id int64) error
}
```

Update `dbExampleStore` methods to:
- Call `SetClassNames` after create/update
- Call `GetClassNames` when listing (or use a join)
- Populate `ClassNames` field on returned `ReportExample`

**Step 4: Run tests**

```bash
cd backend && go test -run TestReportExampleRepo_ -v
```

**Step 5: Fix compilation errors in all ExampleStore callers**

Update `stubExampleStore` in `testutil_test.go` to match new interface.
Update handler code that calls ExampleStore methods.

```bash
cd backend && make lint
```

**Step 6: Run full test suite**

```bash
cd backend && make test
```

**Step 7: Commit**

```bash
git add backend/repo_example.go backend/repo_example_test.go backend/report_examples.go backend/testutil_test.go
git commit -m "feat: add class name associations to report examples"
```

---

### Task 5: Update report generation to filter examples by class name

**Files:**
- Modify: `backend/report_generator.go`

**Step 1: Write failing test**

Add `backend/report_generator_test.go`:

```go
package handler

import (
	"context"
	"testing"
)

func TestLoadExamples_FiltersByClassName(t *testing.T) {
	db := setupTestDB(t)
	exRepo := &ReportExampleRepo{db: db}
	noteRepo := &NoteRepo{db: db}
	reportRepo := &ReportRepo{db: db}

	// Create examples with class name tags
	ex1, _ := exRepo.Create(t.Context(), "user1", "Mousy Report", "mousy style")
	exRepo.SetClassNames(t.Context(), ex1.ID, []string{"Mousy"})

	ex2, _ := exRepo.Create(t.Context(), "user1", "Emma Report", "emma style")
	exRepo.SetClassNames(t.Context(), ex2.ID, []string{"Emma"})

	gen := &gptReportGenerator{
		noteRepo:    noteRepo,
		reportRepo:  reportRepo,
		exampleRepo: exRepo,
	}

	examples, err := gen.loadExamples(t.Context(), "user1", "Mousy")
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) != 1 {
		t.Fatalf("got %d examples, want 1", len(examples))
	}
	if examples[0].Name != "Mousy Report" {
		t.Errorf("name = %q, want 'Mousy Report'", examples[0].Name)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd backend && go test -run TestLoadExamples_FiltersByClassName -v
```

**Step 3: Update loadExamples**

Change `loadExamples` to accept `className` and use `ListReadyByClassName`:

```go
func (g *gptReportGenerator) loadExamples(ctx context.Context, userID, className string) ([]ReportExample, error) {
	if userID == "" {
		return nil, nil
	}
	dbExamples, err := g.exampleRepo.ListReadyByClassName(ctx, userID, className)
	if err != nil {
		return nil, fmt.Errorf("report: list examples: %w", err)
	}
	examples := make([]ReportExample, len(dbExamples))
	for i, e := range dbExamples {
		examples[i] = ReportExample{ID: e.ID, Name: e.Name, Content: e.Content, Status: e.Status}
	}
	return examples, nil
}
```

Update `Generate` and `Regenerate` callers to pass the class name. The `Class` field in `GenerateReportRequest` contains the display name (e.g. "Mousy Thursday"), but we need the `class_name` ("Mousy"). 

**Option:** Add `ClassName` field to `GenerateReportRequest` and `RegenerateReportRequest`. The handler in `reports_handler.go` needs to look up the class for the student and pass `class.ClassName`.

In `Generate`:
```go
examples, err := g.loadExamples(ctx, req.UserID, req.ClassName)
```

In `handleGenerateReports`, when building the request, look up the student's class to get `ClassName`:
```go
student, _ := serviceDeps.GetStudentRepo().GetByID(ctx, s.StudentID)
class, _ := serviceDeps.GetClassRepo().GetByID(ctx, student.ClassID)
// ...
resp, err := generator.Generate(ctx, GenerateReportRequest{
	// ...
	ClassName: class.ClassName,
})
```

Similarly for `handleRegenerateReport` (it already looks up the class).

**Step 4: Run tests**

```bash
cd backend && go test -run TestLoadExamples_FiltersByClassName -v
```

**Step 5: Fix compilation, run full suite**

```bash
cd backend && make test && make lint
```

**Step 6: Commit**

```bash
git add backend/report_generator.go backend/report_generator_test.go backend/reports_handler.go
git commit -m "feat: filter report examples by class name during generation"
```

---

### Task 6: Update report examples handler for classNames

**Files:**
- Modify: `backend/report_examples_handler.go` (read it first — it contains `handleUploadReportExample`, `handleUpdateReportExample`, etc.)

**Step 1: Write failing test**

Add to `backend/report_examples_handler_test.go`:

```go
func TestUploadExample_RequiresClassNames(t *testing.T) {
	store := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: store})

	// Upload text file without classNames — should fail
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "notes.txt")
	part.Write([]byte("content"))
	writer.Close()

	r := httptest.NewRequest(http.MethodPost, "/report-examples", &buf)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUploadReportExample(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateExample_UpdatesClassNames(t *testing.T) {
	stub := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: stub})

	body, _ := json.Marshal(map[string]interface{}{
		"name":       "Updated",
		"content":    "New content",
		"classNames": []string{"Mousy", "Emma"},
	})
	r := httptest.NewRequest(http.MethodPut, "/report-examples/1", bytes.NewReader(body))
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd backend && go test -run "TestUploadExample_RequiresClassNames|TestUpdateExample_UpdatesClassNames" -v
```

**Step 3: Update handlers**

Read `backend/report_examples_handler.go` first to understand current upload flow.

For upload:
- Parse `classNames` from the multipart form (as a JSON-encoded field or repeated form field).
- Validate non-empty.
- Pass to `ExampleStore.UploadExample` / `CreatePendingExample`.

For update:
- Parse `classNames` from JSON body.
- Pass to `ExampleStore.UpdateExample`.

For list:
- Ensure `ClassNames` field is populated in response.

**Step 4: Run tests**

```bash
cd backend && go test -run "TestUploadExample_|TestUpdateExample_" -v
```

**Step 5: Fix all tests, lint**

```bash
cd backend && make test && make lint
```

**Step 6: Commit**

```bash
git add backend/report_examples_handler.go backend/report_examples_handler_test.go
git commit -m "feat: require classNames on report example upload/update"
```

---

### Task 7: Generate TypeScript types and update frontend API layer

**Files:**
- Modify: `backend/tygo.yaml` (if needed)
- Regenerate: `frontend/src/api-types.gen.ts`
- Modify: `frontend/src/api.ts`

**Step 1: Regenerate types**

```bash
cd backend && make generate
```

**Step 2: Update frontend API functions**

In `frontend/src/api.ts`:

- `createClass(className, group, getToken)` — send `{ className, group }` instead of `{ name }`.
- `renameClass(id, className, group, getToken)` — send `{ className, group }`.
- Add `listClassNames(getToken): Promise<{ classNames: string[] }>` — calls `GET /classes/class-names`.
- `uploadReportExample(file, classNames, getToken)` — include `classNames` in form data.
- `updateReportExample(id, name, content, classNames, getToken)` — include `classNames` in body.

**Step 3: Commit**

```bash
git add frontend/src/api-types.gen.ts frontend/src/api.ts
git commit -m "feat: update API types and functions for class categories"
```

---

### Task 8: Update AddClassForm component

**Files:**
- Modify: `frontend/src/components/AddClassForm.tsx`

**Step 1: Update form to two fields**

- Replace single "Class name" input with two: "Class" (with autocomplete) and "Group" (plain text).
- Fetch `listClassNames` on mount for autocomplete suggestions.
- "Class" field: show dropdown of existing class names as user types, allow freeform entry.
- "Group" field: plain text input (optional but encouraged).
- On submit: call `createClass(className, group, getToken)`.

**Step 2: Test manually + verify build**

```bash
cd frontend && npm run build
```

**Step 3: Commit**

```bash
git add frontend/src/components/AddClassForm.tsx
git commit -m "feat: split class form into Class + Group with autocomplete"
```

---

### Task 9: Update ReportExamples component for class name tags

**Files:**
- Modify: `frontend/src/components/ReportExamples.tsx`

**Step 1: Add class name multi-select to upload and edit forms**

- On upload: add required multi-select populated from `listClassNames()`.
- On edit: show multi-select pre-populated with example's current `classNames`.
- Show class name badges on each example row in the list.
- Validation: disable save/upload button when no class names selected.

**Step 2: Verify build**

```bash
cd frontend && npm run build
```

**Step 3: Commit**

```bash
git add frontend/src/components/ReportExamples.tsx
git commit -m "feat: add class name tags to report example upload/edit"
```

---

### Task 10: Update class edit/rename flow in frontend

**Files:**
- Modify: any component that calls `renameClass` (search for `renameClass` in frontend)

**Step 1: Find callers**

```bash
cd frontend && grep -rn "renameClass" src/
```

**Step 2: Update to pass className + group**

Each call site needs to send the split fields. If there's an inline edit for class name, split it into two fields or parse from the current class object (which now has `className` and `groupName`).

**Step 3: Verify build**

```bash
cd frontend && npm run build
```

**Step 4: Commit**

```bash
git add -A frontend/src/
git commit -m "feat: update class rename flow for className + group"
```

---

### Task 11: Update ARCHITECTURE.md + run E2E sanity check

**Files:**
- Modify: `backend/ARCHITECTURE.md`

**Step 1: Update docs**

- Add `class_name`, `group_name` columns to classes table docs.
- Add `report_example_classes` join table.
- Add `GET /classes/class-names` route.
- Update `POST /classes` and `PUT /classes/{id}` request body docs.
- Update report examples endpoints to mention `classNames`.
- Note that report generation filters examples by class name.

**Step 2: Run E2E tests if available**

```bash
make test
```

```bash
cd frontend && npm run build
```

**Step 3: Commit**

```bash
git add backend/ARCHITECTURE.md
git commit -m "docs: update architecture for class categories"
```

---

### Task 12: Move design doc to implemented

```bash
mkdir -p docs/plans/implemented
mv docs/plans/2026-04-14-report-example-categories-design.md docs/plans/implemented/
git add docs/plans/
git commit -m "docs: move categories design to implemented"
```
