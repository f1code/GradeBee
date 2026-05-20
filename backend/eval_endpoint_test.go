package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireEvalToken_WrongToken(t *testing.T) {
	called := false
	h := RequireEvalToken("secret", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/eval/extract", http.NoBody)
	req.Header.Set("X-Eval-Token", "wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.False(t, called, "handler should not be called with wrong token")
}

func TestRequireEvalToken_CorrectToken(t *testing.T) {
	called := false
	h := RequireEvalToken("secret", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/eval/extract", http.NoBody)
	req.Header.Set("X-Eval-Token", "secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, called, "handler should be called with correct token")
}

func TestHandleEvalExtract_MissingTranscript(t *testing.T) {
	body, err := json.Marshal(evalExtractRequest{Transcript: "", Classes: nil})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/eval/extract", bytes.NewReader(body))
	req.Header.Set("X-Eval-Token", "test")
	rec := httptest.NewRecorder()

	HandleEvalExtract(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotEmpty(t, resp["error"])
}

func TestHandleEvalGenerateReport_MissingStudentName(t *testing.T) {
	body, err := json.Marshal(evalGenerateReportRequest{StudentName: ""})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/eval/generate-report", bytes.NewReader(body))
	req.Header.Set("X-Eval-Token", "test")
	rec := httptest.NewRecorder()

	HandleEvalGenerateReport(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotEmpty(t, resp["error"])
}
