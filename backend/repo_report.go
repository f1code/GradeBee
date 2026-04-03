package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// ReportRepo provides CRUD operations for the reports table.
type ReportRepo struct{ db *sql.DB }

// Report represents a row in the reports table.
type Report struct {
	ID           int64   `json:"id"`
	StudentID    int64   `json:"studentId"`
	StartDate    string  `json:"startDate"`
	EndDate      string  `json:"endDate"`
	HTML         string  `json:"html,omitempty"`
	Instructions *string `json:"instructions,omitempty"`
	CreatedAt    string  `json:"createdAt"`
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
		SELECT id, student_id, start_date, end_date, html, instructions, created_at
		FROM reports WHERE id = ?`, id,
	).Scan(&rpt.ID, &rpt.StudentID, &rpt.StartDate, &rpt.EndDate, &rpt.HTML, &rpt.Instructions, &rpt.CreatedAt)
	if err == sql.ErrNoRows {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("get report: %w", err)
	}
	return rpt, nil
}

// Create inserts a new report.
func (r *ReportRepo) Create(ctx context.Context, rpt *Report) error {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO reports (student_id, start_date, end_date, html, instructions)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, created_at`,
		rpt.StudentID, rpt.StartDate, rpt.EndDate, rpt.HTML, rpt.Instructions,
	).Scan(&rpt.ID, &rpt.CreatedAt)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
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
