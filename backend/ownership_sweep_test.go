package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is the systematic tenancy sweep: every entity-scoped API route is
// driven as a caller from a different user AND a different Group against ids
// that belong to someone else, and must (a) refuse with a generic body that
// names nothing of the victim's, (b) leave every one of the victim's rows
// byte-for-byte as it was, and (c) for the requireStudentOwnership gate, log
// the denial by id only (docs/adr/0003).
//
// TestOwnershipSweep_CoversEveryRoute then closes the loop with the router:
// every apiRoutes entry must appear either in the denied table below or in
// notEntityScoped with a written reason. Adding a route without deciding
// which it is fails the suite.

// Two tenants. A is the victim whose rows every denied case targets; B is the
// caller. B is an org admin of G2 so the Level cases prove Group scoping, not
// merely the role gate.
const (
	sweepUserA  = "user_sweep_alpha"
	sweepGroupA = "org_sweep_alpha"
	sweepUserB  = "user_sweep_bravo"
	sweepGroupB = "org_sweep_bravo"
)

// A's content. Each string is one a leak would carry, and every denied
// response body is asserted not to contain any of them. They are chosen so no
// generic error text or B-side fixture value can contain them by accident.
const (
	sweepLevelName    = "Quillon"
	sweepStudentName  = "Zephyrine"
	sweepAliasName    = "Zeph" // also a prefix of the student name, so one NotContains covers both
	sweepNoteSummary  = "practiced chromatic scales"
	sweepReportHTML   = "<p>confidential-report-html</p>"
	sweepInstructions = "Write three warm paragraphs."
)

// sweepSecrets are the fragments of A's data that must never appear in a
// denied response or in telemetry.
var sweepSecrets = []string{sweepLevelName, sweepAliasName, "chromatic scales", "confidential-report-html"}

// sweepFixture is the two-tenant world plus the repos and queue backing it.
type sweepFixture struct {
	db          *sql.DB
	classRepo   *ClassRepo
	studentRepo *StudentRepo
	noteRepo    *NoteRepo
	reportRepo  *ReportRepo
	levelRepo   *LevelRepo
	feedback    *ArtifactFeedbackRepo
	voiceNotes  *VoiceNoteRepo
	queue       *stubVoiceNoteQueue

	// A's rows.
	levelA   Level
	classA   Class
	studentA Student
	aliasA   StudentAlias
	noteA    Note
	reportA  Report
	uploadA  VoiceNote

	// B's rows.
	levelB   Level
	classB   Class
	studentB Student
}

