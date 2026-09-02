package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A note is dated from the voice note's upload time, not from the clock at
// processing time. The two diverge whenever a job is retried or waits behind a
// backlog, and the processing clock would then drop the note out of the report
// window it belongs to.
func TestProcessJob_DatesNoteFromUploadTime(t *testing.T) {
	db := setupTestDB(t)
	studentRepo := &StudentRepo{db: db}
	classRepo := &ClassRepo{db: db}
	voiceNoteRepo := &VoiceNoteRepo{db: db}

	cls := newTestClass(t, classRepo, "test-group", "user1", "Math", "")
	_, err := studentRepo.Create(t.Context(), cls.ID, "Alice")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "recording.m4a")
	require.NoError(t, os.WriteFile(audioPath, []byte("fake audio"), 0o644))

	queue := newStubVoiceNoteQueue()
	nc := &stubNoteCreator{results: []*CreateNoteResponse{{NoteID: 1}}}
	d := &mockDepsAll{
		transcriber: &stubTranscriber{result: "Alice did great today."},
		roster: &stubRoster{
			classNames: []string{"Math"},
			students:   []ClassGroup{{Name: "Math", Students: []ClassStudent{{Name: "Alice"}}}},
		},
		extractor: &stubExtractor{result: &ExtractResponse{
			Students: []MatchedStudent{
				{Name: "Alice", ClassName: "Math · Mon", QuotedText: "Did great", Confidence: 0.9},
			},
		}},
		noteCreator:   nc,
		studentRepo:   studentRepo,
		voiceNoteRepo: voiceNoteRepo,
	}

	// Uploaded well in the past: a job retried days later, or one that sat in a
	// backlog. Processing happens now.
	uploadedAt := time.Date(2026, 2, 10, 16, 15, 0, 0, time.UTC)

	ctx := context.Background()
	require.NoError(t, queue.Publish(ctx, VoiceNoteJob{
		UserID:    "user1",
		UploadID:  1,
		FilePath:  audioPath,
		FileName:  "recording.m4a",
		Status:    JobStatusQueued,
		CreatedAt: uploadedAt,
	}))
	require.NoError(t, processVoiceNote(ctx, d, queue, voiceNoteKey("user1", 1)))

	require.Len(t, nc.calls, 1)
	assert.Equal(t, "2026-02-10", nc.calls[0].Date,
		"note should carry the upload day, not the processing day")
}

// The extraction schema no longer asks the model for a date, so a model that
// volunteers one cannot reach the database.
func TestExtractResponseSchema_HasNoDateField(t *testing.T) {
	raw := extractResponseSchema([]ClassGroup{{Name: "Math"}})

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))

	assert.NotContains(t, schema.Properties, "date")
	assert.NotContains(t, schema.Required, "date")
}

// The UI sends <input type="date">, but the endpoint is reachable directly and
// notes.date is a bare TEXT column the report query compares with BETWEEN — a
// non-date string there silently excludes the note from every report.
func TestHandleCreateNote_RejectsMalformedDate(t *testing.T) {
	ctx, r := testDBAndRepos(t)
	serviceDeps = &mockDepsAll{
		db:          r.notes.db,
		classRepo:   r.classes,
		studentRepo: r.students,
		noteRepo:    r.notes,
	}
	t.Cleanup(func() { serviceDeps = nil })

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)

	post := func(t *testing.T, date string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]string{"date": date, "summary": "Did well"})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/students/%d/notes", s.ID), bytes.NewReader(body))
		req.SetPathValue("id", itoa(s.ID))
		req = withClerkUser(req, "user1")
		rec := httptest.NewRecorder()
		handleCreateNote(rec, req)
		return rec
	}

	for _, date := range []string{"Saturday", "Friday, Marcia, 1740", "1615-01", "2026-13-45", "22/03/2026"} {
		t.Run(date, func(t *testing.T) {
			rec := post(t, date)
			require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
			var resp map[string]string
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
			assert.Equal(t, "date must be YYYY-MM-DD", resp["error"])
		})
	}

	t.Run("well-formed date is accepted", func(t *testing.T) {
		rec := post(t, "2026-03-22")
		require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	})
}

