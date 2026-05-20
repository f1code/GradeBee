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

// ErrDuplicateAlias is returned by AddAlias when the alias collides with an
// existing student name or alias in the same class. ConflictStudentName holds
// the canonical name of the student who owns the conflicting value, so the
// handler can include it in the 409 response.
type ErrDuplicateAlias struct {
	ConflictStudentName string
}

func (e *ErrDuplicateAlias) Error() string { return "alias already in use in this class" }

// Is satisfies errors.Is for target *ErrDuplicateAlias.
func (e *ErrDuplicateAlias) Is(target error) bool {
	_, ok := target.(*ErrDuplicateAlias)
	return ok
}

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
