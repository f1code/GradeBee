package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// ReportExampleRepo provides CRUD operations for the report_examples table.
type ReportExampleRepo struct{ db *sql.DB }

// DBReportExample represents a row in the report_examples table.
// Named DBReportExample to avoid conflict with the existing Drive-backed
// ReportExample type during the migration period.
type DBReportExample struct {
	ID        int64  `json:"id"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

// List returns all report examples for a user.
func (r *ReportExampleRepo) List(ctx context.Context, userID string) ([]DBReportExample, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, name, content, created_at
		FROM report_examples WHERE user_id = ?
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list report examples: %w", err)
	}
	defer rows.Close()

	var result []DBReportExample
	for rows.Next() {
		var e DBReportExample
		if err := rows.Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan report example: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// Create inserts a new report example.
func (r *ReportExampleRepo) Create(ctx context.Context, userID, name, content string) (DBReportExample, error) {
	var e DBReportExample
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO report_examples (user_id, name, content) VALUES (?, ?, ?)
		RETURNING id, user_id, name, content, created_at`,
		userID, name, content,
	).Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.CreatedAt)
	if err != nil {
		return DBReportExample{}, fmt.Errorf("create report example: %w", err)
	}
	return e, nil
}

// Update modifies the name and content of a report example owned by the user.
func (r *ReportExampleRepo) Update(ctx context.Context, userID string, id int64, name, content string) (DBReportExample, error) {
	var e DBReportExample
	err := r.db.QueryRowContext(ctx, `
		UPDATE report_examples SET name = ?, content = ?
		WHERE id = ? AND user_id = ?
		RETURNING id, user_id, name, content, created_at`,
		name, content, id, userID,
	).Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return DBReportExample{}, ErrNotFound
		}
		return DBReportExample{}, fmt.Errorf("update report example: %w", err)
	}
	return e, nil
}

// Delete removes a report example owned by the user.
func (r *ReportExampleRepo) Delete(ctx context.Context, userID string, id int64) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM report_examples WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return fmt.Errorf("delete report example: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
