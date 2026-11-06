package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func termCall(t *testing.T, h http.HandlerFunc, method string, id int64, body any, role string) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		require.NoError(t, err)
	}
	r := levelsReq(method, "/terms", b, "org_a", role)
	if id != 0 {
		r.SetPathValue("id", itoa(id))
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func termBody(name, start, end string) map[string]string {
	return map[string]string{"name": name, "startDate": start, "endDate": end}
}

// No Level, so no seeded Test Term: this test owns the calendar.
func TestTerms_CreateEditDelete(t *testing.T) {
	levelsTestDeps(t)

	w := termCall(t, handleCreateTerm, http.MethodPost, 0, termBody(" Term 1 ", "2026-08-15", "2026-11-01"), "org:admin")
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var t1 Term
	require.NoError(t, json.NewDecoder(w.Body).Decode(&t1))
	assert.Equal(t, "Term 1", t1.Name)

	w = termCall(t, handleCreateTerm, http.MethodPost, 0, termBody("Term 2", "2026-11-02", "2027-01-31"), "org:admin")
	require.Equal(t, http.StatusCreated, w.Code)
	var t2 Term
	require.NoError(t, json.NewDecoder(w.Body).Decode(&t2))

	// Inclusive ends: sharing 2026-11-01 overlaps.
	w = termCall(t, handleUpdateTerm, http.MethodPut, t2.ID, termBody("Term 2", "2026-11-01", "2027-01-31"), "org:admin")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.JSONEq(t, `{"error":"Overlaps Term 1"}`, w.Body.String())

	w = termCall(t, handleUpdateTerm, http.MethodPut, t2.ID, termBody("Winter", "2026-11-03", "2027-02-01"), "org:admin")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"Winter"`)

	// Editing a Term never overlaps itself.
	w = termCall(t, handleUpdateTerm, http.MethodPut, t2.ID, termBody("Winter", "2026-11-04", "2027-02-01"), "org:admin")
	assert.Equal(t, http.StatusOK, w.Code)

	w = termCall(t, handleDeleteTerm, http.MethodDelete, t2.ID, nil, "org:admin")
	assert.Equal(t, http.StatusOK, w.Code)
	w = termCall(t, handleDeleteTerm, http.MethodDelete, t2.ID, nil, "org:admin")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestTerms_CreateRejectsBadInput(t *testing.T) {
	levelsTestDeps(t)
	termCall(t, handleCreateTerm, http.MethodPost, 0, termBody("Term 1", "2026-08-15", "2026-11-01"), "org:admin")

	for _, tc := range []struct {
		body any
		code int
		want string
	}{
		{termBody(" ", "2027-01-01", "2027-02-01"), http.StatusBadRequest, "name is required"},
		{termBody("T", "2027-1-1", "2027-02-01"), http.StatusBadRequest, "dates must be YYYY-MM-DD"},
		{termBody("T", "2027-03-01", "2027-02-01"), http.StatusBadRequest, "start date is after end date"},
		{termBody("T", "2026-01-01", "2026-08-15"), http.StatusConflict, "Overlaps Term 1"},
	} {
		w := termCall(t, handleCreateTerm, http.MethodPost, 0, tc.body, "org:admin")
		assert.Equal(t, tc.code, w.Code, tc.want)
		assert.JSONEq(t, `{"error":"`+tc.want+`"}`, w.Body.String())
	}
}

func TestTerms_TeacherRefused(t *testing.T) {
	levelsTestDeps(t)
	id := testTermID(t, serviceDeps.GetDB(), "org_a")
	body := termBody("T", "2027-01-01", "2027-02-01")
	assert.Equal(t, http.StatusForbidden, termCall(t, handleCreateTerm, http.MethodPost, 0, body, "org:member").Code)
	assert.Equal(t, http.StatusForbidden, termCall(t, handleUpdateTerm, http.MethodPut, id, body, "org:member").Code)
	assert.Equal(t, http.StatusForbidden, termCall(t, handleDeleteTerm, http.MethodDelete, id, nil, "org:member").Code)
}

func TestTerms_DeleteInUse(t *testing.T) {
	levelsTestDeps(t)
	db := serviceDeps.GetDB()
	level := newTestLevel(t, db, "org_a", "Marcia")
	w := termCall(t, handleCreateTerm, http.MethodPost, 0, termBody("Term 3", "2100-01-01", "2100-03-01"), "org:admin")
	var t3 Term
	require.NoError(t, json.NewDecoder(w.Body).Decode(&t3))

	putText := func(text string) {
		b, err := json.Marshal(map[string]any{"reportInstructions": text, "termId": t3.ID})
		require.NoError(t, err)
		r := levelsReq(http.MethodPut, "/levels", b, "org_a", "org:admin")
		r.SetPathValue("id", itoa(level.ID))
		w := httptest.NewRecorder()
		handleUpdateLevel(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	putText("Summer focus.")

	w = termCall(t, handleDeleteTerm, http.MethodDelete, t3.ID, nil, "org:admin")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.JSONEq(t, `{"error":"Term in use"}`, w.Body.String())

	// Blank text drops the row, freeing the Term.
	putText("  ")
	assert.Equal(t, http.StatusOK, termCall(t, handleDeleteTerm, http.MethodDelete, t3.ID, nil, "org:admin").Code)

	// A Report holds its Term too.
	term := testTermID(t, db, "org_a")
	_, err := db.Exec(`INSERT INTO classes (user_id, level_id, day) VALUES ('u', ?, 'Monday')`, level.ID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO students (class_id, name) VALUES ((SELECT MAX(id) FROM classes), 'Ann')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO reports (student_id, term_id, start_date, end_date, html)
		VALUES ((SELECT MAX(id) FROM students), ?, '2026-01-01', '2026-02-01', '<p/>')`, term)
	require.NoError(t, err)
	w = termCall(t, handleDeleteTerm, http.MethodDelete, term, nil, "org:admin")
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleListLevels_TextPerTerm(t *testing.T) {
	levelsTestDeps(t)
	db := serviceDeps.GetDB()
	level := newTestLevel(t, db, "org_a", "Marcia")
	current := testTermID(t, db, "org_a")
	repo := serviceDeps.GetLevelRepo()
	future, err := repo.Terms().Create(t.Context(), "org_a", "Next", "2100-01-01", "2100-03-01")
	require.NoError(t, err)
	require.NoError(t, repo.SetReportInstructions(t.Context(), "org_a", level.ID, current, "Now."))
	require.NoError(t, repo.SetReportInstructions(t.Context(), "org_a", level.ID, future.ID, "Later."))

	r := levelsReq(http.MethodGet, "/levels", nil, "org_a", "org:admin")
	w := httptest.NewRecorder()
	handleListLevels(w, r)
	var resp ListLevelsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Terms, 2)
	assert.Equal(t, []string{"Test Term", "Next"}, []string{resp.Terms[0].Name, resp.Terms[1].Name})
	assert.Equal(t, "Now.", resp.Levels[0].ReportInstructions)
	assert.Equal(t, map[int64]string{current: "Now.", future.ID: "Later."}, resp.Levels[0].InstructionsByTerm)
}

