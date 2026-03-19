package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseStudentRows(t *testing.T) {
	tests := []struct {
		name    string
		rows    [][]interface{}
		want    []classGroup
		wantErr bool
	}{
		{
			name: "valid data - correct grouping and alphabetical sorting",
			rows: [][]interface{}{
				{"class", "student"},
				{"5B", "Noah Davis"},
				{"5A", "Liam Smith"},
				{"5A", "Emma Johnson"},
				{"5B", "Olivia Brown"},
			},
			want: []classGroup{
				{Name: "5A", Students: []student{{Name: "Emma Johnson"}, {Name: "Liam Smith"}}},
				{Name: "5B", Students: []student{{Name: "Noah Davis"}, {Name: "Olivia Brown"}}},
			},
		},
		{
			name:    "empty rows - no data rows after header",
			rows:    [][]interface{}{{"class", "student"}},
			wantErr: true,
		},
		{
			name: "rows with missing class or student - skipped",
			rows: [][]interface{}{
				{"class", "student"},
				{"5A", "Emma"},
				{"", "Ghost"},
				{"5B", ""},
				{"5A", "Liam"},
			},
			want: []classGroup{
				{Name: "5A", Students: []student{{Name: "Emma"}, {Name: "Liam"}}},
			},
		},
		{
			name: "extra columns - ignored",
			rows: [][]interface{}{
				{"class", "student", "extra"},
				{"5A", "Emma", "ignored"},
				{"5A", "Liam", "also ignored"},
			},
			want: []classGroup{
				{Name: "5A", Students: []student{{Name: "Emma"}, {Name: "Liam"}}},
			},
		},
		{
			name: "whitespace in values - trimmed",
			rows: [][]interface{}{
				{"class", "student"},
				{"  5A  ", "  Emma  "},
				{"5A", "Liam"},
			},
			want: []classGroup{
				{Name: "5A", Students: []student{{Name: "Emma"}, {Name: "Liam"}}},
			},
		},
		{
			name: "single class",
			rows: [][]interface{}{
				{"class", "student"},
				{"5A", "Emma"},
				{"5A", "Liam"},
			},
			want: []classGroup{
				{Name: "5A", Students: []student{{Name: "Emma"}, {Name: "Liam"}}},
			},
		},
		{
			name:    "header only - error",
			rows:    [][]interface{}{{"class", "student"}},
			wantErr: true,
		},
		{
			name: "duplicate class names in non-contiguous rows - merged",
			rows: [][]interface{}{
				{"class", "student"},
				{"5A", "Emma"},
				{"5B", "Noah"},
				{"5A", "Liam"},
			},
			want: []classGroup{
				{Name: "5A", Students: []student{{Name: "Emma"}, {Name: "Liam"}}},
				{Name: "5B", Students: []student{{Name: "Noah"}}},
			},
		},
		{
			name:    "nil rows",
			rows:    nil,
			wantErr: true,
		},
		{
			name:    "empty rows",
			rows:    [][]interface{}{},
			wantErr: true,
		},
		{
			name: "single row - header only",
			rows: [][]interface{}{
				{"class", "student"},
			},
			wantErr: true,
		},
		{
			name: "single column rows - skipped",
			rows: [][]interface{}{
				{"class", "student"},
				{"5A"},
				{"5A", "Emma"},
			},
			want: []classGroup{
				{Name: "5A", Students: []student{{Name: "Emma"}}},
			},
		},
		{
			name: "numeric values converted to string",
			rows: [][]interface{}{
				{"class", "student"},
				{5, "Emma"},
				{"5A", "Liam"},
			},
			want: []classGroup{
				{Name: "5", Students: []student{{Name: "Emma"}}},
				{Name: "5A", Students: []student{{Name: "Liam"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStudentRows(tt.rows)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseStudentRows() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseStudentRows() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleGetStudents_HappyPath(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	serviceDeps = &mockDepsAll{
		roster: &stubRoster{
			students: []classGroup{
				{Name: "5A", Students: []student{{Name: "Emma"}, {Name: "Liam"}}},
				{Name: "5B", Students: []student{{Name: "Noah"}}},
			},
			url: "https://docs.google.com/spreadsheets/d/abc/edit",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/students", http.NoBody)
	rec := httptest.NewRecorder()
	handleGetStudents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	var resp studentsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SpreadsheetURL != "https://docs.google.com/spreadsheets/d/abc/edit" {
		t.Errorf("spreadsheetUrl = %q", resp.SpreadsheetURL)
	}
	if len(resp.Classes) != 2 {
		t.Errorf("got %d classes, want 2", len(resp.Classes))
	}
}

func TestHandleGetStudents_RosterAPIError(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	serviceDeps = &mockDepsAll{
		rosterErr: &apiError{Status: 404, Code: "no_spreadsheet", Message: "not found"},
	}

	req := httptest.NewRequest(http.MethodGet, "/students", http.NoBody)
	rec := httptest.NewRecorder()
	handleGetStudents(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("got status %d, want 404", rec.Code)
	}
}

func TestHandleGetStudents_RosterGenericError(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	serviceDeps = &mockDepsAll{
		rosterErr: fmt.Errorf("something broke"),
	}

	req := httptest.NewRequest(http.MethodGet, "/students", http.NoBody)
	rec := httptest.NewRecorder()
	handleGetStudents(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("got status %d, want 500", rec.Code)
	}
}

func TestHandleGetStudents_EmptySpreadsheet(t *testing.T) {
	origDeps := serviceDeps
	defer func() { serviceDeps = origDeps }()

	serviceDeps = &mockDepsAll{
		roster: &stubRoster{
			studentsErr: fmt.Errorf("No students found"),
			url:         "https://docs.google.com/spreadsheets/d/abc/edit",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/students", http.NoBody)
	rec := httptest.NewRecorder()
	handleGetStudents(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got status %d, want 422", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "empty_spreadsheet" {
		t.Errorf("error = %q, want empty_spreadsheet", body["error"])
	}
	if body["spreadsheetUrl"] != "https://docs.google.com/spreadsheets/d/abc/edit" {
		t.Errorf("spreadsheetUrl = %q", body["spreadsheetUrl"])
	}
}
