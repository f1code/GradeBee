package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// StudentRepo provides CRUD operations for the students table.
type StudentRepo struct{ db *sql.DB }

// Student represents a row in the students table.
type Student struct {
	ID        int64    `json:"id"`
	ClassID   int64    `json:"classId"`
	Name      string   `json:"name"`
	CreatedAt string   `json:"createdAt"`
	Aliases   []string `json:"aliases"`
}

// StudentAlias represents a row in the student_aliases table.
type StudentAlias struct {
	ID        int64  `json:"id"`
	StudentID int64  `json:"studentId"`
	ClassID   int64  `json:"classId"`
	Alias     string `json:"alias"`
	CreatedAt string `json:"createdAt"`
}

// List returns all students in a class, ordered by name.
// Aliases are NOT loaded here for performance; use ListWithAliases when needed.
func (r *StudentRepo) List(ctx context.Context, classID int64) ([]Student, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, class_id, name, created_at FROM students WHERE class_id = ? ORDER BY name",
		classID)
	if err != nil {
		return nil, fmt.Errorf("list students: %w", err)
	}
	defer rows.Close()

	var result []Student
	for rows.Next() {
		var s Student
		if err := rows.Scan(&s.ID, &s.ClassID, &s.Name, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan student: %w", err)
		}
		s.Aliases = []string{}
		result = append(result, s)
	}
	return result, rows.Err()
}

// ListWithAliases returns all students in a class with their aliases loaded.
func (r *StudentRepo) ListWithAliases(ctx context.Context, classID int64) ([]Student, error) {
	students, err := r.List(ctx, classID)
	if err != nil {
		return nil, err
	}
	for i := range students {
		aliases, err := r.ListAliases(ctx, students[i].ID)
		if err != nil {
			return nil, err
		}
		names := make([]string, len(aliases))
		for j, a := range aliases {
			names[j] = a.Alias
		}
		students[i].Aliases = names
	}
	return students, nil
}

// GetByID returns a single student by ID (aliases not loaded).
func (r *StudentRepo) GetByID(ctx context.Context, id int64) (Student, error) {
	var s Student
	err := r.db.QueryRowContext(ctx,
		"SELECT id, class_id, name, created_at FROM students WHERE id = ?", id,
	).Scan(&s.ID, &s.ClassID, &s.Name, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return Student{}, ErrNotFound
	}
	if err != nil {
		return Student{}, fmt.Errorf("get student: %w", err)
	}
	s.Aliases = []string{}
	return s, nil
}

// Create inserts a new student into a class.
func (r *StudentRepo) Create(ctx context.Context, classID int64, name string) (Student, error) {
	// Check collision with existing aliases in the same class.
	var collision int
	err := r.db.QueryRowContext(ctx,
		"SELECT 1 FROM student_aliases WHERE class_id = ? AND alias = ? COLLATE NOCASE LIMIT 1",
		classID, name).Scan(&collision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Student{}, fmt.Errorf("create student: check alias collision: %w", err)
	}
	if err == nil {
		return Student{}, fmt.Errorf("create student %q: %w", name, ErrDuplicate)
	}

	var s Student
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO students (class_id, name) VALUES (?, ?)
		RETURNING id, class_id, name, created_at`,
		classID, name,
	).Scan(&s.ID, &s.ClassID, &s.Name, &s.CreatedAt)
	if err != nil {
		if isDuplicateErr(err) {
			return Student{}, fmt.Errorf("create student %q: %w", name, ErrDuplicate)
		}
		return Student{}, fmt.Errorf("create student: %w", err)
	}
	s.Aliases = []string{}
	return s, nil
}

// Rename updates a student's name.
func (r *StudentRepo) Rename(ctx context.Context, id int64, name string) error {
	s, err := r.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("rename student: %w", err)
	}

	// Check collision with existing aliases in the same class (excluding this student's own aliases).
	var collision int
	err = r.db.QueryRowContext(ctx,
		"SELECT 1 FROM student_aliases WHERE class_id = ? AND alias = ? COLLATE NOCASE AND student_id != ? LIMIT 1",
		s.ClassID, name, id).Scan(&collision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("rename student: check alias collision: %w", err)
	}
	if err == nil {
		return fmt.Errorf("rename student: %w", ErrDuplicate)
	}

	res, err := r.db.ExecContext(ctx,
		"UPDATE students SET name = ? WHERE id = ?", name, id)
	if err != nil {
		if isDuplicateErr(err) {
			return fmt.Errorf("rename student: %w", ErrDuplicate)
		}
		return fmt.Errorf("rename student: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Move transfers a student to a different class.
func (r *StudentRepo) Move(ctx context.Context, id, newClassID int64) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE students SET class_id = ? WHERE id = ?", newClassID, id)
	if err != nil {
		if isDuplicateErr(err) {
			return fmt.Errorf("move student: %w", ErrDuplicate)
		}
		return fmt.Errorf("move student: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes a student. Notes and aliases cascade via FK.
func (r *StudentRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM students WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete student: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// FindByNameAndClass looks up a student by canonical name OR alias (case-insensitive)
// for a given class name and user. Returns the student ID or ErrNotFound.
func (r *StudentRepo) FindByNameAndClass(ctx context.Context, name, className, userID string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id FROM students s
		JOIN classes c ON s.class_id = c.id
		LEFT JOIN student_aliases sa ON sa.student_id = s.id
		WHERE c.name = ? AND c.user_id = ?
		  AND (s.name = ? COLLATE NOCASE OR sa.alias = ? COLLATE NOCASE)
		LIMIT 1`,
		className, userID, name, name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("find student by name and class: %w", err)
	}
	return id, nil
}

