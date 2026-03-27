package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrNotFound is returned when a queried entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrDuplicate is returned on unique constraint violations.
	ErrDuplicate = errors.New("duplicate")
)

// isDuplicateErr checks if a SQLite error is a UNIQUE constraint violation.
func isDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// rowsAffectedOrNotFound checks RowsAffected and returns ErrNotFound if 0.
func rowsAffectedOrNotFound(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
