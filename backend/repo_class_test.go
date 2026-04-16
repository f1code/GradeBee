package handler

import (
	"errors"
	"testing"
)

func TestClassRepo_CreateWithClassNameGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	c, err := repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	if err != nil {
		t.Fatal(err)
	}
	if c.ClassName != "Mousy" {
		t.Errorf("ClassName = %q, want Mousy", c.ClassName)
	}
	if c.GroupName != "Thursday" {
		t.Errorf("GroupName = %q, want Thursday", c.GroupName)
	}
	if c.Name != "Mousy-Thursday" {
		t.Errorf("Name = %q, want Mousy-Thursday", c.Name)
	}
}

func TestClassRepo_CreateNoGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	c, err := repo.Create(t.Context(), "user1", "Lions", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Lions" {
		t.Errorf("Name = %q, want Lions", c.Name)
	}
	if c.GroupName != "" {
		t.Errorf("GroupName = %q, want empty", c.GroupName)
	}
}

func TestClassRepo_ListDistinctClassNames(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	for _, args := range [][2]string{
		{"Bears", "Monday"},
		{"Bears", "Tuesday"},
		{"Lions", ""},
		{"Tigers", "AM"},
	} {
		if _, err := repo.Create(t.Context(), "user1", args[0], args[1]); err != nil {
			t.Fatal(err)
		}
	}

	names, err := repo.ListDistinctClassNames(t.Context(), "user1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Bears", "Lions", "Tigers"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestClassRepo_DuplicateClassGroup(t *testing.T) {
	db := setupTestDB(t)
	repo := &ClassRepo{db: db}

	if _, err := repo.Create(t.Context(), "user1", "Mousy", "Thursday"); err != nil {
		t.Fatal(err)
	}
	_, err := repo.Create(t.Context(), "user1", "Mousy", "Thursday")
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("expected ErrDuplicate, got %v", err)
	}
}
