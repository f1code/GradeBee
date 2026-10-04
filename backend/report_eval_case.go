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
