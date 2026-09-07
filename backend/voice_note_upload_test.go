package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAllowedAudioType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"audio/mpeg", true},
		{"audio/wav", true},
		{"audio/mp4", true},
		{"audio/webm", true},
		{"video/webm", true},
		{"Audio/MPEG", true},
		{"video/mp4", false},
		{"application/pdf", false},
		{"text/plain", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isAllowedAudioType(tt.ct); got != tt.want {
			t.Errorf("isAllowedAudioType(%q) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}

// newUploadReq builds a POST /voice-notes/upload multipart request with Clerk
// auth. The part's Content-Type is written by hand because multipart's own
// CreateFormFile hardcodes application/octet-stream, which handleUpload rejects
// before it reaches any of the code under test.
func newUploadReq(t *testing.T, userID, fileName, contentType string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, fileName))
	h.Set("Content-Type", contentType)
	part, err := w.CreatePart(h)
	require.NoError(t, err)
	_, err = part.Write([]byte("fake audio bytes"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r := httptest.NewRequest(http.MethodPost, "/voice-notes/upload", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: userID},
	})
	return r.WithContext(ctx)
}

// recordingNames are the filenames every absence test runs. Each case is
// distinguished by its *expected* extension, not just its input: a well-formed
// name must yield an extension the request's MIME type would not have produced,
// or the table cannot tell "read from the filename" from "read from the MIME
// type" — and the filename branch is the one this task exists to constrain.
//
// The rejected cases are the ones a shape check waves through. `Dr. Manoe 12 sept`
// is what `filepath.Ext` mangles outright; `Dr.Manoe` and `opname.Manoe` are the
// harder half, where the trailing segment *is* the child's given name and looks
// exactly like an extension. Every case names the same child so one assertion
// covers them all.
var recordingNames = []struct {
	desc     string
	fileName string
	// wantExt is the extension the log record and disk path must carry. Empty
	// means the filename yields nothing usable and the MIME type decides, so
	// each test fills in its own request's fallback.
	wantExt string
}{
	{"extension we accept", "Manoe 12 sept.wav", ".wav"},
	{"extension we accept, uppercase", "Manoe 12 sept.WAV", ".wav"},
	{"final dot is a title prefix", "Dr. Manoe 12 sept", ""},
	{"final dot is a title prefix, no space", "Dr.Manoe", ""},
	{"trailing segment is a given name, not a format", "opname.Manoe", ""},
	{"no dot at all", "Manoe", ""},
}

func TestAudioExtension(t *testing.T) {
	for _, tc := range recordingNames {
		t.Run(tc.desc, func(t *testing.T) {
			want := tc.wantExt
			if want == "" {
				// The fallback for audio/mp4.
				want = ".m4a"
			}
			assert.Equal(t, want, audioExtension(tc.fileName, "audio/mp4"))
		})
	}

	// A format we do not accept is not trusted just because it is short and
	// alphanumeric, even when the MIME type gives us nothing better.
	assert.Equal(t, ".bin", audioExtension("Manoe.xyz", "application/octet-stream"))
}

// TestHandleUpload_OmitsFileName locks in ADR 0003 on the upload path: the
// completion record ships to Sentry, and a teacher's own filename routinely
// names a child ("Manoe 12 sept.m4a"). Asserting the record is still emitted
// keeps the absence assertion from passing just because the handler bailed out
// early, and asserting on the name *value* also catches a filename interpolated
// into the message. The response body is the teacher's copy and keeps the name.
func TestHandleUpload_OmitsFileName(t *testing.T) {
	for _, tc := range recordingNames {
		t.Run(tc.desc, func(t *testing.T) {
			old := serviceDeps
			t.Cleanup(func() { serviceDeps = old })

			// audio/mp4 falls back to .m4a, which no case's filename supplies,
			// so a filename-derived extension is distinguishable from a
			// MIME-derived one.
			wantExt := tc.wantExt
			if wantExt == "" {
				wantExt = ".m4a"
			}

			queue := newStubVoiceNoteQueue()
			voiceNotes := &VoiceNoteRepo{db: setupTestDB(t)}
			serviceDeps = &mockDepsAll{
				voiceNoteRepo:  voiceNotes,
				voiceNoteQueue: queue,
				uploadsDir:     t.TempDir(),
			}

			req := newUploadReq(t, "u1", tc.fileName, "audio/mp4")
			ctx, logs := captureLogs(req.Context())
			rec := httptest.NewRecorder()
			handleUpload(rec, req.WithContext(ctx))

			require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
			var resp UploadResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

			out := logs.String()
			require.Contains(t, out, "upload completed", "completion was not logged at all")
			// Lowercased: audioExtension lowercases what it returns, so a leaked
			// stem surfaces as ".manoe" and a case-sensitive check would miss it.
			assert.NotContains(t, strings.ToLower(out), "manoe", "upload leaked a recording's filename into the logs")

			done := logRecord(t, out, "upload completed")
			assert.Contains(t, done, `"file_ext":"`+wantExt+`"`, "completion should carry the extension that replaced the filename")
			// Closed with the trailing comma so "upload_id":1 cannot match "upload_id":12.
			assert.Contains(t, done, `"upload_id":`+strconv.FormatInt(resp.UploadID, 10)+`,`, "completion should carry the id the caller was handed")

			// The on-disk name is built by concatenating this same extension, and
			// that path is logged by the cleanup and transcription paths — and, via
			// *PathError, inside error strings. An unvalidated extension leaks the
			// stem there too, at Info, on every purge.
			require.Len(t, queue.published, 1, "expected 1 queued job")
			assert.NotContains(t, strings.ToLower(queue.published[0].FilePath), "manoe", "disk path leaked the filename's stem")
			assert.True(t, strings.HasSuffix(queue.published[0].FilePath, wantExt), "disk path should end in the validated extension, got %q", queue.published[0].FilePath)

			// The job carries the row's key, so every note the pipeline makes
			// can name this recording. The row is the key's home; this is the
			// one copy from it.
			row, err := voiceNotes.GetByID(t.Context(), resp.UploadID)
			require.NoError(t, err)
			require.NotEmpty(t, row.TraceID)
			assert.Equal(t, row.TraceID, queue.published[0].TraceID, "the job should carry the row's trace id")
			assert.Contains(t, done, `"trace_id":"`+row.TraceID+`"`, "completion should carry the trace id")

			// The teacher's copy is a separate string from the telemetry copy: they
			// recognise the upload by the name they gave it.
			assert.Contains(t, rec.Body.String(), tc.fileName, "response body should still carry the filename")
		})
	}
}
