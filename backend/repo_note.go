package handler

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// NoteRepo provides CRUD operations for the notes table.
type NoteRepo struct{ db *sql.DB }

// Note represents a row in the notes table.
type Note struct {
	ID         int64   `json:"id"`
	StudentID  int64   `json:"studentId"`
	Date       string  `json:"date"`
	Summary    string  `json:"summary"`
	Transcript *string `json:"transcript,omitempty"`
	Source     string  `json:"source"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
}

// List returns all notes for a student, newest first.
func (r *NoteRepo) List(ctx context.Context, studentID int64) ([]Note, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, student_id, date, summary, transcript, source, created_at, updated_at
		FROM notes WHERE student_id = ?
		ORDER BY date DESC, created_at DESC`, studentID)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	var result []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.StudentID, &n.Date, &n.Summary, &n.Transcript, &n.Source, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// GetByID returns a single note.
func (r *NoteRepo) GetByID(ctx context.Context, id int64) (Note, error) {
	var n Note
	err := r.db.QueryRowContext(ctx, `
		SELECT id, student_id, date, summary, transcript, source, created_at, updated_at
		FROM notes WHERE id = ?`, id,
	).Scan(&n.ID, &n.StudentID, &n.Date, &n.Summary, &n.Transcript, &n.Source, &n.CreatedAt, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, fmt.Errorf("get note: %w", err)
	}
	return n, nil
}

// Create inserts a new note. The ID, CreatedAt, and UpdatedAt fields of the
// passed Note are populated on return.
func (r *NoteRepo) Create(ctx context.Context, n *Note) error {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO notes (student_id, date, summary, transcript, source)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at`,
		n.StudentID, n.Date, n.Summary, n.Transcript, n.Source,
	).Scan(&n.ID, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create note: %w", err)
	}
	return nil
}

// Update changes a note's summary and sets updated_at.
func (r *NoteRepo) Update(ctx context.Context, id int64, summary string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notes SET summary = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ?`, summary, id)
	if err != nil {
		return fmt.Errorf("update note: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes a note.
func (r *NoteRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM notes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// ListForStudents returns notes for multiple students within a date range.
// Used by report generation.
func (r *NoteRepo) ListForStudents(ctx context.Context, studentIDs []int64, startDate, endDate string) ([]Note, error) {
	if len(studentIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(studentIDs))
	args := make([]any, 0, len(studentIDs)+2)
	for i, id := range studentIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, startDate, endDate)

	query := fmt.Sprintf(`
		SELECT id, student_id, date, summary, transcript, source, created_at, updated_at
		FROM notes
		WHERE student_id IN (%s) AND date BETWEEN ? AND ?
		ORDER BY student_id, date DESC`,
		strings.Join(placeholders, ","))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list notes for students: %w", err)
	}
	defer rows.Close()

	var result []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.StudentID, &n.Date, &n.Summary, &n.Transcript, &n.Source, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		result = append(result, n)
	}
	return result, rows.Err()
}
