package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	handler "github.com/nicogaller/gradebee/backend"
)

// termStart is the first day of the current term. Notes from before the
// summer break must not feed a reference report. Hard-coded until terms exist
// in the DB.
var termStart = time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)

type genReportOptions struct {
	refsDir      string
	caseID       string
	studentID    int64
	start, end   string
	instructions string
}

// runGenReport drafts a fresh report through production Generate's prompt
// and writes it as a case's eval reference. It prints no report text: the
// HTML holds names, and the redacted dump-report is the way to read it.
func runGenReport(args []string) error {
	fls := flag.NewFlagSet("gen-report", flag.ContinueOnError)
	dbPath := fls.String("db", "", "SQLite DB to read inputs from")
	var o genReportOptions
	fls.StringVar(&o.refsDir, "refs", "", "reference store; the reference goes to REFS/CASE")
	fls.StringVar(&o.caseID, "case", "", "manifest case id")
	fls.Int64Var(&o.studentID, "student", 0, "student id")
	fls.StringVar(&o.start, "start", "", "range start, YYYY-MM-DD, on or after "+termStart.Format(time.DateOnly))
	fls.StringVar(&o.end, "end", "", "range end, YYYY-MM-DD")
	fls.StringVar(&o.instructions, "instructions", "", "ad-hoc instructions (optional)")
	if err := fls.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || o.refsDir == "" || o.caseID == "" || o.studentID <= 0 || o.start == "" || o.end == "" {
		return fmt.Errorf("usage: eval-cli gen-report -db PATH -refs DIR -case ID -student N -start YYYY-MM-DD -end YYYY-MM-DD [-instructions TEXT]")
	}
	if _, err := os.Stat(*dbPath); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	db, err := handler.OpenDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	drafter, err := handler.NewReportDrafter(db)
	if err != nil {
		return err
	}
	return genReport(context.Background(), db, drafter, os.Stdout, o)
}

func checkGenReportRange(start, end string) error {
	s, err := time.Parse(time.DateOnly, start)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	e, err := time.Parse(time.DateOnly, end)
	if err != nil {
		return fmt.Errorf("end: %w", err)
	}
	if s.Before(termStart) {
		return fmt.Errorf("start %s is before the current term (%s): earlier notes must not feed a reference report", start, termStart.Format(time.DateOnly))
	}
	if e.Before(s) {
		return fmt.Errorf("end %s is before start %s", end, start)
	}
	return nil
}

func genReport(ctx context.Context, db *sql.DB, drafter handler.ReportDrafter, w io.Writer, o genReportOptions) error {
	if _, err := checkNoReference(o.refsDir, o.caseID); err != nil {
		return err
	}
	if err := checkGenReportRange(o.start, o.end); err != nil {
		return err
	}
	in, err := handler.NewReportInputResolver(db).ForGenerate(ctx, "", handler.ReportStudentInput{StudentID: o.studentID}, o.start, o.end, o.instructions)
	if err != nil {
		return err
	}
	if len(in.Notes) == 0 {
		return fmt.Errorf("student %d has no notes between %s and %s", o.studentID, o.start, o.end)
	}
	html, err := drafter.Draft(ctx, in)
	if err != nil {
		return err
	}
	meta := referenceMeta{
		StudentID:    o.studentID,
		StartDate:    o.start,
		EndDate:      o.end,
		Instructions: o.instructions,
		Model:        drafter.Model(),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		Notes:        referenceNotes(in.Notes),
	}
	if err := writeReference(o.refsDir, o.caseID, meta, html); err != nil {
		return err
	}
	draft, err := json.MarshalIndent(reportManifestEntry{
		ID:          o.caseID,
		Description: "WHAT THE CASE TESTS, NO NAMES",
		StudentID:   o.studentID,
	}, "    ", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "reference %s: student %d, %s..%s, %d notes\n", o.caseID, o.studentID, o.start, o.end, len(in.Notes))
	fmt.Fprintf(w, "\nAppend to \"reports\" in evals/fixtures.manifest.json, then run make eval-fixtures:\n\n    %s\n", draft)
	return nil
}
