package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"text/tabwriter"

	handler "github.com/nicogaller/gradebee/backend"
)

// runAddReportCase prints a student's candidate reports and a draft manifest
// entry. It writes to stdout only: the table names the Level, and the author
// copies the name-free entry into the manifest by hand.
func runAddReportCase(args []string) error {
	fls := flag.NewFlagSet("add-report-case", flag.ContinueOnError)
	dbPath := fls.String("db", "", "SQLite DB to read reports from")
	manifestPath := fls.String("manifest", "", "report case manifest (JSON), checked for duplicates")
	studentID := fls.Int64("student", 0, "student id")
	reportID := fls.Int64("report", 0, "report to draft an entry for (default: newest usable)")
	if err := fls.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *manifestPath == "" || *studentID <= 0 {
		return fmt.Errorf("usage: eval-cli add-report-case -db PATH -manifest PATH -student N [-report M]")
	}
	existing, err := readExistingManifest(*manifestPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(*dbPath); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	db, err := handler.OpenDB(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return addReportCase(context.Background(), db, os.Stdout, *studentID, *reportID, existing)
}

// readExistingManifest tolerates a missing manifest: the first case
// is drafted before any exist.
func readExistingManifest(path string) (reportManifest, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return reportManifest{}, nil
	}
	if err != nil {
		return reportManifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m reportManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return reportManifest{}, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return m, nil
}

func addReportCase(ctx context.Context, db *sql.DB, w io.Writer, studentID, reportID int64, existing reportManifest) error {
	candidates, err := handler.ListReportEvalCandidates(ctx, db, studentID)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REPORT\tRANGE\tCREATED\tLEVEL\tLEVEL INSTRUCTIONS\tAD-HOC INSTRUCTIONS\tNOTES")
	for _, c := range candidates {
		fmt.Fprintf(tw, "%d\t%s..%s\t%s\t%s\t%s\t%s\t%d\n",
			c.ReportID, c.StartDate, c.EndDate, c.CreatedAt, c.LevelName,
			yesNo(c.HasReportInstructions), yesNo(c.HasInstructions), c.NoteCount)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	inManifest := map[int64]string{}
	for _, e := range existing.Reports {
		inManifest[e.ReportID] = e.ID
	}
	if reportID == 0 {
		for _, c := range candidates {
			if _, dup := inManifest[c.ReportID]; !dup && c.HasReportInstructions && c.NoteCount > 0 {
				reportID = c.ReportID
				break
			}
		}
		if reportID == 0 {
			return fmt.Errorf("student %d has no report outside the manifest with Level Report Instructions and notes in range", studentID)
		}
	}
	if id, dup := inManifest[reportID]; dup {
		return fmt.Errorf("report %d is already in the manifest as %q", reportID, id)
	}
	// Loading the case the way gen-report-cases will is what guarantees the
	// pasted entry generates.
	c, err := handler.LoadReportEvalCase(ctx, db, studentID, reportID)
	if err != nil {
		return err
	}

	adHoc := "no ad-hoc instructions"
	if strings.TrimSpace(c.Instructions) != "" {
		adHoc = "ad-hoc instructions"
	}
	entry := reportManifestEntry{
		ID:          fmt.Sprintf("student%d_report%d", studentID, reportID),
		Description: fmt.Sprintf("Level Report Instructions, %d notes, %s", len(c.Notes), adHoc),
		StudentID:   studentID,
		ReportID:    reportID,
	}
	for _, e := range existing.Reports {
		if e.ID == entry.ID {
			return fmt.Errorf("id %q is already in the manifest", entry.ID)
		}
	}
	draft, err := json.MarshalIndent(entry, "    ", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\nDraft entry for report %d. Append it to \"reports\" in evals/fixtures.manifest.json,\n", reportID)
	fmt.Fprintln(w, "say in the description what the case tests (no names), then run make eval-fixtures.")
	fmt.Fprintf(w, "\n    %s\n", draft)
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
