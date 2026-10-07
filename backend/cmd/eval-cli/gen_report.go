package main

import (
	"context"
	"database/sql"
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
var termStart = time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

type genReportOptions struct {
	studentID    int64
	start, end   string
	instructions string
}

// runGenReport generates a fresh report through production Generate, for use
// as an eval reference. It prints the new report id only: the HTML holds
// names, and the redacted dump-report is the way to read it.
func runGenReport(args []string) error {
	fls := flag.NewFlagSet("gen-report", flag.ContinueOnError)
	dbPath := fls.String("db", "", "SQLite DB to read inputs from and write the report to")
	var o genReportOptions
	fls.Int64Var(&o.studentID, "student", 0, "student id")
	fls.StringVar(&o.start, "start", "", "range start, YYYY-MM-DD, on or after "+termStart.Format(time.DateOnly))
	fls.StringVar(&o.end, "end", "", "range end, YYYY-MM-DD")
	fls.StringVar(&o.instructions, "instructions", "", "ad-hoc instructions (optional)")
	if err := fls.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || o.studentID <= 0 || o.start == "" || o.end == "" {
		return fmt.Errorf("usage: eval-cli gen-report -db PATH -student N -start YYYY-MM-DD -end YYYY-MM-DD [-instructions TEXT]")
	}
	if _, err := os.Stat(*dbPath); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	db, err := handler.OpenDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	gen, err := handler.NewReportGenerator(db)
	if err != nil {
		return err
	}
	return genReport(context.Background(), db, gen, os.Stdout, o)
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

func genReport(ctx context.Context, db *sql.DB, gen handler.ReportGenerator, w io.Writer, o genReportOptions) error {
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
	resp, err := gen.Generate(ctx, handler.GenerateReportRequest{ReportInputs: in})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "report %d: student %d, %s..%s, %d notes\n", resp.ReportID, o.studentID, o.start, o.end, len(in.Notes))
	return nil
}
