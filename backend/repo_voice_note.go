package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// VoiceNoteRepo provides CRUD operations for the voice_notes table.
type VoiceNoteRepo struct{ db *sql.DB }

// VoiceNote represents a row in the voice_notes table.
type VoiceNote struct {
	ID          int64   `json:"id"`
	UserID      string  `json:"userId"`
	FileName    string  `json:"fileName"`
	FilePath    string  `json:"filePath"`
	ProcessedAt *string `json:"processedAt,omitempty"`
	CreatedAt   string  `json:"createdAt"`
}

// Create inserts a new voice note record.
func (r *VoiceNoteRepo) Create(ctx context.Context, userID, fileName, filePath string) (VoiceNote, error) {
	var v VoiceNote
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO voice_notes (user_id, file_name, file_path) VALUES (?, ?, ?)
		RETURNING id, user_id, file_name, file_path, processed_at, created_at`,
		userID, fileName, filePath,
	).Scan(&v.ID, &v.UserID, &v.FileName, &v.FilePath, &v.ProcessedAt, &v.CreatedAt)
	if err != nil {
		return VoiceNote{}, fmt.Errorf("create voice note: %w", err)
	}
	return v, nil
}

// GetByID returns a single voice note.
func (r *VoiceNoteRepo) GetByID(ctx context.Context, id int64) (VoiceNote, error) {
	var v VoiceNote
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, file_name, file_path, processed_at, created_at
		FROM voice_notes WHERE id = ?`, id,
	).Scan(&v.ID, &v.UserID, &v.FileName, &v.FilePath, &v.ProcessedAt, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return VoiceNote{}, ErrNotFound
	}
	if err != nil {
		return VoiceNote{}, fmt.Errorf("get voice note: %w", err)
	}
	return v, nil
}

// MarkProcessed sets processed_at to now.
func (r *VoiceNoteRepo) MarkProcessed(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE voice_notes SET processed_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mark voice note processed: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// ListStale returns voice notes that were processed before the given ISO8601 cutoff.
// Used by the cleanup goroutine to find files safe to delete.
func (r *VoiceNoteRepo) ListStale(ctx context.Context, olderThan string) ([]VoiceNote, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, file_name, file_path, processed_at, created_at
		FROM voice_notes
		WHERE processed_at IS NOT NULL AND processed_at < ?`, olderThan)
	if err != nil {
		return nil, fmt.Errorf("list stale voice notes: %w", err)
	}
	defer rows.Close()

	var result []VoiceNote
	for rows.Next() {
		var v VoiceNote
		if err := rows.Scan(&v.ID, &v.UserID, &v.FileName, &v.FilePath, &v.ProcessedAt, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan voice note: %w", err)
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// Delete removes a voice note record.
func (r *VoiceNoteRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM voice_notes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete voice note: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
