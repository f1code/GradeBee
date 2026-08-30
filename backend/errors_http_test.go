package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// probeWriteError is a named frame between the test and writeError so the "op"
// label has a stable value to pin; a closure would be named funcN.
func probeWriteError(w http.ResponseWriter, r *http.Request, err error) {
	writeError(w, r, err)
}

// probeWriteInternalError does the same for the direct 500 writer.
func probeWriteInternalError(w http.ResponseWriter, r *http.Request, err error) {
	writeInternalError(w, r, err)
}

func errProbeReq(t *testing.T) (req *http.Request, logs func() string) {
	t.Helper()
	req = httptest.NewRequest(http.MethodDelete, "/api/probe", http.NoBody)
	ctx, buf := captureLogs(req.Context())
	return req.WithContext(ctx), buf.String
}

func TestWriteError(t *testing.T) {
	const secret = "dial tcp 10.0.0.7:5432: connection refused"

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
		// wantLogged is text the log must carry; wantOp the "op" label. Empty means
		// no Error-level record is expected.
		wantLogged string
		wantOp     string
	}{
		{
			name:       "apiError passes through with its status and code",
			err:        &apiError{Status: http.StatusConflict, Code: "alias_conflict", Message: "Alias already in use in this class."},
			wantStatus: http.StatusConflict,
			wantBody:   `{"error":"alias_conflict","message":"Alias already in use in this class."}` + "\n",
		},
		{
			name:       "wrapped apiError still passes through",
			err:        fmt.Errorf("lookup: %w", &apiError{Status: http.StatusBadGateway, Code: "upstream"}),
			wantStatus: http.StatusBadGateway,
			wantBody:   `{"error":"upstream"}` + "\n",
		},
		{
			name:       "ErrNotFound is a generic 404",
			err:        ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":"not found"}` + "\n",
		},
		{
			name:       "wrapped ErrNotFound is a generic 404",
			err:        fmt.Errorf("get note 7: %w", ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":"not found"}` + "\n",
		},
		{
			name:       "anything else is a fixed 500 with the cause logged",
			err:        errors.New(secret),
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal server error"}` + "\n",
			wantLogged: `"error":"` + secret + `"`,
			wantOp:     "probeWriteError",
		},
		{
			name:       "ErrDuplicate is not mapped here",
			err:        fmt.Errorf("create: %w", ErrDuplicate),
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal server error"}` + "\n",
			wantLogged: `"error":"create: duplicate"`,
			wantOp:     "probeWriteError",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, logs := errProbeReq(t)
			rec := httptest.NewRecorder()

			probeWriteError(rec, req, tc.err)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, tc.wantBody, rec.Body.String())
			out := logs()
			if tc.wantLogged == "" {
				assert.NotContains(t, out, `"level":"ERROR"`, "a mapped error is not an outage")
				return
			}
			// The absence in the body is only meaningful paired with presence in the log.
			assert.NotContains(t, rec.Body.String(), tc.err.Error(), "500 body must not carry the cause")
			require.Contains(t, out, `"level":"ERROR"`)
			assert.Contains(t, out, `"msg":"internal error"`)
			assert.Contains(t, out, tc.wantLogged)
			assert.Contains(t, out, `"op":"`+tc.wantOp+`"`)
			assert.Contains(t, out, `"method":"DELETE"`)
		})
	}
}

func TestWriteInternalError_LabelsCallingHandler(t *testing.T) {
	req, logs := errProbeReq(t)
	rec := httptest.NewRecorder()

	probeWriteInternalError(rec, req, errors.New("sql: database is closed"))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, `{"error":"internal server error"}`+"\n", rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "sql:")
	out := logs()
	assert.Contains(t, out, `"error":"sql: database is closed"`)
	assert.Contains(t, out, `"op":"probeWriteInternalError"`)
}

func TestWriteInternalError_NilRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	writeInternalError(rec, nil, errors.New("boom"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, `{"error":"internal server error"}`+"\n", rec.Body.String())
}
