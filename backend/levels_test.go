package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// levelsTestDeps sets up a real DB-backed deps for handler-level tests, since
// levels handlers go straight through serviceDeps.GetLevelRepo().
func levelsTestDeps(t *testing.T) {
	t.Helper()
	db := setupTestDB(t)
	prevDeps := serviceDeps
	serviceDeps = &mockDepsAll{db: db, levelRepo: &LevelRepo{db: db}}
	t.Cleanup(func() { serviceDeps = prevDeps })
}

func levelsReq(method, path string, body []byte, orgID, orgRole string) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, http.NoBody)
	}
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "test-user"},
		Claims: clerk.Claims{
			ActiveOrganizationID:   orgID,
			ActiveOrganizationRole: orgRole,
		},
	})
	return r.WithContext(ctx)
}

func TestHandleListLevels_ScopedToCallersGroup(t *testing.T) {
	levelsTestDeps(t)
	db := serviceDeps.GetDB()
	newTestLevel(t, db, "org_a", "Marcia")
	newTestLevel(t, db, "org_b", "Oliver")

	r := levelsReq(http.MethodGet, "/levels", nil, "org_a", "org:member")
	w := httptest.NewRecorder()
	handleListLevels(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp ListLevelsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Levels, 1)
	assert.Equal(t, "Marcia", resp.Levels[0].Name)
}