// BelongsToUser checks if a student belongs to a class owned by the given user.
func (r *StudentRepo) BelongsToUser(ctx context.Context, studentID int64, userID string) (bool, error) {
	var exists int
	err := r.db.QueryRowContext(ctx, `
		SELECT 1 FROM students s
		JOIN classes c ON s.class_id = c.id
		WHERE s.id = ? AND c.user_id = ?`,
		studentID, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check student ownership: %w", err)
	}
	return true, nil
}

// --- Alias methods ---

// AddAlias adds an alias for a student. Returns ErrDuplicate if the alias
// already exists in the same class (case-insensitive, checked across both
// students.name and student_aliases.alias).
func (r *StudentRepo) AddAlias(ctx context.Context, studentID int64, alias string) (StudentAlias, error) {
	// Fetch the class_id for this student.
	s, err := r.GetByID(ctx, studentID)
	if err != nil {
		return StudentAlias{}, fmt.Errorf("add alias: get student: %w", err)
	}

	// Check collision with canonical names in the same class.
	var collision int
	err = r.db.QueryRowContext(ctx,
		"SELECT 1 FROM students WHERE class_id = ? AND name = ? COLLATE NOCASE AND id != ? LIMIT 1",
		s.ClassID, alias, studentID).Scan(&collision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return StudentAlias{}, fmt.Errorf("add alias: check name collision: %w", err)
	}
	if err == nil {
		return StudentAlias{}, fmt.Errorf("add alias %q: %w", alias, ErrDuplicate)
	}

	var a StudentAlias
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO student_aliases (student_id, class_id, alias)
		VALUES (?, ?, ?)
		RETURNING id, student_id, class_id, alias, created_at`,
		studentID, s.ClassID, alias,
	).Scan(&a.ID, &a.StudentID, &a.ClassID, &a.Alias, &a.CreatedAt)
	if err != nil {
		if isDuplicateErr(err) {
			return StudentAlias{}, fmt.Errorf("add alias %q: %w", alias, ErrDuplicate)
		}
		return StudentAlias{}, fmt.Errorf("add alias: %w", err)
	}
	return a, nil
}

// RemoveAlias deletes an alias by ID, verifying it belongs to studentID. Returns ErrNotFound if not found.
func (r *StudentRepo) RemoveAlias(ctx context.Context, studentID, aliasID int64) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM student_aliases WHERE id = ? AND student_id = ?", aliasID, studentID)
	if err != nil {
		return fmt.Errorf("remove alias: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// ListAliases returns all aliases for a student.
func (r *StudentRepo) ListAliases(ctx context.Context, studentID int64) ([]StudentAlias, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, student_id, class_id, alias, created_at FROM student_aliases WHERE student_id = ? ORDER BY alias",
		studentID)
	if err != nil {
		return nil, fmt.Errorf("list aliases: %w", err)
	}
	defer rows.Close()

	var result []StudentAlias
	for rows.Next() {
		var a StudentAlias
		if err := rows.Scan(&a.ID, &a.StudentID, &a.ClassID, &a.Alias, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alias: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
