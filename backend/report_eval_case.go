package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ReportEvalCase is what report generation would feed BuildReportPrompt for
// an existing report, plus that report's HTML as the eval reference.
type ReportEvalCase struct {
	StudentName        string
	ClassName          string
	Notes              []Note
	ReportInstructions string
	Instructions       string
	ReferenceHTML      string
}

// LoadReportEvalCase resolves a report into eval inputs the way
// handleRegenerateReport does: same student, class name, Level, date range
// and ad-hoc instructions. Used by cmd/eval-cli.
func LoadReportEvalCase(ctx context.Context, db *sql.DB, studentID, reportID int64) (ReportEvalCase, error) {
	rpt, err := (&ReportRepo{db: db}).GetByID(ctx, reportID)
	if errors.Is(err, ErrNotFound) {
		return ReportEvalCase{}, fmt.Errorf("report %d not found", reportID)
	}
	if err != nil {
		return ReportEvalCase{}, err
	}
	if rpt.StudentID != studentID {
		return ReportEvalCase{}, fmt.Errorf("report %d belongs to student %d, not student %d", reportID, rpt.StudentID, studentID)
	}
	student, err := (&StudentRepo{db: db}).GetByID(ctx, studentID)
	if err != nil {
		return ReportEvalCase{}, fmt.Errorf("student %d: %w", studentID, err)
	}
	cls, err := (&ClassRepo{db: db}).GetByID(ctx, student.ClassID)
	if err != nil {
		return ReportEvalCase{}, fmt.Errorf("class %d: %w", student.ClassID, err)
	}
	// The app reads the Group from the request; a local CLI has none, so take
	// the Level's own.
	var groupID string
	if err := db.QueryRowContext(ctx, "SELECT group_id FROM levels WHERE id = ?", cls.LevelID).Scan(&groupID); err != nil {
		return ReportEvalCase{}, fmt.Errorf("level %d: %w", cls.LevelID, err)
	}
	lvl, err := (&LevelRepo{db: db}).GetByID(ctx, groupID, cls.LevelID)
	if err != nil {
		return ReportEvalCase{}, fmt.Errorf("level %d: %w", cls.LevelID, err)
	}
	if instructionsBlank(lvl.ReportInstructions) {
		return ReportEvalCase{}, fmt.Errorf("level %d (%s) has no Report Instructions", lvl.ID, lvl.Name)
	}
	notes, err := (&NoteRepo{db: db}).ListForStudents(ctx, []int64{studentID}, rpt.StartDate, rpt.EndDate)
	if err != nil {
		return ReportEvalCase{}, err
	}
	if len(notes) == 0 {
		return ReportEvalCase{}, fmt.Errorf("report %d: student %d has no notes between %s and %s", reportID, studentID, rpt.StartDate, rpt.EndDate)
	}
	var instructions string
	if rpt.Instructions != nil {
		instructions = *rpt.Instructions
	}
	return ReportEvalCase{
		StudentName:        student.Name,
		ClassName:          cls.Name,
		Notes:              notes,
		ReportInstructions: lvl.ReportInstructions,
		Instructions:       instructions,
		ReferenceHTML:      rpt.HTML,
	}, nil
}

// ReportEvalCandidate summarizes one of a student's reports for picking a
// curated eval case.
type ReportEvalCandidate struct {
	ReportID              int64
	StartDate             string
	EndDate               string
	CreatedAt             string
	LevelName             string
	HasReportInstructions bool
	HasInstructions       bool
	NoteCount             int
}

// ListReportEvalCandidates returns a student's reports, newest first. Used by
// cmd/eval-cli.
func ListReportEvalCandidates(ctx context.Context, db *sql.DB, studentID int64) ([]ReportEvalCandidate, error) {
	_, err := (&StudentRepo{db: db}).GetByID(ctx, studentID)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("student %d not found", studentID)
	}
	if err != nil {
		return nil, fmt.Errorf("student %d: %w", studentID, err)
	}
	// Same note filter as NoteRepo.ListForStudents.
	rows, err := db.QueryContext(ctx, `
		SELECT r.id, r.start_date, r.end_date, r.created_at, l.name, l.report_instructions,
		       COALESCE(r.instructions, ''),
		       (SELECT count(*) FROM notes n
		        WHERE n.student_id = r.student_id AND n.date BETWEEN r.start_date AND r.end_date)
		FROM reports r
		JOIN students s ON s.id = r.student_id
		JOIN classes c ON c.id = s.class_id
		JOIN levels l ON l.id = c.level_id
		WHERE r.student_id = ?
		ORDER BY r.created_at DESC, r.id DESC`, studentID)
	if err != nil {
		return nil, fmt.Errorf("list reports for student %d: %w", studentID, err)
	}
	defer rows.Close()
	var out []ReportEvalCandidate
	for rows.Next() {
		var c ReportEvalCandidate
		var reportInstructions, instructions string
		if err := rows.Scan(&c.ReportID, &c.StartDate, &c.EndDate, &c.CreatedAt, &c.LevelName, &reportInstructions, &instructions, &c.NoteCount); err != nil {
			return nil, err
		}
		c.HasReportInstructions = !instructionsBlank(reportInstructions)
		c.HasInstructions = !instructionsBlank(instructions)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("student %d has no reports", studentID)
	}
	return out, nil
}
