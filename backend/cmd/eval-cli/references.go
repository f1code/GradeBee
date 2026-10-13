package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	handler "github.com/nicogaller/gradebee/backend"
)

// A fresh case's reference lives in <refs>/<case id>/, outside the DB: a prod
// copy replacing data/gradebee.db drops and renumbers report rows. Files hold
// names and ad-hoc text, so the store sits under data/ (ADR 0005).
const (
	referenceHTMLFile = "reference.html"
	referenceMetaFile = "meta.json"
)

// referenceMeta records what a reference was written from. Notes pins the note
// set: gen-report-cases fails when the DB's notes in range no longer match.
type referenceMeta struct {
	StudentID    int64           `json:"student_id"`
	StartDate    string          `json:"start_date"`
	EndDate      string          `json:"end_date"`
	Instructions string          `json:"instructions"`
	Model        string          `json:"model"`
	CreatedAt    string          `json:"created_at"`
	Notes        []referenceNote `json:"notes"`
}

type referenceNote struct {
	ID        int64  `json:"id"`
	UpdatedAt string `json:"updated_at"`
}

func referenceNotes(notes []handler.Note) []referenceNote {
	out := make([]referenceNote, len(notes))
	for i, n := range notes {
		out[i] = referenceNote{ID: n.ID, UpdatedAt: n.UpdatedAt}
	}
	return out
}

func referenceDir(refsDir, caseID string) (string, error) {
	if !caseIDPattern.MatchString(caseID) {
		return "", fmt.Errorf("case id %q must match %s", caseID, caseIDPattern)
	}
	return filepath.Join(refsDir, caseID), nil
}

// checkNoReference returns the case's reference directory, refusing to
// overwrite a reviewed reference; call it before paying for a draft.
func checkNoReference(refsDir, caseID string) (string, error) {
	dir, err := referenceDir(refsDir, caseID)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("reference %s exists; delete it to write a new one", dir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return dir, nil
}

func writeReference(refsDir, caseID string, meta referenceMeta, html string) error {
	dir, err := checkNoReference(refsDir, caseID)
	if err != nil {
		return err
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, content := range map[string][]byte{
		referenceHTMLFile: []byte(html),
		referenceMetaFile: append(metaJSON, '\n'),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			os.RemoveAll(dir)
			return err
		}
	}
	return nil
}

func readReference(refsDir, caseID string) (referenceMeta, string, error) {
	dir, err := referenceDir(refsDir, caseID)
	if err != nil {
		return referenceMeta{}, "", err
	}
	raw, err := os.ReadFile(filepath.Join(dir, referenceMetaFile))
	if err != nil {
		return referenceMeta{}, "", fmt.Errorf("reference: %w", err)
	}
	var meta referenceMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return referenceMeta{}, "", fmt.Errorf("reference %s: %w", referenceMetaFile, err)
	}
	html, err := os.ReadFile(filepath.Join(dir, referenceHTMLFile))
	if err != nil {
		return referenceMeta{}, "", fmt.Errorf("reference: %w", err)
	}
	return meta, string(html), nil
}

// checkReferenceNotes fails when the notes in range differ from those the
// reference was written from: the judge would grade against other facts.
func checkReferenceNotes(want []referenceNote, got []handler.Note) error {
	wantAt := make(map[int64]string, len(want))
	for _, n := range want {
		wantAt[n.ID] = n.UpdatedAt
	}
	var missing, extra, edited []int64
	for _, n := range got {
		at, ok := wantAt[n.ID]
		switch {
		case !ok:
			extra = append(extra, n.ID)
		case at != n.UpdatedAt:
			edited = append(edited, n.ID)
		}
		delete(wantAt, n.ID)
	}
	for id := range wantAt {
		missing = append(missing, id)
	}
	var parts []string
	for _, p := range []struct {
		label string
		ids   []int64
	}{{"missing", missing}, {"extra", extra}, {"edited", edited}} {
		if len(p.ids) == 0 {
			continue
		}
		slices.Sort(p.ids)
		ids := make([]string, len(p.ids))
		for i, id := range p.ids {
			ids[i] = fmt.Sprint(id)
		}
		parts = append(parts, p.label+" "+strings.Join(ids, ", "))
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("notes in range differ from the reference's (%s); write a new reference", strings.Join(parts, "; "))
}
