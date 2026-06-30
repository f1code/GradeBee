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
	Status    string `json:"status"`
	FilePath  string `json:"-"`
	CreatedAt string `json:"createdAt"`
}

// List returns all report examples for a user.
func (r *ReportExampleRepo) List(ctx context.Context, userID string) ([]DBReportExample, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, name, content, status, file_path, created_at
		FROM report_examples WHERE user_id = ?
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list report examples: %w", err)
	}
	defer rows.Close()

	var result []DBReportExample
	for rows.Next() {
		var e DBReportExample
		if err := rows.Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.Status, &e.FilePath, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan report example: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// Create inserts a new report example with status 'ready'.
func (r *ReportExampleRepo) Create(ctx context.Context, userID, name, content string) (DBReportExample, error) {
	var e DBReportExample
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO report_examples (user_id, name, content, status) VALUES (?, ?, ?, 'ready')
		RETURNING id, user_id, name, content, status, file_path, created_at`,
		userID, name, content,
	).Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.Status, &e.FilePath, &e.CreatedAt)
	if err != nil {
		return DBReportExample{}, fmt.Errorf("create report example: %w", err)
	}
	return e, nil
}

// CreatePending inserts a new report example with status 'processing' and a file path.
func (r *ReportExampleRepo) CreatePending(ctx context.Context, userID, name, filePath string) (DBReportExample, error) {
	var e DBReportExample
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO report_examples (user_id, name, content, status, file_path) VALUES (?, ?, '', 'processing', ?)
		RETURNING id, user_id, name, content, status, file_path, created_at`,
		userID, name, filePath,
	).Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.Status, &e.FilePath, &e.CreatedAt)
	if err != nil {
		return DBReportExample{}, fmt.Errorf("create pending report example: %w", err)
	}
	return e, nil
}

// UpdateStatus sets the status and content of a report example (used by async extraction).
func (r *ReportExampleRepo) UpdateStatus(ctx context.Context, id int64, status, content string) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE report_examples SET status = ?, content = ? WHERE id = ?",
		status, content, id)
	if err != nil {
		return fmt.Errorf("update report example status: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Update modifies the name and content of a report example owned by the user.
func (r *ReportExampleRepo) Update(ctx context.Context, userID string, id int64, name, content string) (DBReportExample, error) {
	var e DBReportExample
	err := r.db.QueryRowContext(ctx, `
		UPDATE report_examples SET name = ?, content = ?
		WHERE id = ? AND user_id = ?
		RETURNING id, user_id, name, content, status, file_path, created_at`,
		name, content, id, userID,
	).Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.Status, &e.FilePath, &e.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return DBReportExample{}, ErrNotFound
		}
		return DBReportExample{}, fmt.Errorf("update report example: %w", err)
	}
	return e, nil
}

// GetFilePath returns the file_path for a report example (empty if none).
func (r *ReportExampleRepo) GetFilePath(ctx context.Context, userID string, id int64) (string, error) {
	var fp string
	err := r.db.QueryRowContext(ctx,
		"SELECT file_path FROM report_examples WHERE id = ? AND user_id = ?",
		id, userID).Scan(&fp)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get file path: %w", err)
	}
	return fp, nil
}

// SetLevelNames replaces the level names for a report example in a transaction.
func (r *ReportExampleRepo) SetLevelNames(ctx context.Context, exampleID int64, levelNames []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set level names: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback on error
	if _, err := tx.ExecContext(ctx, "DELETE FROM report_example_classes WHERE example_id = ?", exampleID); err != nil {
		return fmt.Errorf("set level names: delete: %w", err)
	}
	for _, ln := range levelNames {
		if _, err := tx.ExecContext(ctx, "INSERT INTO report_example_classes (example_id, level_name) VALUES (?, ?)", exampleID, ln); err != nil {
			return fmt.Errorf("set level names: insert %q: %w", ln, err)
		}
	}
	return tx.Commit()
}

// GetLevelNames returns the level names associated with a report example.
func (r *ReportExampleRepo) GetLevelNames(ctx context.Context, exampleID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT level_name FROM report_example_classes WHERE example_id = ? ORDER BY level_name",
		exampleID)
	if err != nil {
		return nil, fmt.Errorf("get level names: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var ln string
		if err := rows.Scan(&ln); err != nil {
			return nil, fmt.Errorf("get level names: scan: %w", err)
		}
		names = append(names, ln)
	}
	return names, rows.Err()
}

// ListReadyByLevelName returns 'ready' examples for a user filtered by level name.
// An empty levelName matches no example tags, so it returns no examples.
func (r *ReportExampleRepo) ListReadyByLevelName(ctx context.Context, userID, levelName string) ([]DBReportExample, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT re.id, re.user_id, re.name, re.content, re.status, re.file_path, re.created_at
		FROM report_examples re
		JOIN report_example_classes rec ON rec.example_id = re.id
		WHERE re.user_id = ? AND re.status = 'ready' AND rec.level_name = ?
		ORDER BY re.created_at DESC`, userID, levelName)
	if err != nil {
		return nil, fmt.Errorf("list ready by level name: %w", err)
	}
	defer rows.Close()
	var result []DBReportExample
	for rows.Next() {
		var e DBReportExample
		if err := rows.Scan(&e.ID, &e.UserID, &e.Name, &e.Content, &e.Status, &e.FilePath, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("list ready by level name: scan: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
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
