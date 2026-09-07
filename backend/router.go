// router.go registers every /api/ route on a Go 1.22+ http.ServeMux using
// method+pattern strings, so path parameters come from r.PathValue and an
// unmatched path or method is a 404 rather than a prefix over-match.
package handler

import (
	"net/http"
	"strconv"
)

// apiMux is the process-wide API router, built once with the Clerk middleware.
// Tests build their own via newAPIMux with a fake auth wrapper.
var apiMux = newAPIMux(clerkAuthMiddleware)

// apiRoute is one registered (method, pattern) pair and its handler.
type apiRoute struct {
	Method, Pattern string
	Handler         http.HandlerFunc
}

// apiRoutes is the complete route table. It is a package-level value rather
// than a sequence of calls so tests can enumerate it: TestAPIMux_Routes
// drives every entry, and ownership_sweep_test.go fails the build of any
// route that has not been classified as entity-scoped or explicitly exempt.
var apiRoutes = []apiRoute{
	// Classes CRUD
	{"GET", "/api/classes", handleListClasses},
	{"POST", "/api/classes", handleCreateClass},
	{"PUT", "/api/classes/{id}", handleUpdateClass},
	{"DELETE", "/api/classes/{id}", handleDeleteClass},

	// Levels CRUD (Group-owned; write endpoints admin-gated in the handler)
	{"GET", "/api/levels", handleListLevels},
	{"POST", "/api/levels", handleCreateLevel},
	{"PUT", "/api/levels/{id}", handleUpdateLevel},
	{"DELETE", "/api/levels/{id}", handleDeleteLevel},

	// Students under class
	{"GET", "/api/classes/{id}/students", handleListStudents},
	{"POST", "/api/classes/{id}/students", handleCreateStudent},

	// Students by ID
	{"PUT", "/api/students/{id}", handleUpdateStudent},
	{"DELETE", "/api/students/{id}", handleDeleteStudent},

	// Aliases under student
	{"GET", "/api/students/{id}/aliases", handleListAliases},
	{"POST", "/api/students/{id}/aliases", handleAddAlias},
	{"DELETE", "/api/students/{id}/aliases/{aliasID}", handleRemoveAlias},

	// Notes under student
	{"GET", "/api/students/{id}/notes", handleListNotes},
	{"POST", "/api/students/{id}/notes", handleCreateNote},

	// Notes by ID
	{"GET", "/api/notes/{id}", handleGetNote},
	{"PUT", "/api/notes/{id}", handleUpdateNote},
	{"DELETE", "/api/notes/{id}", handleDeleteNote},

	// Reports
	{"POST", "/api/reports", handleGenerateReports},
	{"POST", "/api/reports/{id}/regenerate", handleRegenerateReport},
	{"GET", "/api/students/{id}/reports", handleListReports},
	{"GET", "/api/reports/{id}", handleGetReport},
	{"DELETE", "/api/reports/{id}", handleDeleteReport},

	// Voice note upload + Drive import
	{"POST", "/api/voice-notes/upload", handleUpload},
	{"POST", "/api/text-notes/upload", handleTextNotesUpload},
	{"POST", "/api/voice-notes/drive-import", handleDriveImport},

	// Google token (for Drive Picker)
	{"GET", "/api/google-token", handleGoogleToken},

	// Artifact feedback (explicit thumbs ratings)
	{"POST", "/api/feedback", handleSubmitFeedback},

	// Voice note jobs
	{"GET", "/api/voice-notes/jobs", handleJobList},
	{"POST", "/api/voice-notes/jobs/retry", handleJobRetry},
	{"POST", "/api/voice-notes/jobs/dismiss", handleJobDismiss},

	// Re-assemble a recording against a class the teacher picked. Registered
	// after the /jobs routes: ServeMux prefers the more specific pattern, so
	// "/api/voice-notes/jobs" is never captured as an {uploadId}.
	{"POST", "/api/voice-notes/{uploadId}/assemble", handleAssembleNotes},
	// File passages that reached nobody to a child the teacher picked.
	{"POST", "/api/voice-notes/{uploadId}/assign", handleAssignPassages},
	{"DELETE", "/api/voice-notes/{uploadId}/assign/{studentId}", handleUndoAssignment},
}

// key is the "METHOD /pattern" string ServeMux is given for the route.
func (rt apiRoute) key() string { return rt.Method + " " + rt.Pattern }

// newAPIMux registers every apiRoutes entry behind auth. Each pattern carries
// its method, so a known path with the wrong method falls to the "/api/"
// catch-all and answers the same JSON 404 as an unknown path — ServeMux's
// text/plain 405 never fires because the catch-all always matches.
func newAPIMux(auth func(http.Handler) http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	for _, rt := range apiRoutes {
		mux.Handle(rt.key(), auth(rt.Handler))
	}

	// Catch-all: unknown path, wrong method, or trailing segments. Registering
	// bare "/api" too stops ServeMux redirecting it to "/api/" with a 301.
	mux.HandleFunc("/api/", jsonNotFound)
	mux.HandleFunc("/api", jsonNotFound)

	return mux
}

// jsonNotFound is the router's own 404; handlers write theirs through writeError.
func jsonNotFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// idParam reads the named path wildcard (e.g. "id" from "/api/classes/{id}")
// as an int64. It reports false when the segment is absent or not an integer.
func idParam(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
