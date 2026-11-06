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

func scanTerm(row *sql.Row) (Term, error) {
	var t Term
	err := row.Scan(&t.ID, &t.GroupID, &t.Name, &t.StartDate, &t.EndDate, &t.ArchivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Term{}, ErrNotFound
	}
	if err != nil {
		return Term{}, fmt.Errorf("get term: %w", err)
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
