package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ownerGateFixture seeds one class owned by ownerID with one student in it, and
// points serviceDeps at the repos backing db.
func ownerGateFixture(t *testing.T, ownerID string) (db *sql.DB, studentID int64) {
	t.Helper()
	db = setupTestDB(t)
	classRepo := &ClassRepo{db: db}
	studentRepo := &StudentRepo{db: db}

	class := newTestClass(t, classRepo, "test-group", ownerID, "Math", "")
	student, err := studentRepo.Create(t.Context(), class.ID, "Zephyrine")
	require.NoError(t, err)

	origDeps := serviceDeps
	t.Cleanup(func() { serviceDeps = origDeps })
	serviceDeps = &mockDepsAll{db: db, classRepo: classRepo, studentRepo: studentRepo}

	return db, student.ID
}

// gateReq builds a request as callerID with a log-capturing context attached.
func gateReq(t *testing.T, callerID string) (*http.Request, *bytes.Buffer) {
	t.Helper()
	return gateReqPath(t, callerID, "/api/students/1/notes")
}

// gateReqPath is gateReq with an explicit URL path.
func gateReqPath(t *testing.T, callerID, path string) (*http.Request, *bytes.Buffer) {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, http.NoBody)
	req = clerkReq(req, callerID)
	ctx, logs := captureLogs(req.Context())
	return req.WithContext(ctx), logs
}

// The 404 text deliberately carries a student name, because handleGenerateReports
// echoes the name the caller supplied. Using it here is what makes the "never
// logged" assertions meaningful.
const gateNotFoundMsg = "student Zephyrine not found"

// TestRequireStudentOwnership covers the three outcomes of the shared gate. The
// two refusals must be one indistinguishable 404 to the caller while staying two
// separate events in telemetry: a check that could not run is an outage, a check
// that ran and said no is a denial (docs/adr/0003 keeps the name out of both).
func TestRequireStudentOwnership(t *testing.T) {
	t.Run("owner passes and nothing is logged", func(t *testing.T) {
		_, studentID := ownerGateFixture(t, "user_owner")
		req, logs := gateReq(t, "user_owner")
		rec := httptest.NewRecorder()

		ok := requireStudentOwnership(rec, req, studentID, "user_owner", gateNotFoundMsg)

		assert.True(t, ok, "the owning caller should pass the gate")
		assert.Empty(t, rec.Body.String(), "a passing gate must not write a response")
		assert.Empty(t, logs.String(), "a passing gate is not an event")
	})

	t.Run("denial is logged at warn", func(t *testing.T) {
		_, studentID := ownerGateFixture(t, "user_owner")
		req, logs := gateReq(t, "user_intruder")
		rec := httptest.NewRecorder()

		ok := requireStudentOwnership(rec, req, studentID, "user_intruder", gateNotFoundMsg)

		require.False(t, ok, "another teacher's student must not pass the gate")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, gateNotFoundMsg, decodeGateError(t, rec))

		out := logs.String()
		require.Contains(t, out, "ownership check denied", "a denial left no trace anywhere")
		assert.Contains(t, out, `"level":"WARN"`, "a denial is queryable, not a page")
		assert.Contains(t, out, `"student_id":`+strconv.FormatInt(studentID, 10), "the denial should carry the student id")
		// Called from a subtest closure here, so op is that closure's name; the
		// assertion that it resolves to the real handler lives in reports_gate_test.go.
		assert.Contains(t, out, `"op":`, "without a site label all sixteen call sites log the same line")
		assert.NotContains(t, out, "Zephyrine", "telemetry must not name the student")
		assert.NotContains(t, out, "ownership check failed", "a denial is not an outage")
	})

	t.Run("outage is logged at error", func(t *testing.T) {
		db, studentID := ownerGateFixture(t, "user_owner")
		require.NoError(t, db.Close())

		req, logs := gateReq(t, "user_owner")
		rec := httptest.NewRecorder()

		ok := requireStudentOwnership(rec, req, studentID, "user_owner", gateNotFoundMsg)

		require.False(t, ok, "a check that could not run must not be treated as ownership")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, gateNotFoundMsg, decodeGateError(t, rec))

		out := logs.String()
		require.Contains(t, out, "ownership check failed", "a failed ownership check vanished from telemetry")
		assert.Contains(t, out, `"level":"ERROR"`, "an outage is not routine")
		assert.Contains(t, out, `"student_id":`+strconv.FormatInt(studentID, 10), "the outage should carry the student id")
		assert.Contains(t, out, `"error":"check student ownership: sql: database is closed"`, "the outage should carry the underlying failure")
		assert.NotContains(t, out, "Zephyrine", "telemetry must not name the student")
	})

	t.Run("both refusals are byte-identical to the caller", func(t *testing.T) {
		denied := httptest.NewRecorder()
		_, studentID := ownerGateFixture(t, "user_owner")
		req, _ := gateReq(t, "user_intruder")
		require.False(t, requireStudentOwnership(denied, req, studentID, "user_intruder", gateNotFoundMsg))

		outage := httptest.NewRecorder()
		db2, studentID2 := ownerGateFixture(t, "user_owner")
		require.NoError(t, db2.Close())
		req2, _ := gateReq(t, "user_owner")
		require.False(t, requireStudentOwnership(outage, req2, studentID2, "user_owner", gateNotFoundMsg))

		assert.Equal(t, denied.Code, outage.Code,
			"an outage must not be distinguishable from a denial by status")
		assert.Equal(t, denied.Body.Bytes(), outage.Body.Bytes(),
			"an outage must not be distinguishable from a denial by body")
		assert.Equal(t, denied.Result().Header, outage.Result().Header,
			"an outage must not be distinguishable from a denial by headers")
	})

	// The router now 404s a stray trailing segment before any handler runs, but
	// the gate itself is called with whatever path the request carries. If the
	// record ever logged r.URL.Path, a caller could park a child's name there and
	// have it written to Sentry — the exact bug ADR 0003 exists to stop. This is
	// what keeps that constraint load-bearing rather than incidental.
	t.Run("a name planted in the request path is never logged", func(t *testing.T) {
		_, studentID := ownerGateFixture(t, "user_owner")
		req, logs := gateReqPath(t, "user_intruder", "/api/notes/5/Zephyrine")
		rec := httptest.NewRecorder()

		require.False(t, requireStudentOwnership(rec, req, studentID, "user_intruder", gateNotFoundMsg))

		out := logs.String()
		require.Contains(t, out, "ownership check denied", "the denial should still be recorded")
		assert.NotContains(t, out, "Zephyrine", "caller-controlled path text must not reach telemetry")
	})
}

func decodeGateError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp["error"]
}
