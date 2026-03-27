package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// ClassRepo provides CRUD operations for the classes table.
type ClassRepo struct{ db *sql.DB }

// Class represents a row in the classes table.
type Class struct {
	ID        int64  `json:"id"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	CreatedAt string `json:"createdAt"`
}

// ClassWithCount is a Class with its student count.
type ClassWithCount struct {
	Class
	StudentCount int `json:"studentCount"`
}

// List returns all classes for a user, ordered by position then name,
// including the count of students in each class.
func (r *ClassRepo) List(ctx context.Context, userID string) ([]ClassWithCount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.id, c.user_id, c.name, c.position, c.created_at, COUNT(s.id)
		FROM classes c
		LEFT JOIN students s ON s.class_id = c.id
		WHERE c.user_id = ?
		GROUP BY c.id
		ORDER BY c.position, c.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list classes: %w", err)
	}
	defer rows.Close()

	var result []ClassWithCount
	for rows.Next() {
		var c ClassWithCount
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.Position, &c.CreatedAt, &c.StudentCount); err != nil {
			return nil, fmt.Errorf("scan class: %w", err)
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// Create inserts a new class for the user. Position is set to max+1.
func (r *ClassRepo) Create(ctx context.Context, userID, name string) (Class, error) {
	var c Class
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO classes (user_id, name, position)
		VALUES (?, ?, COALESCE((SELECT MAX(position) FROM classes WHERE user_id = ?), 0) + 1)
		RETURNING id, user_id, name, position, created_at`,
		userID, name, userID,
	).Scan(&c.ID, &c.UserID, &c.Name, &c.Position, &c.CreatedAt)
	if err != nil {
		if isDuplicateErr(err) {
			return Class{}, fmt.Errorf("create class %q: %w", name, ErrDuplicate)
		}
		return Class{}, fmt.Errorf("create class: %w", err)
	}
	return c, nil
}

// Rename updates the name of a class owned by the user.
func (r *ClassRepo) Rename(ctx context.Context, userID string, id int64, name string) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE classes SET name = ? WHERE id = ? AND user_id = ?",
		name, id, userID)
	if err != nil {
		if isDuplicateErr(err) {
			return fmt.Errorf("rename class: %w", ErrDuplicate)
		}
		return fmt.Errorf("rename class: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes a class owned by the user. Students and notes cascade.
func (r *ClassRepo) Delete(ctx context.Context, userID string, id int64) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM classes WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return fmt.Errorf("delete class: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
