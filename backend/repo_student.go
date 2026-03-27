package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// StudentRepo provides CRUD operations for the students table.
type StudentRepo struct{ db *sql.DB }

// Student represents a row in the students table.
type Student struct {
	ID        int64  `json:"id"`
	ClassID   int64  `json:"classId"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

// List returns all students in a class, ordered by name.
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
		result = append(result, s)
	}
	return result, rows.Err()
}

// GetByID returns a single student by ID.
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
	return s, nil
}

// Create inserts a new student into a class.
func (r *StudentRepo) Create(ctx context.Context, classID int64, name string) (Student, error) {
	var s Student
	err := r.db.QueryRowContext(ctx, `
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
	return s, nil
}

// Rename updates a student's name.
func (r *StudentRepo) Rename(ctx context.Context, id int64, name string) error {
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

// Delete removes a student. Notes cascade via FK.
func (r *StudentRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM students WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete student: %w", err)
	}
	return rowsAffectedOrNotFound(res)
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