// newSweepFixture seeds both tenants and installs deps backed by the same DB.
// The report generator is a stub returning a fixed body so the two /reports
// positive controls can complete without an LLM.
func newSweepFixture(t *testing.T) *sweepFixture {
	t.Helper()
	ctx := context.Background()
	f := &sweepFixture{db: setupTestDB(t)}
	f.classRepo = &ClassRepo{db: f.db}
	f.studentRepo = &StudentRepo{db: f.db}
	f.noteRepo = &NoteRepo{db: f.db}
	f.reportRepo = &ReportRepo{db: f.db}
	f.levelRepo = &LevelRepo{db: f.db}
	f.feedback = &ArtifactFeedbackRepo{db: f.db}
	f.voiceNotes = &VoiceNoteRepo{db: f.db}
	f.queue = newStubVoiceNoteQueue()

	var err error

	// Tenant A.
	f.levelA = newTestLevel(t, f.db, sweepGroupA, sweepLevelName)
	require.NoError(t, f.levelRepo.UpdateReportInstructions(ctx, sweepGroupA, f.levelA.ID, sweepInstructions))
	f.classA, err = f.classRepo.Create(ctx, sweepGroupA, sweepUserA, f.levelA.ID, "Monday", "09:00")
	require.NoError(t, err)
	f.studentA, err = f.studentRepo.Create(ctx, f.classA.ID, sweepStudentName)
	require.NoError(t, err)
	f.aliasA, err = f.studentRepo.AddAlias(ctx, f.studentA.ID, sweepAliasName)
	require.NoError(t, err)
	f.noteA = Note{StudentID: f.studentA.ID, Date: "2026-02-03", Summary: sweepNoteSummary, Source: "auto"}
	require.NoError(t, f.noteRepo.Create(ctx, &f.noteA))
	f.reportA = Report{StudentID: f.studentA.ID, StartDate: "2026-01-01", EndDate: "2026-03-31", HTML: sweepReportHTML}
	require.NoError(t, f.reportRepo.Create(ctx, &f.reportA))
	f.uploadA, err = f.voiceNotes.Create(ctx, sweepUserA, "monday.m4a", "/nowhere/monday.m4a")
	require.NoError(t, err)
	require.NoError(t, f.queue.Publish(ctx, VoiceNoteJob{
		UserID: sweepUserA, UploadID: f.uploadA.ID, FileName: "monday.m4a", Status: JobStatusFailed, Error: "boom",
	}))

	// Tenant B.
	f.levelB = newTestLevel(t, f.db, sweepGroupB, "Brevity")
	require.NoError(t, f.levelRepo.UpdateReportInstructions(ctx, sweepGroupB, f.levelB.ID, "Be brief."))
	f.classB, err = f.classRepo.Create(ctx, sweepGroupB, sweepUserB, f.levelB.ID, "Tuesday", "14:00")
	require.NoError(t, err)
	f.studentB, err = f.studentRepo.Create(ctx, f.classB.ID, "Ozymandias")
	require.NoError(t, err)

	withDeps(t, &mockDepsAll{
		db:             f.db,
		classRepo:      f.classRepo,
		studentRepo:    f.studentRepo,
		noteRepo:       f.noteRepo,
		reportRepo:     f.reportRepo,
		levelRepo:      f.levelRepo,
		feedbackRepo:   f.feedback,
		voiceNoteRepo:  f.voiceNotes,
		voiceNoteQueue: f.queue,
		reportGen: &stubReportGenerator{
			generateResp:   &GenerateReportResponse{ReportID: 4242, HTML: "<p>generated</p>"},
			regenerateResp: &GenerateReportResponse{ReportID: 4343, HTML: "<p>regenerated</p>"},
		},
		uploadsDir: t.TempDir(),
	})
	return f
}

// sweepSnapshot is every value a denied request could have changed, read back
// from the DB and queue. Comparing two of them by value is the "nothing was
// mutated" assertion for every case; it is stricter than a per-case check
// because a mutation landing on an unexpected row is caught as well.
type sweepSnapshot struct {
	LevelAName, LevelAInstructions string
	LevelCount                     map[string]int // group -> number of levels
	ClassA                         Class
	ClassesForA, ClassesForB       int
	ClassesOnLevelA                int
	StudentsInClassA               []Student // names and aliases
	StudentBClassID                int64
	NotesForA                      []Note
	ReportsForA                    []Report
	FeedbackRows                   int
	JobAStatus                     string // "" when the job is gone
	UploadAProcessed               bool
}