// The guard in migration 015 must discard the whole file, not just the statement that
// raised. RAISE(ABORT) alone would not do it — it rolls back only its own statement and
// leaves the transaction open — so this pins the tx.Rollback() in migrate.go that does.
func TestMigration015_RollsBackWholeFileWhenGuardFires(t *testing.T) {
	ctx, r := testDBAndRepos(t)
	db := r.notes.db

	// Re-arm 015 so RunMigrations replays it over the rows seeded below.
	_, err := db.Exec("DELETE FROM _migrations WHERE name = ?", "015_repair_auto_note_dates.sql")
	require.NoError(t, err)

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)

	// An auto note the repair would rewrite...
	auto := &Note{StudentID: s.ID, Date: "2023-10-21", Summary: "hallucinated year", Source: "auto"}
	require.NoError(t, r.notes.Create(ctx, auto))
	// ...and a manual note the guard rejects, e.g. from a restored backup predating the
	// API validation.
	bad := &Note{StudentID: s.ID, Date: "Saturday", Summary: "not a date", Source: "manual"}
	require.NoError(t, r.notes.Create(ctx, bad))

	err = RunMigrations(db)
	require.Error(t, err, "guard should fail the migration")
	assert.Contains(t, err.Error(), "migration 015")

	var got string
	require.NoError(t, db.QueryRow("SELECT date FROM notes WHERE id = ?", auto.ID).Scan(&got))
	assert.Equal(t, "2023-10-21", got,
		"the repair must not survive a failed guard")
}

// The repair covers every auto row, not only the visibly broken ones — a hallucination
// that landed inside the right year is indistinguishable from a real date, so a test on
// the year would leave it wrong and silent. Manual rows are never touched.
func TestMigration015_RepairsEveryAutoRow(t *testing.T) {
	ctx, r := testDBAndRepos(t)
	db := r.notes.db

	_, err := db.Exec("DELETE FROM _migrations WHERE name = ?", "015_repair_auto_note_dates.sql")
	require.NoError(t, err)

	c := newTestClass(t, r.classes, "test-group", "user1", "Math", "")
	s, err := r.students.Create(ctx, c.ID, "Alice")
	require.NoError(t, err)

	seed := func(date, source string) *Note {
		n := &Note{StudentID: s.ID, Date: date, Summary: "obs", Source: source}
		require.NoError(t, r.notes.Create(ctx, n))
		return n
	}
	// Same year as the insert day but the wrong month — the case a year comparison
	// cannot catch, and the whole reason the repair is blanket rather than targeted.
	// Shifting the month keeps it in the current year while guaranteeing it is never
	// today, so the assertion below cannot pass by coincidence.
	now := time.Now().UTC()
	sameYearDate := time.Date(now.Year(), (now.Month()%12)+1, 15, 0, 0, 0, 0, time.UTC).
		Format(time.DateOnly)

	malformed := seed("Friday, Marcia, 1740", "auto")
	offYear := seed("2023-10-21", "auto")
	sameYearWrong := seed(sameYearDate, "auto")
	manualBackdated := seed("2019-04-02", "manual")

	require.NoError(t, RunMigrations(db))

	var insertDay string
	require.NoError(t, db.QueryRow(
		"SELECT substr(created_at,1,10) FROM notes WHERE id = ?", malformed.ID).Scan(&insertDay))

	dateOf := func(id int64) string {
		var d string
		require.NoError(t, db.QueryRow("SELECT date FROM notes WHERE id = ?", id).Scan(&d))
		return d
	}
	assert.Equal(t, insertDay, dateOf(malformed.ID))
	assert.Equal(t, insertDay, dateOf(offYear.ID))
	assert.Equal(t, insertDay, dateOf(sameYearWrong.ID),
		"a hallucination inside the right year must be repaired too — this is the row "+
			"a targeted migration would have left wrong and silent")
	require.NotEqual(t, insertDay, sameYearDate, "seed must differ from the insert day")
	assert.Equal(t, "2019-04-02", dateOf(manualBackdated.ID),
		"a teacher may legitimately backdate a manual note")

	// updated_at records teacher edits; a repair the teacher never saw is not one.
	var updatedAt, createdAt string
	require.NoError(t, db.QueryRow(
		"SELECT updated_at, created_at FROM notes WHERE id = ?", offYear.ID).Scan(&updatedAt, &createdAt))
	assert.Equal(t, createdAt, updatedAt, "repair must not stamp updated_at")
}
