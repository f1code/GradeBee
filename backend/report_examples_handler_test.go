package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateReportExample_OK(t *testing.T) {
	stub := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: stub})

	body, err := json.Marshal(map[string]string{"name": "Updated", "content": "New content"})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPut, "/report-examples/1", bytes.NewReader(body))
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, r)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

	var result ReportExample
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	assert.Equal(t, "Updated", result.Name)
	assert.Equal(t, "New content", result.Content)
}

func TestUpdateReportExample_MissingFields(t *testing.T) {
	stub := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: stub})

	body, err := json.Marshal(map[string]string{"name": "Only name"})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPut, "/report-examples/1", bytes.NewReader(body))
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, r)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateReportExample_NoAuth(t *testing.T) {
	stub := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: stub})

	body, err := json.Marshal(map[string]string{"name": "x", "content": "y"})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPut, "/report-examples/1", bytes.NewReader(body))

	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, r)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestUploadExample_IncludesContent(t *testing.T) {
	store := &dbExampleStore{repo: &ReportExampleRepo{db: setupTestDB(t)}}
	ex, err := store.UploadExample(context.Background(), "user1", "My Report", "Some content here", nil)
	require.NoError(t, err)
	assert.Equal(t, "Some content here", ex.Content)
}

func TestUploadExample_PDFDispatchesAsync(t *testing.T) {
	queue := newStubExtractionQueue()
	store := &stubExampleStore{}
	tmpDir := t.TempDir()
	withDeps(t, &mockDepsAll{
		exampleStore:    store,
		extractionQueue: queue,
		uploadsDir:      tmpDir,
	})

	// Build multipart form with a PDF file.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "report.pdf")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake pdf data"))
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("classNames", `["Grade 4"]`))
	writer.Close()

	r := httptest.NewRequest(http.MethodPost, "/report-examples", &buf)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUploadReportExample(rec, r)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())

	var result ReportExample
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	assert.Equal(t, "processing", result.Status)
	require.Len(t, queue.published, 1)
	assert.Equal(t, "report.pdf", queue.published[0].FileName)
}

func TestUploadExample_TextFileStoresDirect(t *testing.T) {
	store := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: store})

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "notes.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("Some report card text"))
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("classNames", `["Grade 4"]`))
	writer.Close()

	r := httptest.NewRequest(http.MethodPost, "/report-examples", &buf)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUploadReportExample(rec, r)

	require.Equal(t, http.StatusOK, rec.Code, "body = %s", rec.Body.String())
	assert.Equal(t, "Some report card text", store.uploadedContent)
}
