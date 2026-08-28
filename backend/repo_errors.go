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
	// ErrInvalidDay is returned when a Class's day is missing or not one of
	// the seven canonical weekday names.
	ErrInvalidDay = errors.New("invalid day")
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

// ErrDuplicateStudentName is returned by Move when the student's canonical
// name collides with an existing student's name or alias in the target
// class. ConflictName holds the colliding name so the handler can name it
// in the 409 response.
type ErrDuplicateStudentName struct {
	ConflictName string
}

func (e *ErrDuplicateStudentName) Error() string {
	return fmt.Sprintf("student name conflicts with %q in target class", e.ConflictName)
}

// Is satisfies errors.Is for target *ErrDuplicateStudentName.
func (e *ErrDuplicateStudentName) Is(target error) bool {
	_, ok := target.(*ErrDuplicateStudentName)
	return ok
}

// Unwrap lets generic `errors.Is(err, ErrDuplicate)` checks keep working.
func (e *ErrDuplicateStudentName) Unwrap() error { return ErrDuplicate }

// ErrLevelInUse is returned by LevelRepo.Delete when Classes still
// reference the Level. Count holds how many, so the handler can tell the
// Admin exactly how many Classes need to move first.
type ErrLevelInUse struct {
	Count int
}

func (e *ErrLevelInUse) Error() string {
	return fmt.Sprintf("level is used by %d class(es)", e.Count)
}

// Is satisfies errors.Is for target *ErrLevelInUse.
func (e *ErrLevelInUse) Is(target error) bool {
	_, ok := target.(*ErrLevelInUse)
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
