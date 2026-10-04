// report_inputs.go resolves what report generation feeds the prompt:
// student → class → Level → Report Instructions gate → notes in range. The
// generate and regenerate handlers and cmd/eval-cli all go through it, so the
// eval sees the inputs production does.
package handler

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ReportInputs is everything BuildReportPrompt reads for one student, except
// regenerate feedback.
type ReportInputs struct {
	StudentID          int64
	StudentName        string
	ClassName          string
	LevelName          string
	Notes              []Note
	ReportInstructions string
	Instructions       string
	StartDate          string
	EndDate            string
}

// ErrLevelInstructionsMissing is returned when the student's Level has no
// Report Instructions.
type ErrLevelInstructionsMissing struct {
	LevelName string
}

func (e *ErrLevelInstructionsMissing) Error() string {
	return levelsMissingInstructionsError([]string{e.LevelName})
}

// levelsMissingInstructionsError formats the pre-flight refusal message
// naming every offending Level.
func levelsMissingInstructionsError(levelNames []string) string {
	quoted := make([]string, len(levelNames))
	for i, name := range levelNames {
		quoted[i] = fmt.Sprintf("'%s'", name)
	}
	noun, verb := "Level", "has"
	if len(quoted) > 1 {
		noun, verb = "Levels", "have"
	}
	return fmt.Sprintf("%s %s %s no report instructions — an admin must set them up", noun, strings.Join(quoted, ", "), verb)
}

// ReportInputResolver resolves ReportInputs from the DB.
type ReportInputResolver struct {
	students *StudentRepo
	classes  *ClassRepo
	levels   *LevelRepo
	notes    *NoteRepo
	reports  *ReportRepo
}

func NewReportInputResolver(db *sql.DB) *ReportInputResolver {
	return &ReportInputResolver{
		students: &StudentRepo{db: db},
		classes:  &ClassRepo{db: db},
		levels:   &LevelRepo{db: db},
		notes:    &NoteRepo{db: db},
		reports:  &ReportRepo{db: db},
	}
}

// ForGenerate resolves a first generation. Names, range and ad-hoc
// instructions come from the request.
func (r *ReportInputResolver) ForGenerate(ctx context.Context, groupID string, in ReportStudentInput, startDate, endDate, instructions string) (ReportInputs, error) {
	out, err := r.resolve(ctx, groupID, in.StudentID, startDate, endDate)
	if err != nil {
		return ReportInputs{}, err
	}
	out.StudentName = in.Name
	out.ClassName = in.ClassName
	out.Instructions = instructions
	return out, nil
}

// ForReport resolves an existing report: range and ad-hoc instructions from
// the report row, names from the DB. It returns the row too, for callers that
// need its CreatedAt.
func (r *ReportInputResolver) ForReport(ctx context.Context, groupID string, studentID, reportID int64) (ReportInputs, Report, error) {
	rpt, err := r.reports.GetByID(ctx, reportID)
	if err != nil {
		return ReportInputs{}, Report{}, fmt.Errorf("report %d: %w", reportID, err)
	}
	if rpt.StudentID != studentID {
		return ReportInputs{}, Report{}, fmt.Errorf("report %d belongs to student %d, not student %d", reportID, rpt.StudentID, studentID)
	}
	out, err := r.resolve(ctx, groupID, studentID, rpt.StartDate, rpt.EndDate)
	if err != nil {
		return ReportInputs{}, Report{}, err
	}
	if rpt.Instructions != nil {
		out.Instructions = *rpt.Instructions
	}
	return out, rpt, nil
}

func (r *ReportInputResolver) resolve(ctx context.Context, groupID string, studentID int64, startDate, endDate string) (ReportInputs, error) {
	student, err := r.students.GetByID(ctx, studentID)
	if err != nil {
		return ReportInputs{}, fmt.Errorf("student %d: %w", studentID, err)
	}
	cls, err := r.classes.GetByID(ctx, student.ClassID)
	if err != nil {
		return ReportInputs{}, fmt.Errorf("class %d: %w", student.ClassID, err)
	}
	// HTTP callers always pass the request's Group; only the local eval CLI,
	// which has no session, passes "" and trusts the class's own.
	if groupID == "" {
		groupID = cls.GroupID
	}
	lvl, err := r.levels.GetByID(ctx, groupID, cls.LevelID)
	if err != nil {
		return ReportInputs{}, fmt.Errorf("level %d: %w", cls.LevelID, err)
	}
	// Blank only blocks generation: the Levels admin endpoints accept an empty
	// save, since a Level exists before its instructions are written.
	if strings.TrimSpace(lvl.ReportInstructions) == "" {
		return ReportInputs{}, &ErrLevelInstructionsMissing{LevelName: lvl.Name}
	}
	notes, err := r.notes.ListForStudents(ctx, []int64{studentID}, startDate, endDate)
	if err != nil {
		return ReportInputs{}, fmt.Errorf("report: read notes: %w", err)
	}
	return ReportInputs{
		StudentID:          studentID,
		StudentName:        student.Name,
		ClassName:          cls.Name,
		LevelName:          lvl.Name,
		Notes:              notes,
		ReportInstructions: lvl.ReportInstructions,
		StartDate:          startDate,
		EndDate:            endDate,
	}, nil
}
