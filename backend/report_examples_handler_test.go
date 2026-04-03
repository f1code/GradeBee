package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
)

func TestUpdateReportExample_OK(t *testing.T) {
	stub := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: stub})

	body, err := json.Marshal(map[string]string{"name": "Updated", "content": "New content"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPut, "/report-examples/1", bytes.NewReader(body))
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result ReportExample
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "Updated" {
		t.Errorf("name = %q, want Updated", result.Name)
	}
	if result.Content != "New content" {
		t.Errorf("content = %q, want 'New content'", result.Content)
	}
}

func TestUpdateReportExample_MissingFields(t *testing.T) {
	stub := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: stub})

	body, err := json.Marshal(map[string]string{"name": "Only name"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPut, "/report-examples/1", bytes.NewReader(body))
	ctx := clerk.ContextWithSessionClaims(r.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user1"},
	})
	r = r.WithContext(ctx)

	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestUpdateReportExample_NoAuth(t *testing.T) {
	stub := &stubExampleStore{}
	withDeps(t, &mockDepsAll{exampleStore: stub})

	body, err := json.Marshal(map[string]string{"name": "x", "content": "y"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPut, "/report-examples/1", bytes.NewReader(body))

	rec := httptest.NewRecorder()
	handleUpdateReportExample(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestUploadExample_IncludesContent(t *testing.T) {
	store := &dbExampleStore{repo: &ReportExampleRepo{db: setupTestDB(t)}}
	ex, err := store.UploadExample(context.Background(), "user1", "My Report", "Some content here")
	if err != nil {
		t.Fatal(err)
	}
	if ex.Content != "Some content here" {
		t.Errorf("Content = %q, want 'Some content here'", ex.Content)
	}
}