func TestTerms_ArchivedReadOnly(t *testing.T) {
	levelsTestDeps(t)
	db := serviceDeps.GetDB()
	level := newTestLevel(t, db, "org_a", "Marcia")
	var archived int64
	require.NoError(t, db.QueryRow(`INSERT INTO terms (group_id, name, start_date, end_date, archived_at)
		VALUES ('org_a', 'Old', '1990-01-01', '1990-06-01', '1990-07-01') RETURNING id`).Scan(&archived))

	body := termBody("Old", "1990-01-01", "1990-06-01")
	notFound := `{"error":"term not found"}`
	w := termCall(t, handleUpdateTerm, http.MethodPut, archived, body, "org:admin")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.JSONEq(t, notFound, w.Body.String())
	w = termCall(t, handleDeleteTerm, http.MethodDelete, archived, nil, "org:admin")
	assert.Equal(t, http.StatusNotFound, w.Code)

	b, err := json.Marshal(map[string]any{"reportInstructions": "x", "termId": archived})
	require.NoError(t, err)
	r := levelsReq(http.MethodPut, "/levels", b, "org_a", "org:admin")
	r.SetPathValue("id", itoa(level.ID))
	w = httptest.NewRecorder()
	handleUpdateLevel(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.JSONEq(t, notFound, w.Body.String())
	assert.ErrorIs(t, serviceDeps.GetLevelRepo().SetReportInstructions(t.Context(), "org_a", level.ID, archived, "x"), ErrNotFound)

	// An archived Term's dates don't block a new one.
	w = termCall(t, handleCreateTerm, http.MethodPost, 0, termBody("Archived span", "1990-02-01", "1990-03-01"), "org:admin")
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func TestTerms_Rollover(t *testing.T) {
	levelsTestDeps(t)
	db := serviceDeps.GetDB()
	repo := serviceDeps.GetLevelRepo()
	level := newTestLevel(t, db, "org_a", "Marcia")
	other := testTermID(t, db, "org_b")
	// newTestLevel's Test Term spans every date; archiving it frees the calendar.
	_, err := db.Exec(`UPDATE terms SET archived_at = '1990-07-01' WHERE group_id = 'org_a'`)
	require.NoError(t, err)
	t1, err := repo.Terms().Create(t.Context(), "org_a", "Term 1", "2027-09-01", "2027-12-20")
	require.NoError(t, err)
	t2, err := repo.Terms().Create(t.Context(), "org_a", "Term 2", "2028-01-05", "2028-02-29")
	require.NoError(t, err)
	require.NoError(t, repo.SetReportInstructions(t.Context(), "org_a", level.ID, t1.ID, "Autumn."))
	require.NoError(t, repo.SetReportInstructions(t.Context(), "org_a", level.ID, t2.ID, "Winter."))

	assert.Equal(t, http.StatusForbidden, termCall(t, handleRolloverTerms, http.MethodPost, 0, nil, "org:member").Code)
	w := termCall(t, handleRolloverTerms, http.MethodPost, 0, nil, "org:admin")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	active, err := repo.Terms().ListActive(t.Context(), "org_a")
	require.NoError(t, err)
	require.Len(t, active, 2)
	assert.Equal(t, Term{ID: active[0].ID, GroupID: "org_a", Name: "Term 1", StartDate: "2028-09-01", EndDate: "2028-12-20"}, active[0])
	// Feb 29 ends Feb 28, not Mar 1.
	assert.Equal(t, Term{ID: active[1].ID, GroupID: "org_a", Name: "Term 2", StartDate: "2029-01-05", EndDate: "2029-02-28"}, active[1])
	for i, want := range []string{"Autumn.", "Winter."} {
		text, err := repo.Terms().Instructions(t.Context(), level.ID, active[i].ID)
		require.NoError(t, err)
		assert.Equal(t, want, text)
	}
	var archivedCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM terms WHERE group_id = 'org_a' AND archived_at IS NOT NULL`).Scan(&archivedCount))
	assert.Equal(t, 3, archivedCount)
	var oldArchivedAt string
	require.NoError(t, db.QueryRow(`SELECT archived_at FROM terms WHERE group_id = 'org_a' AND name = 'Test Term'`).Scan(&oldArchivedAt))
	assert.Equal(t, "1990-07-01", oldArchivedAt)

	// Other Group untouched.
	b, err := repo.Terms().GetByID(t.Context(), "org_b", other)
	require.NoError(t, err)
	assert.Nil(t, b.ArchivedAt)

	_, err = db.Exec(`UPDATE terms SET archived_at = 'x' WHERE group_id = 'org_a'`)
	require.NoError(t, err)
	w = termCall(t, handleRolloverTerms, http.MethodPost, 0, nil, "org:admin")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.JSONEq(t, `{"error":"No active terms"}`, w.Body.String())
}