func TestHandleListLevels_TeacherCanRead(t *testing.T) {
	levelsTestDeps(t)
	newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	r := levelsReq(http.MethodGet, "/levels", nil, "org_a", "org:member")
	w := httptest.NewRecorder()
	handleListLevels(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleCreateLevel_AdminSucceeds(t *testing.T) {
	levelsTestDeps(t)

	body, err := json.Marshal(map[string]string{"name": "Marcia"})
	require.NoError(t, err)
	r := levelsReq(http.MethodPost, "/levels", body, "org_a", "org:admin")
	w := httptest.NewRecorder()
	handleCreateLevel(w, r)

	require.Equal(t, http.StatusCreated, w.Code)
	var l Level
	require.NoError(t, json.NewDecoder(w.Body).Decode(&l))
	assert.Equal(t, "Marcia", l.Name)
	assert.Equal(t, "org_a", l.GroupID)
}

func TestHandleCreateLevel_TrimsWhitespace(t *testing.T) {
	levelsTestDeps(t)

	body, err := json.Marshal(map[string]string{"name": "  Marcia  "})
	require.NoError(t, err)
	r := levelsReq(http.MethodPost, "/levels", body, "org_a", "org:admin")
	w := httptest.NewRecorder()
	handleCreateLevel(w, r)

	require.Equal(t, http.StatusCreated, w.Code)
	var l Level
	require.NoError(t, json.NewDecoder(w.Body).Decode(&l))
	assert.Equal(t, "Marcia", l.Name)
}

func TestHandleCreateLevel_WhitespaceOnlyName_Rejected(t *testing.T) {
	levelsTestDeps(t)

	body, err := json.Marshal(map[string]string{"name": "   "})
	require.NoError(t, err)
	r := levelsReq(http.MethodPost, "/levels", body, "org_a", "org:admin")
	w := httptest.NewRecorder()
	handleCreateLevel(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateLevel_TeacherRefused(t *testing.T) {
	levelsTestDeps(t)

	body, err := json.Marshal(map[string]string{"name": "Marcia"})
	require.NoError(t, err)
	r := levelsReq(http.MethodPost, "/levels", body, "org_a", "org:member")
	w := httptest.NewRecorder()
	handleCreateLevel(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)

	// Refusal must not create the row.
	list, err := (&LevelRepo{db: serviceDeps.GetDB()}).List(r.Context(), "org_a")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestHandleUpdateLevel_RenameCollision_Returns409WithClearMessage(t *testing.T) {
	levelsTestDeps(t)
	db := serviceDeps.GetDB()
	newTestLevel(t, db, "org_a", "Marcia")
	target := newTestLevel(t, db, "org_a", "Oliver")

	body, err := json.Marshal(map[string]string{"name": "Marcia"})
	require.NoError(t, err)
	r := levelsReq(http.MethodPut, "/levels/", body, "org_a", "org:admin")
	// pathParam reads from r.URL.Path, set directly since httptest.NewRequest parsed "/levels/".
	r.URL.Path = "/levels/" + itoa(target.ID)
	w := httptest.NewRecorder()
	handleUpdateLevel(w, r)

	require.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "already exists")
}

func TestHandleUpdateLevel_TeacherWriteRefused(t *testing.T) {
	levelsTestDeps(t)
	target := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	body, err := json.Marshal(map[string]string{"name": "New Name"})
	require.NoError(t, err)
	r := levelsReq(http.MethodPut, "/levels/"+itoa(target.ID), body, "org_a", "org:member")
	r.URL.Path = "/levels/" + itoa(target.ID)
	w := httptest.NewRecorder()
	handleUpdateLevel(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleUpdateLevel_ReportInstructions(t *testing.T) {
	levelsTestDeps(t)
	target := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	body, err := json.Marshal(map[string]string{"reportInstructions": "Focus on fluency."})
	require.NoError(t, err)
	r := levelsReq(http.MethodPut, "/levels/"+itoa(target.ID), body, "org_a", "org:admin")
	r.URL.Path = "/levels/" + itoa(target.ID)
	w := httptest.NewRecorder()
	handleUpdateLevel(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var l Level
	require.NoError(t, json.NewDecoder(w.Body).Decode(&l))
	assert.Equal(t, "Focus on fluency.", l.ReportInstructions)
}

func TestHandleUpdateLevel_CrossGroup_NotFound(t *testing.T) {
	levelsTestDeps(t)
	target := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	body, err := json.Marshal(map[string]string{"name": "New Name"})
	require.NoError(t, err)
	r := levelsReq(http.MethodPut, "/levels/"+itoa(target.ID), body, "org_b", "org:admin")
	r.URL.Path = "/levels/" + itoa(target.ID)
	w := httptest.NewRecorder()
	handleUpdateLevel(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleDeleteLevel_AdminSucceeds(t *testing.T) {
	levelsTestDeps(t)
	target := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	r := levelsReq(http.MethodDelete, "/levels/"+itoa(target.ID), nil, "org_a", "org:admin")
	r.URL.Path = "/levels/" + itoa(target.ID)
	w := httptest.NewRecorder()
	handleDeleteLevel(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleDeleteLevel_TeacherRefused(t *testing.T) {
	levelsTestDeps(t)
	target := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	r := levelsReq(http.MethodDelete, "/levels/"+itoa(target.ID), nil, "org_a", "org:member")
	r.URL.Path = "/levels/" + itoa(target.ID)
	w := httptest.NewRecorder()
	handleDeleteLevel(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)

	// Refused write must not have deleted the row.
	_, err := (&LevelRepo{db: serviceDeps.GetDB()}).GetByID(r.Context(), "org_a", target.ID)
	assert.NoError(t, err)
}

func TestHandleDeleteLevel_CrossGroup_NotFound(t *testing.T) {
	levelsTestDeps(t)
	target := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	r := levelsReq(http.MethodDelete, "/levels/"+itoa(target.ID), nil, "org_b", "org:admin")
	r.URL.Path = "/levels/" + itoa(target.ID)
	w := httptest.NewRecorder()
	handleDeleteLevel(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleDeleteLevel_ReferencedByClass_Returns409WithCount(t *testing.T) {
	levelsTestDeps(t)
	db := serviceDeps.GetDB()
	target := newTestLevel(t, db, "org_a", "Marcia")
	classRepo := &ClassRepo{db: db}
	_, err := classRepo.Create(context.Background(), "org_a", "user_1", target.ID, "Monday", "")
	require.NoError(t, err)
	_, err = classRepo.Create(context.Background(), "org_a", "user_1", target.ID, "Monday", "AM")
	require.NoError(t, err)

	r := levelsReq(http.MethodDelete, "/levels/"+itoa(target.ID), nil, "org_a", "org:admin")
	r.URL.Path = "/levels/" + itoa(target.ID)
	w := httptest.NewRecorder()
	handleDeleteLevel(w, r)

	require.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "2 classes use this Level")

	// Refused delete must not have removed the Level.
	_, err = (&LevelRepo{db: db}).GetByID(r.Context(), "org_a", target.ID)
	assert.NoError(t, err)
}

func TestHandleDeleteLevel_ReferencedByOneClass_Returns409WithSingularMessage(t *testing.T) {
	levelsTestDeps(t)
	db := serviceDeps.GetDB()
	target := newTestLevel(t, db, "org_a", "Marcia")
	classRepo := &ClassRepo{db: db}
	_, err := classRepo.Create(context.Background(), "org_a", "user_1", target.ID, "Monday", "")
	require.NoError(t, err)

	r := levelsReq(http.MethodDelete, "/levels/"+itoa(target.ID), nil, "org_a", "org:admin")
	r.URL.Path = "/levels/" + itoa(target.ID)
	w := httptest.NewRecorder()
	handleDeleteLevel(w, r)

	require.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "1 class uses this Level — move it to another Level first", resp["error"])
}

func TestHandleListLevels_NoActiveOrg_Refused(t *testing.T) {
	levelsTestDeps(t)

	r := levelsReq(http.MethodGet, "/levels", nil, "", "")
	w := httptest.NewRecorder()
	handleListLevels(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
