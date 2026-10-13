// repo_feedback.go provides CRUD operations for the artifact_feedback table.
// Feedback rows are append-only (no UPDATE); each edit/regen/delete event
// creates a new row to preserve the full signal trajectory.
package handler

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ArtifactFeedbackRepo provides insert + read access to the artifact_feedback table.
type ArtifactFeedbackRepo struct{ db *sql.DB }

// ArtifactFeedback represents a single feedback event.
type ArtifactFeedback struct {
	ID            int64
	ArtifactType  string // 'report' | 'note'
	ArtifactID    int64
	Rating        string  // 'up' | 'down'
	Signal        string  // 'explicit' | 'regenerated' | 'edited' | 'deleted'
	Comment       *string // populated for 'explicit' and 'regenerated'
	PreviousValue *string // populated for 'edited' and 'deleted'
	ResultID      *int64  // 'regenerated': the report this regenerate produced; ArtifactID is its source
	UserID        string
	CreatedAt     time.Time
}

// Insert creates a new feedback row. Returns the new row ID.
func (r *ArtifactFeedbackRepo) Insert(ctx context.Context, f ArtifactFeedback) (int64, error) {
	return insertFeedback(ctx, r.db, f)
}

// insertFeedback runs on a *sql.DB or a *sql.Tx.
func insertFeedback(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, f ArtifactFeedback) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `
		INSERT INTO artifact_feedback
		  (artifact_type, artifact_id, rating, signal, comment, previous_value, user_id, result_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id`,
		f.ArtifactType, f.ArtifactID, f.Rating, f.Signal,
		f.Comment, f.PreviousValue, f.UserID, f.ResultID,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert artifact_feedback: %w", err)
	}
	return id, nil
}

// RegenerateChain returns the Teacher feedback of every regenerate that led to
// report id, oldest first; empty for a first draft. It walks the feedback rows,
// not the reports, so a deleted draft in the middle does not cut the chain.
func (r *ArtifactFeedbackRepo) RegenerateChain(ctx context.Context, id int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH RECURSIVE chain(artifact_id, comment, depth) AS (
			SELECT artifact_id, comment, 0 FROM artifact_feedback
			WHERE result_id = ? AND artifact_type = 'report' AND signal = 'regenerated'
			UNION ALL
			SELECT f.artifact_id, f.comment, c.depth + 1
			FROM artifact_feedback f JOIN chain c ON f.result_id = c.artifact_id
			WHERE f.artifact_type = 'report' AND f.signal = 'regenerated'
		)
		SELECT comment FROM chain WHERE comment IS NOT NULL ORDER BY depth DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("regenerate chain: %w", err)
	}
	defer rows.Close()

	var chain []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan regenerate chain: %w", err)
		}
		chain = append(chain, c)
	}
	return chain, rows.Err()
}

// ListByArtifact returns all feedback rows for a specific artifact, newest first.
func (r *ArtifactFeedbackRepo) ListByArtifact(ctx context.Context, artifactType string, artifactID int64) ([]ArtifactFeedback, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, artifact_type, artifact_id, rating, signal, comment, previous_value, result_id, user_id, created_at
		FROM artifact_feedback
		WHERE artifact_type = ? AND artifact_id = ?
		ORDER BY created_at DESC`, artifactType, artifactID)
	if err != nil {
		return nil, fmt.Errorf("list artifact_feedback by artifact: %w", err)
	}
	defer rows.Close()
	return scanFeedbackRows(rows)
}

// ListByUser returns feedback rows for a user since a given time, capped by limit.
func (r *ArtifactFeedbackRepo) ListByUser(ctx context.Context, userID string, since time.Time, limit int) ([]ArtifactFeedback, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, artifact_type, artifact_id, rating, signal, comment, previous_value, result_id, user_id, created_at
		FROM artifact_feedback
		WHERE user_id = ? AND created_at >= ?
		ORDER BY created_at DESC
		LIMIT ?`, userID, since.UTC().Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		return nil, fmt.Errorf("list artifact_feedback by user: %w", err)
	}
	defer rows.Close()
	return scanFeedbackRows(rows)
}

// CountSignals returns a map of "signal:rating" -> count since the given time.
// Useful for lightweight dashboard metrics.
func (r *ArtifactFeedbackRepo) CountSignals(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT signal, rating, COUNT(*) AS n
		FROM artifact_feedback
		WHERE created_at >= ?
		GROUP BY signal, rating`, since.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("count artifact_feedback signals: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var signal, rating string
		var n int
		if err := rows.Scan(&signal, &rating, &n); err != nil {
			return nil, fmt.Errorf("scan signal count: %w", err)
		}
		result[signal+":"+rating] = n
	}
	return result, rows.Err()
}

func scanFeedbackRows(rows *sql.Rows) ([]ArtifactFeedback, error) {
	var result []ArtifactFeedback
	for rows.Next() {
		var f ArtifactFeedback
		var createdAt string
		if err := rows.Scan(
			&f.ID, &f.ArtifactType, &f.ArtifactID, &f.Rating, &f.Signal,
			&f.Comment, &f.PreviousValue, &f.ResultID, &f.UserID, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan artifact_feedback: %w", err)
		}
		// SQLite datetime('now') returns "YYYY-MM-DD HH:MM:SS"; try both formats.
		t, err := time.Parse("2006-01-02 15:04:05", createdAt)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z", createdAt)
			if err != nil {
				return nil, fmt.Errorf("parse artifact_feedback created_at %q: %w", createdAt, err)
			}
		}
		f.CreatedAt = t
		result = append(result, f)
	}
	return result, rows.Err()
}
