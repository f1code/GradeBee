package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
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
	PurgedAt    *string `json:"purgedAt,omitempty"`
	// Transcript is written by the processor once transcription succeeds, before
	// extraction, so it exists whether or not any note is created. It lives as long
	// as the row: the retention cleanup deletes both together.
	Transcript *string `json:"transcript,omitempty"`
	// TraceID is the recording's key, a UUID minted at upload. Every note the
	// pipeline, assemble or assign makes from this recording carries a copy
	// (notes.trace_id), so a note can name its recording after the job is
	// gone and after this row is deleted by retention. Not the row id: the
	// table has no AUTOINCREMENT, so SQLite reuses the top id once the newest
	// row is deleted, and an old note could then claim a new recording.
	// The column is nullable and the reads COALESCE it to "", so a row that
	// somehow has none cannot fail a whole ListStale batch.
	TraceID   string `json:"traceId"`
	CreatedAt string `json:"createdAt"`
}

// Create inserts a new voice note record and mints its TraceID.
func (r *VoiceNoteRepo) Create(ctx context.Context, userID, fileName, filePath string) (VoiceNote, error) {
	var v VoiceNote
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO voice_notes (user_id, file_name, file_path, trace_id) VALUES (?, ?, ?, ?)
		RETURNING id, user_id, file_name, file_path, processed_at, purged_at, transcript, COALESCE(trace_id, ''), created_at`,
		userID, fileName, filePath, uuid.NewString(),
	).Scan(&v.ID, &v.UserID, &v.FileName, &v.FilePath, &v.ProcessedAt, &v.PurgedAt, &v.Transcript, &v.TraceID, &v.CreatedAt)
	if err != nil {
		return VoiceNote{}, fmt.Errorf("create voice note: %w", err)
	}
	return v, nil
}

// GetByID returns a single voice note.
func (r *VoiceNoteRepo) GetByID(ctx context.Context, id int64) (VoiceNote, error) {
	var v VoiceNote
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, file_name, file_path, processed_at, purged_at, transcript, COALESCE(trace_id, ''), created_at
		FROM voice_notes WHERE id = ?`, id,
	).Scan(&v.ID, &v.UserID, &v.FileName, &v.FilePath, &v.ProcessedAt, &v.PurgedAt, &v.Transcript, &v.TraceID, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return VoiceNote{}, ErrNotFound
	}
	if err != nil {
		return VoiceNote{}, fmt.Errorf("get voice note: %w", err)
	}
	return v, nil
}

// MarkProcessed sets processed_at to now, unless it is already set: the retention
// window counts from the first of "done" or "dismissed", and dismissing a finished
// job must not restart it — the privacy page promises the transcript copy is gone
// within the window.
func (r *VoiceNoteRepo) MarkProcessed(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE voice_notes
		SET processed_at = COALESCE(processed_at, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mark voice note processed: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// SetTranscript stores the transcript on the voice note. Called by the processor
// before extraction: for an audio job after the file has been deleted and purged_at
// set, so from then on the row is the only durable copy of what the teacher said;
// a pasted-text job or a retry after transcription arrives here directly.
func (r *VoiceNoteRepo) SetTranscript(ctx context.Context, id int64, transcript string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE voice_notes SET transcript = ? WHERE id = ?`, transcript, id)
	if err != nil {
		return fmt.Errorf("set voice note transcript: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// ListStale returns voice notes the cleanup goroutine may remove: processed before
// the given ISO8601 cutoff, or never processed and created before it. The second
// arm bounds the row — and the transcript on it — for a job that failed or was
// lost to a restart: nothing else ever sets processed_at on such a row, and its
// transcript would otherwise live forever. A failed job still in memory past the
// cutoff loses its row, so its retry fails permanently; at the default 7 days that
// is the intended trade. Transcript is left nil: the cleanup only needs the path and
// purge state, not every stale transcript.
func (r *VoiceNoteRepo) ListStale(ctx context.Context, olderThan string) ([]VoiceNote, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, file_name, file_path, processed_at, purged_at, COALESCE(trace_id, ''), created_at
		FROM voice_notes
		WHERE (processed_at IS NOT NULL AND processed_at < ?)
		   OR (processed_at IS NULL AND created_at < ?)`, olderThan, olderThan)
	if err != nil {
		return nil, fmt.Errorf("list stale voice notes: %w", err)
	}
	defer rows.Close()

	var result []VoiceNote
	for rows.Next() {
		var v VoiceNote
		if err := rows.Scan(&v.ID, &v.UserID, &v.FileName, &v.FilePath, &v.ProcessedAt, &v.PurgedAt, &v.TraceID, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan voice note: %w", err)
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// MarkPurged sets purged_at to now, indicating the audio file has been deleted.
func (r *VoiceNoteRepo) MarkPurged(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE voice_notes SET purged_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mark voice note purged: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes a voice note record.
func (r *VoiceNoteRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM voice_notes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete voice note: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