func (f *sweepFixture) snapshot(t *testing.T) sweepSnapshot {
	t.Helper()
	ctx := context.Background()
	var s sweepSnapshot

	lvl, err := f.levelRepo.GetByID(ctx, sweepGroupA, f.levelA.ID)
	require.NoError(t, err)
	s.LevelAName, s.LevelAInstructions = lvl.Name, lvl.ReportInstructions
	s.LevelCount = map[string]int{}
	for _, g := range []string{sweepGroupA, sweepGroupB} {
		levels, err := f.levelRepo.List(ctx, g)
		require.NoError(t, err)
		s.LevelCount[g] = len(levels)
	}

	s.ClassA, err = f.classRepo.GetByID(ctx, f.classA.ID)
	require.NoError(t, err)
	for _, u := range []struct {
		id  string
		dst *int
	}{{sweepUserA, &s.ClassesForA}, {sweepUserB, &s.ClassesForB}} {
		classes, err := f.classRepo.List(ctx, u.id)
		require.NoError(t, err)
		*u.dst = len(classes)
	}
	require.NoError(t, f.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM classes WHERE level_id = ?", f.levelA.ID).Scan(&s.ClassesOnLevelA))

	s.StudentsInClassA, err = f.studentRepo.ListWithAliases(ctx, f.classA.ID)
	require.NoError(t, err)
	stuB, err := f.studentRepo.GetByID(ctx, f.studentB.ID)
	require.NoError(t, err)
	s.StudentBClassID = stuB.ClassID

	s.NotesForA, err = f.noteRepo.List(ctx, f.studentA.ID)
	require.NoError(t, err)
	summaries, err := f.reportRepo.List(ctx, f.studentA.ID)
	require.NoError(t, err)
	for _, r := range summaries {
		full, err := f.reportRepo.GetByID(ctx, r.ID)
		require.NoError(t, err)
		s.ReportsForA = append(s.ReportsForA, full)
	}
	require.NoError(t, f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM artifact_feedback").Scan(&s.FeedbackRows))

	if job, err := f.queue.GetJob(ctx, voiceNoteKey(sweepUserA, f.uploadA.ID)); err == nil {
		s.JobAStatus = job.Status
	}
	up, err := f.voiceNotes.GetByID(ctx, f.uploadA.ID)
	require.NoError(t, err)
	s.UploadAProcessed = up.ProcessedAt != nil
	return s
}

// deniedCase is one cross-tenant request. route is the apiRoutes key the case
// covers; path is that pattern with A's ids filled in. wantStatus/wantBody are
// the exact refusal contract; both are asserted by value.
type deniedCase struct {
	name       string
	route      string
	method     string
	path       func(f *sweepFixture) string
	body       func(f *sweepFixture) any // nil for no body
	wantStatus int
	wantBody   string // full JSON body, trailing newline included
	// studentGate marks a route that refuses through requireStudentOwnership,
	// so the denial must also appear in the log by student id and never by name.
	studentGate bool
	// gap, when set, marks a known refusal failure: the case is skipped with the
	// text so the table stays complete without turning the suite red.
	gap string
}

// jsonBody is the exact bytes writeJSON emits for a one-field error envelope.
func jsonBody(msg string) string { return `{"error":"` + msg + `"}` + "\n" }

var sweepDenied = []deniedCase{
	// --- class gate (verifyClassOwnership / user_id-scoped repo methods) ---
	{
		name: "list another user's students", route: "GET /api/classes/{id}/students", method: http.MethodGet,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/classes/%d/students", f.classA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("class not found"),
	},
	{
		name: "create a student in another user's class", route: "POST /api/classes/{id}/students", method: http.MethodPost,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/classes/%d/students", f.classA.ID) },
		body:       func(*sweepFixture) any { return map[string]any{"name": "Intruder"} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("class not found"),
	},
	{
		name: "update another user's class", route: "PUT /api/classes/{id}", method: http.MethodPut,
		path: func(f *sweepFixture) string { return fmt.Sprintf("/api/classes/%d", f.classA.ID) },
		// B's own Level, so the only thing that can refuse is class ownership.
		body: func(f *sweepFixture) any {
			return map[string]any{"levelId": f.levelB.ID, "day": "Friday", "timeSlot": ""}
		},
		wantStatus: http.StatusNotFound, wantBody: jsonBody("class or level not found"),
	},
	{
		name: "delete another user's class", route: "DELETE /api/classes/{id}", method: http.MethodDelete,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/classes/%d", f.classA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("class not found"),
	},
	{
		name: "move own student into another user's class", route: "PUT /api/students/{id}", method: http.MethodPut,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/students/%d", f.studentB.ID) },
		body:       func(f *sweepFixture) any { return map[string]any{"classId": f.classA.ID} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("target class not found"),
	},
	{
		name: "create a class on another Group's Level", route: "POST /api/classes", method: http.MethodPost,
		path: func(*sweepFixture) string { return "/api/classes" },
		body: func(f *sweepFixture) any {
			return map[string]any{"levelId": f.levelA.ID, "day": "Monday", "timeSlot": "10:00"}
		},
		wantStatus: http.StatusNotFound, wantBody: jsonBody("level not found"),
	},

	// --- student gate (requireStudentOwnership) ---
	{
		name: "rename another user's student", route: "PUT /api/students/{id}", method: http.MethodPut,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/students/%d", f.studentA.ID) },
		body:       func(*sweepFixture) any { return map[string]any{"name": "Renamed"} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student not found"), studentGate: true,
	},
	{
		name: "pull another user's student into own class", route: "PUT /api/students/{id}", method: http.MethodPut,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/students/%d", f.studentA.ID) },
		body:       func(f *sweepFixture) any { return map[string]any{"classId": f.classB.ID} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student not found"), studentGate: true,
	},
	{
		name: "delete another user's student", route: "DELETE /api/students/{id}", method: http.MethodDelete,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/students/%d", f.studentA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student not found"), studentGate: true,
	},
	{
		name: "list another user's aliases", route: "GET /api/students/{id}/aliases", method: http.MethodGet,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/students/%d/aliases", f.studentA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student not found"), studentGate: true,
	},
	{
		name: "add an alias to another user's student", route: "POST /api/students/{id}/aliases", method: http.MethodPost,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/students/%d/aliases", f.studentA.ID) },
		body:       func(*sweepFixture) any { return map[string]any{"alias": "Zed"} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student not found"), studentGate: true,
	},
	{
		name: "remove another user's alias", route: "DELETE /api/students/{id}/aliases/{aliasID}", method: http.MethodDelete,
		path: func(f *sweepFixture) string {
			return fmt.Sprintf("/api/students/%d/aliases/%d", f.studentA.ID, f.aliasA.ID)
		},
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student not found"), studentGate: true,
	},
	{
		name: "list another user's notes", route: "GET /api/students/{id}/notes", method: http.MethodGet,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/students/%d/notes", f.studentA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student not found"), studentGate: true,
	},
	{
		name: "create a note on another user's student", route: "POST /api/students/{id}/notes", method: http.MethodPost,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/students/%d/notes", f.studentA.ID) },
		body:       func(*sweepFixture) any { return map[string]any{"date": "2026-03-01", "summary": "planted"} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student not found"), studentGate: true,
	},
	{
		name: "read another user's note", route: "GET /api/notes/{id}", method: http.MethodGet,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/notes/%d", f.noteA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("note not found"), studentGate: true,
	},
	{
		name: "edit another user's note", route: "PUT /api/notes/{id}", method: http.MethodPut,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/notes/%d", f.noteA.ID) },
		body:       func(*sweepFixture) any { return map[string]any{"summary": "tampered"} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("note not found"), studentGate: true,
	},
	{
		name: "delete another user's note", route: "DELETE /api/notes/{id}", method: http.MethodDelete,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/notes/%d", f.noteA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("note not found"), studentGate: true,
	},
	{
		name: "list another user's reports", route: "GET /api/students/{id}/reports", method: http.MethodGet,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/students/%d/reports", f.studentA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student not found"), studentGate: true,
	},
	{
		name: "read another user's report", route: "GET /api/reports/{id}", method: http.MethodGet,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/reports/%d", f.reportA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("report not found"), studentGate: true,
	},
	{
		name: "delete another user's report", route: "DELETE /api/reports/{id}", method: http.MethodDelete,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/reports/%d", f.reportA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("report not found"), studentGate: true,
	},
	{
		name: "regenerate another user's report", route: "POST /api/reports/{id}/regenerate", method: http.MethodPost,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/reports/%d/regenerate", f.reportA.ID) },
		body:       func(*sweepFixture) any { return map[string]any{"feedback": "shorter"} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("report not found"), studentGate: true,
	},
	{
		name: "rate another user's report", route: "POST /api/feedback", method: http.MethodPost,
		path: func(*sweepFixture) string { return "/api/feedback" },
		body: func(f *sweepFixture) any {
			return map[string]any{"artifact_type": "report", "artifact_id": f.reportA.ID, "rating": "up"}
		},
		wantStatus: http.StatusNotFound, wantBody: jsonBody("report not found"), studentGate: true,
	},
	{
		name: "rate another user's note", route: "POST /api/feedback", method: http.MethodPost,
		path: func(*sweepFixture) string { return "/api/feedback" },
		body: func(f *sweepFixture) any {
			return map[string]any{"artifact_type": "note", "artifact_id": f.noteA.ID, "rating": "down"}
		},
		wantStatus: http.StatusNotFound, wantBody: jsonBody("note not found"), studentGate: true,
	},
	{
		// Name-scoped: the handler echoes the name the caller supplied, so the
		// body carries B's guess ("Probe"), never the real name behind the id.
		name: "generate a report for another user's student", route: "POST /api/reports", method: http.MethodPost,
		path: func(*sweepFixture) string { return "/api/reports" },
		body: func(f *sweepFixture) any {
			return map[string]any{
				"students":  []map[string]any{{"studentId": f.studentA.ID, "name": "Probe", "className": "Probe class"}},
				"startDate": "2026-01-01",
				"endDate":   "2026-03-31",
			}
		},
		wantStatus: http.StatusNotFound, wantBody: jsonBody("student Probe not found"), studentGate: true,
	},

	// --- Group gate (LevelRepo group_id scoping); B is an admin of G2 ---
	{
		name: "rename another Group's Level as admin", route: "PUT /api/levels/{id}", method: http.MethodPut,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/levels/%d", f.levelA.ID) },
		body:       func(*sweepFixture) any { return map[string]any{"name": "Hijacked"} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("level not found"),
	},
	{
		name: "overwrite another Group's report instructions as admin", route: "PUT /api/levels/{id}", method: http.MethodPut,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/levels/%d", f.levelA.ID) },
		body:       func(*sweepFixture) any { return map[string]any{"reportInstructions": "sabotaged"} },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("level not found"),
	},
	{
		name: "delete another Group's Level as admin", route: "DELETE /api/levels/{id}", method: http.MethodDelete,
		path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/levels/%d", f.levelA.ID) },
		wantStatus: http.StatusNotFound, wantBody: jsonBody("level not found"),
	},

	// --- job key gate (voiceNoteKey(userID, uploadID)) ---
	{
		// Not a 404: dismiss is a batch that skips keys it cannot find, and B's
		// key for A's upload id does not exist. The contract is "0 dismissed" and
		// A's job untouched.
		name: "dismiss another user's job", route: "POST /api/voice-notes/jobs/dismiss", method: http.MethodPost,
		path:       func(*sweepFixture) string { return "/api/voice-notes/jobs/dismiss" },
		body:       func(f *sweepFixture) any { return map[string]any{"uploadIds": []int64{f.uploadA.ID}} },
		wantStatus: http.StatusOK, wantBody: "{\"dismissed\":0}\n",
	},
	{
		// B names a class of B's own, so the only thing left that can refuse is
		// the row's user_id. A leaked transcript or a planted note would both
		// show up in the snapshot comparison.
		name: "assemble another user's recording", route: "POST /api/voice-notes/{uploadId}/assemble", method: http.MethodPost,
		path: func(f *sweepFixture) string { return fmt.Sprintf("/api/voice-notes/%d/assemble", f.uploadA.ID) },
		body: func(f *sweepFixture) any {
			return map[string]any{
				"className": f.classB.Name,
				"passages": []map[string]any{
					{"kind": string(PassageChild), "spokenLabels": []string{sweepStudentName}, "summary": "planted"},
				},
			}
		},
		wantStatus: http.StatusNotFound, wantBody: jsonBody("recording not found"),
	},
}

// notEntityScoped lists every route that takes no id belonging to anyone and
// so has nothing for the sweep to deny. Each entry says why. A route may not
// be both here and in sweepDenied.
var notEntityScoped = map[string]string{
	"GET /api/classes":                   "lists only the caller's own classes (ClassRepo.List is user_id-scoped)",
	"GET /api/levels":                    "lists only the caller's Group's Levels (LevelRepo.List is group_id-scoped)",
	"POST /api/levels":                   "creates under the caller's Group; the body carries a name, no foreign id",
	"POST /api/voice-notes/upload":       "creates an upload under the caller; multipart file only, no foreign id",
	"POST /api/text-notes/upload":        "creates under the caller; students are resolved by name within the caller's own roster",
	"POST /api/voice-notes/drive-import": "imports under the caller; the file id is Google's and read with the caller's own token",
	"GET /api/google-token":              "returns the caller's own Clerk OAuth token",
	"GET /api/voice-notes/jobs":          "lists only jobs whose key is prefixed by the caller's user id",
	"POST /api/voice-notes/jobs/retry":   "takes no ids; republishes only the caller's own failed jobs",
}

// sweepRequest builds the request for a case, with a log-capturing context so
// the denial record can be asserted on.
func sweepRequest(t *testing.T, f *sweepFixture, method, path string, body func(*sweepFixture) any) (*http.Request, *bytes.Buffer) {
	t.Helper()
	var rdr io.Reader = http.NoBody
	if body != nil {
		b, err := json.Marshal(body(f))
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	ctx, logs := captureLogs(req.Context())
	return req.WithContext(ctx), logs
}

// TestOwnershipSweep_DeniesOtherTenant runs every sweepDenied case as B against
// A's ids through the real router and auth wrapper.
func TestOwnershipSweep_DeniesOtherTenant(t *testing.T) {
	for _, c := range sweepDenied {
		t.Run(c.name, func(t *testing.T) {
			if c.gap != "" {
				t.Skip("GAP: " + c.gap)
			}
			f := newSweepFixture(t)
			mux := newAPIMux(fakeAuth(sweepUserB, sweepGroupB, "org:admin"))
			before := f.snapshot(t)

			req, logs := sweepRequest(t, f, c.method, c.path(f), c.body)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, c.wantStatus, rec.Code, "body: %s", rec.Body.String())
			assert.Equal(t, c.wantBody, rec.Body.String(), "the refusal must be the generic body, byte for byte")
			for _, secret := range sweepSecrets {
				assert.NotContains(t, rec.Body.String(), secret, "the response leaks the victim's data")
			}

			assert.Equal(t, before, f.snapshot(t), "a denied request must leave every row exactly as it was")

			out := logs.String()
			if c.studentGate {
				require.Contains(t, out, "ownership check denied", "the denial must be queryable in telemetry")
				assert.Contains(t, out, `"student_id":`+strconv.FormatInt(f.studentA.ID, 10), "the record should carry the victim's student id")
				for _, secret := range sweepSecrets {
					assert.NotContains(t, out, secret, "telemetry must not name the student (ADR 0003)")
				}
			} else {
				assert.NotContains(t, out, "ownership check denied", "this route refuses before the student gate; a record here means the table is mislabelled")
			}
		})
	}
}

// TestOwnershipSweep_OwnerSucceeds is the positive control, one per gate
// type: the same path that refuses B succeeds for A, so a denial above cannot
// be a route that refuses everyone.
func TestOwnershipSweep_OwnerSucceeds(t *testing.T) {
	cases := []struct {
		name, gate string
		method     string
		path       func(f *sweepFixture) string
		body       func(f *sweepFixture) any
		wantStatus int
		wantIn     string // a fragment of A's data the success body must carry
	}{
		{
			name: "class gate", gate: "verifyClassOwnership", method: http.MethodGet,
			path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/classes/%d/students", f.classA.ID) },
			wantStatus: http.StatusOK, wantIn: sweepStudentName,
		},
		{
			name: "student gate", gate: "requireStudentOwnership", method: http.MethodGet,
			path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/notes/%d", f.noteA.ID) },
			wantStatus: http.StatusOK, wantIn: sweepNoteSummary,
		},
		{
			name: "student gate via feedback body", gate: "requireStudentOwnership", method: http.MethodPost,
			path: func(*sweepFixture) string { return "/api/feedback" },
			body: func(f *sweepFixture) any {
				return map[string]any{"artifact_type": "report", "artifact_id": f.reportA.ID, "rating": "up"}
			},
			wantStatus: http.StatusCreated, wantIn: `"id":`,
		},
		{
			name: "student gate via regenerate", gate: "requireStudentOwnership", method: http.MethodPost,
			path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/reports/%d/regenerate", f.reportA.ID) },
			body:       func(*sweepFixture) any { return map[string]any{"feedback": "shorter"} },
			wantStatus: http.StatusOK, wantIn: sweepStudentName,
		},
		{
			name: "name-scoped generate", gate: "requireStudentOwnership", method: http.MethodPost,
			path: func(*sweepFixture) string { return "/api/reports" },
			body: func(f *sweepFixture) any {
				return map[string]any{
					"students":  []map[string]any{{"studentId": f.studentA.ID, "name": sweepStudentName, "className": f.classA.Name}},
					"startDate": "2026-01-01",
					"endDate":   "2026-03-31",
				}
			},
			wantStatus: http.StatusOK, wantIn: `"student":"` + sweepStudentName + `"`,
		},
		{
			name: "group gate", gate: "LevelRepo group_id scoping", method: http.MethodPut,
			path:       func(f *sweepFixture) string { return fmt.Sprintf("/api/levels/%d", f.levelA.ID) },
			body:       func(*sweepFixture) any { return map[string]any{"reportInstructions": "updated by owner"} },
			wantStatus: http.StatusOK, wantIn: sweepLevelName,
		},
		{
			name: "level cross-check on class create", gate: "ClassRepo.Create level/group check", method: http.MethodPost,
			path: func(*sweepFixture) string { return "/api/classes" },
			body: func(f *sweepFixture) any {
				return map[string]any{"levelId": f.levelA.ID, "day": "Wednesday", "timeSlot": ""}
			},
			wantStatus: http.StatusCreated, wantIn: sweepLevelName,
		},
		{
			name: "job key gate", gate: "voiceNoteKey", method: http.MethodPost,
			path:       func(*sweepFixture) string { return "/api/voice-notes/jobs/dismiss" },
			body:       func(f *sweepFixture) any { return map[string]any{"uploadIds": []int64{f.uploadA.ID}} },
			wantStatus: http.StatusOK, wantIn: `"dismissed":1`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newSweepFixture(t)
			mux := newAPIMux(fakeAuth(sweepUserA, sweepGroupA, "org:admin"))

			req, logs := sweepRequest(t, f, c.method, c.path(f), c.body)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			require.Equal(t, c.wantStatus, rec.Code, "%s: the owner must pass; body: %s", c.gate, rec.Body.String())
			assert.Contains(t, rec.Body.String(), c.wantIn)
			assert.NotContains(t, logs.String(), "ownership check denied", "the owner is not a denial")
		})
	}
}

// TestOwnershipSweep_CoversEveryRoute is the coverage guard: the set of router
// keys must equal sweepDenied's routes plus notEntityScoped's keys, with no
// route in both. A new route fails here until someone decides which it is.
func TestOwnershipSweep_CoversEveryRoute(t *testing.T) {
	registered := map[string]bool{}
	for _, rt := range apiRoutes {
		registered[rt.key()] = true
	}
	swept := map[string]bool{}
	for _, c := range sweepDenied {
		swept[c.route] = true
	}

	var missing, unknown, both []string
	for key := range registered {
		if !swept[key] && notEntityScoped[key] == "" {
			missing = append(missing, key)
		}
	}
	for key := range swept {
		if !registered[key] {
			unknown = append(unknown, key)
		}
		if notEntityScoped[key] != "" {
			both = append(both, key)
		}
	}
	for key, reason := range notEntityScoped {
		if !registered[key] {
			unknown = append(unknown, key)
		}
		assert.NotEmpty(t, strings.TrimSpace(reason), "%s: an exemption needs a reason", key)
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	sort.Strings(both)

	assert.Empty(t, missing, "routes neither swept nor exempted — add a sweepDenied case or a notEntityScoped reason")
	assert.Empty(t, unknown, "sweep/exemption entries that are not registered routes — stale after a router change")
	assert.Empty(t, both, "a route is either entity-scoped or not; remove it from one list")

	// A pin on the control itself: swept routes must still be denied by case,
	// not merely listed. Something to notice if sweepDenied were ever emptied.
	assert.GreaterOrEqual(t, len(swept), 20, "the sweep table has lost most of its routes")
}
