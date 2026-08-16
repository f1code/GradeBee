package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// LevelRepo provides CRUD operations for the levels table. Every method is
// scoped by group_id — a Level belongs to exactly one Group and is never
// visible or reachable outside it.
type LevelRepo struct{ db *sql.DB }

// Level represents a row in the levels table.
type Level struct {
	ID                 int64  `json:"id"`
	GroupID            string `json:"groupId"`
	Name               string `json:"name"`
	ReportInstructions string `json:"reportInstructions"`
	CreatedAt          string `json:"createdAt"`
}

// List returns all Levels for a Group, ordered by name.
func (r *LevelRepo) List(ctx context.Context, groupID string) ([]Level, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, group_id, name, report_instructions, created_at FROM levels WHERE group_id = ? ORDER BY name",
		groupID)
	if err != nil {
		return nil, fmt.Errorf("list levels: %w", err)
	}
	defer rows.Close()

	var result []Level
	for rows.Next() {
		var l Level
		if err := rows.Scan(&l.ID, &l.GroupID, &l.Name, &l.ReportInstructions, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan level: %w", err)
		}
		result = append(result, l)
	}
	return result, rows.Err()
}

// GetByID returns a single Level, scoped to the Group. Returns ErrNotFound if
// the Level doesn't exist or belongs to a different Group.
func (r *LevelRepo) GetByID(ctx context.Context, groupID string, id int64) (Level, error) {
	var l Level
	err := r.db.QueryRowContext(ctx,
		"SELECT id, group_id, name, report_instructions, created_at FROM levels WHERE id = ? AND group_id = ?",
		id, groupID,
	).Scan(&l.ID, &l.GroupID, &l.Name, &l.ReportInstructions, &l.CreatedAt)
	if err == sql.ErrNoRows {
		return Level{}, ErrNotFound
	}
	if err != nil {
		return Level{}, fmt.Errorf("get level: %w", err)
	}
	return l, nil
}

// Create inserts a new Level for the Group. Returns ErrDuplicate if the name
// is already used within the Group.
func (r *LevelRepo) Create(ctx context.Context, groupID, name string) (Level, error) {
	var l Level
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO levels (group_id, name)
		VALUES (?, ?)
		RETURNING id, group_id, name, report_instructions, created_at`,
		groupID, name,
	).Scan(&l.ID, &l.GroupID, &l.Name, &l.ReportInstructions, &l.CreatedAt)
	if err != nil {
		if isDuplicateErr(err) {
			return Level{}, fmt.Errorf("create level %q: %w", name, ErrDuplicate)
		}
		return Level{}, fmt.Errorf("create level: %w", err)
	}
	return l, nil
}

// Rename updates a Level's name within the Group. A plain rename — no merge
// path. Returns ErrNotFound if the Level doesn't exist in the Group, or
// ErrDuplicate if another Level in the Group already has that name.
func (r *LevelRepo) Rename(ctx context.Context, groupID string, id int64, name string) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE levels SET name = ? WHERE id = ? AND group_id = ?",
		name, id, groupID)
	if err != nil {
		if isDuplicateErr(err) {
			return fmt.Errorf("rename level: %w", ErrDuplicate)
		}
		return fmt.Errorf("rename level: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// UpdateReportInstructions updates a Level's Report Instructions within the
// Group. Returns ErrNotFound if the Level doesn't exist in the Group.
func (r *LevelRepo) UpdateReportInstructions(ctx context.Context, groupID string, id int64, instructions string) error {
	res, err := r.db.ExecContext(ctx,
		"UPDATE levels SET report_instructions = ? WHERE id = ? AND group_id = ?",
		instructions, id, groupID)
	if err != nil {
		return fmt.Errorf("update report instructions: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Delete removes a Level from the Group. Returns ErrNotFound if the Level
// doesn't exist in the Group.
func (r *LevelRepo) Delete(ctx context.Context, groupID string, id int64) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM levels WHERE id = ? AND group_id = ?", id, groupID)
	if err != nil {
		return fmt.Errorf("delete level: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}
