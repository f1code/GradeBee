package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TermRepo reads a Group's Terms and their per-Level Report Instructions.
// Term reads are scoped by group_id; Instructions trusts ids the caller
// already scoped.
type TermRepo struct{ db *sql.DB }

// Term represents a row in the terms table.
type Term struct {
	ID         int64   `json:"id"`
	GroupID    string  `json:"groupId"`
	Name       string  `json:"name"`
	StartDate  string  `json:"startDate"`
	EndDate    string  `json:"endDate"`
	ArchivedAt *string `json:"archivedAt,omitempty"`
}

const termColumns = "id, group_id, name, start_date, end_date, archived_at"

// currentTermSQL selects the id of the latest active Term already started in
// group, the Term the Levels screen edits.
func currentTermSQL(group string) string {
	return `SELECT id FROM terms
	WHERE group_id = ` + group + ` AND archived_at IS NULL AND start_date <= date('now')
	ORDER BY start_date DESC LIMIT 1`
}

func scanTerm(row interface{ Scan(...any) error }) (Term, error) {
	var t Term
	err := row.Scan(&t.ID, &t.GroupID, &t.Name, &t.StartDate, &t.EndDate, &t.ArchivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Term{}, ErrNotFound
	}
	if err != nil {
		return Term{}, fmt.Errorf("scan term: %w", err)
	}
	return t, nil
}

// GetByID returns ErrNotFound if the Term belongs to another Group.
func (r *TermRepo) GetByID(ctx context.Context, groupID string, id int64) (Term, error) {
	return scanTerm(r.db.QueryRowContext(ctx,
		"SELECT "+termColumns+" FROM terms WHERE id = ? AND group_id = ?", id, groupID))
}

// Current returns ErrNoCurrentTerm when no active Term has started.
func (r *TermRepo) Current(ctx context.Context, groupID string) (Term, error) {
	t, err := scanTerm(r.db.QueryRowContext(ctx,
		"SELECT "+termColumns+" FROM terms WHERE id = ("+currentTermSQL("?")+")", groupID))
	if errors.Is(err, ErrNotFound) {
		return Term{}, ErrNoCurrentTerm
	}
	return t, err
}

// Covering returns the active Term containing date, or ErrNotFound.
func (r *TermRepo) Covering(ctx context.Context, groupID, date string) (Term, error) {
	return scanTerm(r.db.QueryRowContext(ctx,
		"SELECT "+termColumns+` FROM terms
		 WHERE group_id = ? AND archived_at IS NULL AND start_date <= ? AND end_date >= ?
		 ORDER BY start_date DESC LIMIT 1`, groupID, date, date))
}

// Instructions returns the Level's text for the Term, "" when unset.
func (r *TermRepo) Instructions(ctx context.Context, levelID, termID int64) (string, error) {
	var text string
	err := r.db.QueryRowContext(ctx,
		"SELECT text FROM report_instructions WHERE level_id = ? AND term_id = ?", levelID, termID).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get report instructions: %w", err)
	}
	return text, nil
}

// ListActive returns the Group's active Terms by start date.
func (r *TermRepo) ListActive(ctx context.Context, groupID string) ([]Term, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT "+termColumns+" FROM terms WHERE group_id = ? AND archived_at IS NULL ORDER BY start_date", groupID)
	if err != nil {
		return nil, fmt.Errorf("list terms: %w", err)
	}
	defer rows.Close()
	terms := []Term{}
	for rows.Next() {
		t, err := scanTerm(rows)
		if err != nil {
			return nil, err
		}
		terms = append(terms, t)
	}
	return terms, rows.Err()
}

// checkOverlap returns *ErrTermOverlap when an active Term other than id
// shares a day with start..end.
func checkOverlap(ctx context.Context, tx *sql.Tx, groupID string, id int64, start, end string) error {
	var name string
	err := tx.QueryRowContext(ctx, `SELECT name FROM terms
		WHERE group_id = ? AND archived_at IS NULL AND id <> ? AND start_date <= ? AND end_date >= ?
		ORDER BY start_date LIMIT 1`, groupID, id, end, start).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check term overlap: %w", err)
	}
	return &ErrTermOverlap{Name: name}
}

// Create adds an active Term. Returns *ErrTermOverlap.
func (r *TermRepo) Create(ctx context.Context, groupID, name, start, end string) (Term, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Term{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if err := checkOverlap(ctx, tx, groupID, 0, start, end); err != nil {
		return Term{}, err
	}
	t, err := scanTerm(tx.QueryRowContext(ctx, `INSERT INTO terms (group_id, name, start_date, end_date) VALUES (?, ?, ?, ?)
		RETURNING `+termColumns, groupID, name, start, end))
	if err != nil {
		return Term{}, err
	}
	return t, tx.Commit()
}

// Update renames and redates an active Term. Returns ErrNotFound (other
// Group's or archived) or *ErrTermOverlap.
func (r *TermRepo) Update(ctx context.Context, groupID string, id int64, name, start, end string) (Term, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Term{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	t, err := scanTerm(tx.QueryRowContext(ctx, `UPDATE terms SET name = ?, start_date = ?, end_date = ?
		WHERE id = ? AND group_id = ? AND archived_at IS NULL RETURNING `+termColumns,
		name, start, end, id, groupID))
	if err != nil {
		return Term{}, err
	}
	// Checked after the write so another Group's id reads as not found; the
	// rollback undoes it.
	if err := checkOverlap(ctx, tx, groupID, id, start, end); err != nil {
		return Term{}, err
	}
	return t, tx.Commit()
}

// Delete removes an active Term. Returns ErrNotFound, or ErrTermInUse while
// Reports or Report Instructions point at it.
func (r *TermRepo) Delete(ctx context.Context, groupID string, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var found, inUse bool
	err = tx.QueryRowContext(ctx, `SELECT
		EXISTS (SELECT 1 FROM terms WHERE id = ? AND group_id = ? AND archived_at IS NULL),
		EXISTS (SELECT 1 FROM reports WHERE term_id = ?) OR EXISTS (SELECT 1 FROM report_instructions WHERE term_id = ?)`,
		id, groupID, id, id).Scan(&found, &inUse)
	switch {
	case err != nil:
		return fmt.Errorf("check term: %w", err)
	case !found:
		return ErrNotFound
	case inUse:
		return ErrTermInUse
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM terms WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete term: %w", err)
	}
	return tx.Commit()
}
