package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// UploadRepo provides CRUD operations for the uploads table.
type UploadRepo struct{ db *sql.DB }

// Upload represents a row in the uploads table.
type Upload struct {
	ID          int64   `json:"id"`
	UserID      string  `json:"userId"`
	FileName    string  `json:"fileName"`
	FilePath    string  `json:"filePath"`
	ProcessedAt *string `json:"processedAt,omitempty"`
	CreatedAt   string  `json:"createdAt"`
}

// Create inserts a new upload record.
func (r *UploadRepo) Create(ctx context.Context, userID, fileName, filePath string) (Upload, error) {
	var u Upload
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO uploads (user_id, file_name, file_path) VALUES (?, ?, ?)
		RETURNING id, user_id, file_name, file_path, processed_at, created_at`,
		userID, fileName, filePath,
	).Scan(&u.ID, &u.UserID, &u.FileName, &u.FilePath, &u.ProcessedAt, &u.CreatedAt)
	if err != nil {
		return Upload{}, fmt.Errorf("create upload: %w", err)
	}
	return u, nil
}

// GetByID returns a single upload.
func (r *UploadRepo) GetByID(ctx context.Context, id int64) (Upload, error) {
	var u Upload
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, file_name, file_path, processed_at, created_at
		FROM uploads WHERE id = ?`, id,
	).Scan(&u.ID, &u.UserID, &u.FileName, &u.FilePath, &u.ProcessedAt, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return Upload{}, ErrNotFound
	}
	if err != nil {
		return Upload{}, fmt.Errorf("get upload: %w", err)
	}
	return u, nil
}

// MarkProcessed sets processed_at to now.
func (r *UploadRepo) MarkProcessed(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE uploads SET processed_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mark upload processed: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// ListStale returns uploads that were processed before the given ISO8601 cutoff.
// Used by the cleanup goroutine to find files safe to delete.
func (r *UploadRepo) ListStale(ctx context.Context, olderThan string) ([]Upload, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, file_name, file_path, processed_at, created_at
		FROM uploads
		WHERE processed_at IS NOT NULL AND processed_at < ?`, olderThan)
	if err != nil {
		return nil, fmt.Errorf("list stale uploads: %w", err)
	}
	defer rows.Close()

	var result []Upload
	for rows.Next() {
		var u Upload
		if err := rows.Scan(&u.ID, &u.UserID, &u.FileName, &u.FilePath, &u.ProcessedAt, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan upload: %w", err)
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

// Delete removes an upload record.
func (r *UploadRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM uploads WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete upload: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
