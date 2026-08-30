// errors_http.go holds the HTTP error-response contract: the apiError type that
// carries a status through the call stack, and the writers that turn any error
// into a JSON body without leaking internal text to the caller.
package handler

import (
	"errors"
	"net/http"
)

// apiError is an error that carries an HTTP status code.
type apiError struct {
	Status  int
	Err     error
	Code    string            // machine-readable error code, e.g. "no_spreadsheet"
	Message string            // human-readable message
	Details map[string]string // optional structured details (forward-compat)
}

func (e *apiError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

// writeAPIError writes an apiError as a JSON response and logs it.
func writeAPIError(w http.ResponseWriter, r *http.Request, err *apiError) {
	log := getLogger()
	if r != nil {
		log = loggerFromRequest(r)
	}
	log.Warn("api error", "status", err.Status, "code", err.Code, "message", err.Message, "error", err.Err)

	type errorResponse struct {
		Error   string            `json:"error"`
		Message string            `json:"message,omitempty"`
		Details map[string]string `json:"details,omitempty"`
	}
	resp := errorResponse{}
	switch {
	case err.Code != "":
		resp.Error = err.Code
	case err.Err != nil:
		resp.Error = err.Err.Error()
	default:
		resp.Error = "unknown error"
	}
	if err.Message != "" {
		resp.Message = err.Message
	}
	if len(err.Details) > 0 {
		resp.Details = err.Details
	}
	writeJSON(w, err.Status, resp)
}

// internalErrorBody is the only text a 500 ever carries. Repo and driver errors
// name tables, files and hosts; none of that belongs in a response.
const internalErrorBody = "internal server error"

// writeInternalError answers 500 with a fixed body and logs err at Error, labelled
// with the calling handler's name so the record can be traced to its site without
// putting the client-controlled URL path into telemetry (see requireStudentOwnership).
// err.Error() never reaches the response.
func writeInternalError(w http.ResponseWriter, r *http.Request, err error) {
	writeInternalErrorFrom(w, r, err, callerAt(1))
}

// writeInternalErrorFrom is writeInternalError with the site label supplied by the
// caller, for helpers that sit between the handler and the write.
func writeInternalErrorFrom(w http.ResponseWriter, r *http.Request, err error, op string) {
	log := getLogger()
	method := ""
	if r != nil {
		log = loggerFromRequest(r)
		method = r.Method
	}
	log.Error("internal error", "op", op, "method", method, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": internalErrorBody})
}

// writeError maps an error to its response: an *apiError is written as-is,
// ErrNotFound (wrapped or bare) becomes a generic 404, and anything else is an
// internal error. Duplicate/conflict errors are deliberately not mapped here —
// each 409 site crafts a body the frontend reads by field, so those stay bespoke.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		writeAPIError(w, r, ae)
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	default:
		writeInternalErrorFrom(w, r, err, callerAt(1))
	}
}
