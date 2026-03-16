package handler

import (
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
