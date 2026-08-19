// students_class_day_test.go covers the handler-level day validation added to
// handleCreateClass/handleUpdateClass: a missing or invalid day must be
// rejected with a 400 before ever reaching ClassRepo, so a stale client never
// sees a raw CHECK-constraint 500.
package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classHandlerTestDeps wires a real DB-backed deps for class handler tests,
// mirroring levelsTestDeps.
func classHandlerTestDeps(t *testing.T) *ClassRepo {
	t.Helper()
	db := setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	prevDeps := serviceDeps
	serviceDeps = &mockDepsAll{db: db, classRepo: classRepo, levelRepo: &LevelRepo{db: db}}
	t.Cleanup(func() { serviceDeps = prevDeps })
	return classRepo
}

func TestHandleCreateClass_MissingDayRejected(t *testing.T) {
	classHandlerTestDeps(t)
	level := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	body, err := json.Marshal(map[string]any{"levelId": level.ID, "timeSlot": ""})
	require.NoError(t, err)
	r := levelsReq("POST", "/classes", body, "org_a", "org:member")
	w := httptest.NewRecorder()
	handleCreateClass(w, r)

	assert.Equal(t, 400, w.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp["error"], "day")
}

func TestHandleCreateClass_InvalidDayRejected(t *testing.T) {
	classHandlerTestDeps(t)
	level := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	body, err := json.Marshal(map[string]any{"levelId": level.ID, "day": "Someday", "timeSlot": ""})
	require.NoError(t, err)
	r := levelsReq("POST", "/classes", body, "org_a", "org:member")
	w := httptest.NewRecorder()
	handleCreateClass(w, r)

	assert.Equal(t, 400, w.Code)
}

func TestHandleCreateClass_ValidDayAccepted(t *testing.T) {
	classHandlerTestDeps(t)
	level := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")

	body, err := json.Marshal(map[string]any{"levelId": level.ID, "day": "Wednesday", "timeSlot": "14:10"})
	require.NoError(t, err)
	r := levelsReq("POST", "/classes", body, "org_a", "org:member")
	w := httptest.NewRecorder()
	handleCreateClass(w, r)

	require.Equal(t, 201, w.Code)
	var resp ClassWithCount
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "Marcia · Wed · 14:10", resp.Name)
}

func TestHandleUpdateClass_MissingDayRejected(t *testing.T) {
	classRepo := classHandlerTestDeps(t)
	level := newTestLevel(t, serviceDeps.GetDB(), "org_a", "Marcia")
	c, err := classRepo.Create(t.Context(), "org_a", "test-user", level.ID, "Wednesday", "")
	require.NoError(t, err)

	body, err := json.Marshal(map[string]any{"levelId": level.ID, "timeSlot": ""})
	require.NoError(t, err)
	r := levelsReq("PUT", "/classes/"+itoa(c.ID), body, "org_a", "org:member")
	r.URL.Path = "/classes/" + itoa(c.ID)
	w := httptest.NewRecorder()
	handleUpdateClass(w, r)

	assert.Equal(t, 400, w.Code)
}
