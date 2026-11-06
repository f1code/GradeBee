package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ReportRepo provides CRUD operations for the reports table.
type ReportRepo struct{ db *sql.DB }

// Report represents a row in the reports table.
type Report struct {
	ID           int64   `json:"id"`
	StudentID    int64   `json:"studentId"`
	TermID       int64   `json:"-"`
	StartDate    string  `json:"startDate"`
	EndDate      string  `json:"endDate"`
	HTML         string  `json:"html,omitempty"`
	Instructions *string `json:"instructions,omitempty"`
	ModelVersion *string `json:"modelVersion,omitempty"`
	PromptHash   *string `json:"promptHash,omitempty"`
	// The Level text the prompt used; the eval replays it (task 185).
	LevelInstructions *string `json:"-"`
	CreatedAt         string  `json:"createdAt"`
}

// ReportSummary is the lightweight representation returned by list endpoints
// (no HTML body, no instructions, no studentId — list is already scoped).
type ReportSummary struct {
	ID        int64  `json:"id"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	CreatedAt string `json:"createdAt"`
}

// List returns reports for a student (without HTML body), newest first.
func (r *ReportRepo) List(ctx context.Context, studentID int64) ([]ReportSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, start_date, end_date, created_at
		FROM reports WHERE student_id = ?
		ORDER BY created_at DESC`, studentID)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()

	var result []ReportSummary
	for rows.Next() {
		var rpt ReportSummary
		if err := rows.Scan(&rpt.ID, &rpt.StartDate, &rpt.EndDate, &rpt.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan report: %w", err)
		}
		result = append(result, rpt)
	}
	return result, rows.Err()
}

// GetByID returns a single report including HTML body.
func (r *ReportRepo) GetByID(ctx context.Context, id int64) (Report, error) {
	var rpt Report
	err := r.db.QueryRowContext(ctx, `
		SELECT id, student_id, term_id, start_date, end_date, html, instructions, model_version, prompt_hash, level_instructions, created_at
		FROM reports WHERE id = ?`, id,
	).Scan(&rpt.ID, &rpt.StudentID, &rpt.TermID, &rpt.StartDate, &rpt.EndDate, &rpt.HTML, &rpt.Instructions, &rpt.ModelVersion, &rpt.PromptHash, &rpt.LevelInstructions, &rpt.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("get report: %w", err)
	}
	return rpt, nil
}

// Create inserts a new report.
func (r *ReportRepo) Create(ctx context.Context, rpt *Report) error {
	return insertReport(ctx, r.db, rpt)
}

// insertReport runs on a *sql.DB or a *sql.Tx.
func insertReport(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, rpt *Report) error {
	err := q.QueryRowContext(ctx, `
		INSERT INTO reports (student_id, term_id, start_date, end_date, html, instructions, model_version, prompt_hash, level_instructions)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, created_at`,
		rpt.StudentID, rpt.TermID, rpt.StartDate, rpt.EndDate, rpt.HTML, rpt.Instructions, rpt.ModelVersion, rpt.PromptHash, rpt.LevelInstructions,
	).Scan(&rpt.ID, &rpt.CreatedAt)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	return nil
}

// CreateRegenerated inserts the report a regenerate produced together with its
// feedback row, so no report exists without the text that produced it.
func (r *ReportRepo) CreateRegenerated(ctx context.Context, rpt *Report, fb ArtifactFeedback) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create regenerated report: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if err := insertReport(ctx, tx, rpt); err != nil {
		return err
	}
	fb.ResultID = &rpt.ID
	if _, err := insertFeedback(ctx, tx, fb); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create regenerated report: %w", err)
	}
	return nil
}

// Delete removes a report.
func (r *ReportRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM reports WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete report: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// EarlierTerms returns the student's newest Report in each Term of term's
// school year starting before it, oldest Term first. A year is the Group's
// Terms sharing archived_at: NULL while active, one rollover's timestamp after.
func (r *ReportRepo) EarlierTerms(ctx context.Context, studentID int64, term Term) ([]EarlierReport, error) {
	// MAX(id), not created_at: ids rise with each insert; created_at strings
	// with and without milliseconds sort wrong.
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.name, r.html FROM reports r JOIN terms t ON t.id = r.term_id
		WHERE r.id IN (SELECT MAX(id) FROM reports WHERE student_id = ? GROUP BY term_id)
		  AND t.group_id = ? AND t.archived_at IS ? AND t.start_date < ?
		ORDER BY t.start_date`, studentID, term.GroupID, term.ArchivedAt, term.StartDate)
	if err != nil {
		return nil, fmt.Errorf("list earlier reports: %w", err)
	}
	defer rows.Close()
	var out []EarlierReport
	for rows.Next() {
		var e EarlierReport
		if err := rows.Scan(&e.TermName, &e.HTML); err != nil {
			return nil, fmt.Errorf("scan earlier report: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
