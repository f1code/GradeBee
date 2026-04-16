package handler

import (
	"slices"
	"testing"
)

func TestReportExampleRepo_SetAndGetClassNames(t *testing.T) {
	db := setupTestDB(t)
	repo := &ReportExampleRepo{db: db}

	// Create an example first.
	e, err := repo.Create(t.Context(), "user1", "Test Example", "some content")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Set class names.
	classNames := []string{"Grade 4", "Grade 5"}
	if err := repo.SetClassNames(t.Context(), e.ID, classNames); err != nil {
		t.Fatalf("SetClassNames: %v", err)
	}

	// Get them back.
	got, err := repo.GetClassNames(t.Context(), e.ID)
	if err != nil {
		t.Fatalf("GetClassNames: %v", err)
	}
	if !slices.Equal(got, classNames) {
		t.Errorf("GetClassNames = %v, want %v", got, classNames)
	}

	// Replace with new set.
	if err := repo.SetClassNames(t.Context(), e.ID, []string{"Grade 6"}); err != nil {
		t.Fatalf("SetClassNames replace: %v", err)
	}
	got2, err := repo.GetClassNames(t.Context(), e.ID)
	if err != nil {
		t.Fatalf("GetClassNames after replace: %v", err)
	}
	if len(got2) != 1 || got2[0] != "Grade 6" {
		t.Errorf("after replace: got %v, want [Grade 6]", got2)
	}
}

func TestReportExampleRepo_ListReadyByClassName(t *testing.T) {
	db := setupTestDB(t)
	repo := &ReportExampleRepo{db: db}

	// Create examples.
	e1, err := repo.Create(t.Context(), "user1", "Example 1", "content 1")
	if err != nil {
		t.Fatalf("create e1: %v", err)
	}
	e2, err := repo.Create(t.Context(), "user1", "Example 2", "content 2")
	if err != nil {
		t.Fatalf("create e2: %v", err)
	}
	e3, err := repo.Create(t.Context(), "user1", "Example 3", "content 3")
	if err != nil {
		t.Fatalf("create e3: %v", err)
	}

	if err := repo.SetClassNames(t.Context(), e1.ID, []string{"Grade 4"}); err != nil {
		t.Fatalf("SetClassNames e1: %v", err)
	}
	if err := repo.SetClassNames(t.Context(), e2.ID, []string{"Grade 4", "Grade 5"}); err != nil {
		t.Fatalf("SetClassNames e2: %v", err)
	}
	if err := repo.SetClassNames(t.Context(), e3.ID, []string{"Grade 5"}); err != nil {
		t.Fatalf("SetClassNames e3: %v", err)
	}

	// Filter by Grade 4 — should return e1 and e2.
	results, err := repo.ListReadyByClassName(t.Context(), "user1", "Grade 4")
	if err != nil {
		t.Fatalf("ListReadyByClassName: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Grade 4: got %d results, want 2", len(results))
	}

	// Filter by Grade 5 — should return e2 and e3.
	results, err = repo.ListReadyByClassName(t.Context(), "user1", "Grade 5")
	if err != nil {
		t.Fatalf("ListReadyByClassName: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Grade 5: got %d results, want 2", len(results))
	}

	// Empty className — should return all 3.
	results, err = repo.ListReadyByClassName(t.Context(), "user1", "")
	if err != nil {
		t.Fatalf("ListReadyByClassName empty: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("empty className: got %d results, want 3", len(results))
	}

	// Different user — should return nothing.
	results, err = repo.ListReadyByClassName(t.Context(), "user2", "Grade 4")
	if err != nil {
		t.Fatalf("ListReadyByClassName user2: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("user2: got %d results, want 0", len(results))
	}
}
