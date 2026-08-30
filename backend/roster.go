// roster.go defines the Roster interface and its DB-backed implementation.
// The Roster is used by the upload processing pipeline to get class names
// (for Whisper prompts) and student lists (for extraction matching).
package handler

import (
	"context"
)

// Roster abstracts read access to the user's student roster.
type Roster interface {
	ClassNames(ctx context.Context) ([]string, error)
	Students(ctx context.Context) ([]ClassGroup, error)
}

// dbRoster reads roster data from the SQLite database.
type dbRoster struct {
	classRepo   *ClassRepo
	studentRepo *StudentRepo
	userID      string
}

func newDBRoster(cr *ClassRepo, sr *StudentRepo, userID string) *dbRoster {
	return &dbRoster{classRepo: cr, studentRepo: sr, userID: userID}
}

// ClassNames returns unique composite class names (Level + Time slot) for the user.
func (r *dbRoster) ClassNames(ctx context.Context) ([]string, error) {
	classes, err := r.classRepo.List(ctx, r.userID)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(classes))
	for i, c := range classes {
		names[i] = c.Name
	}
	return names, nil
}

// Students returns the full roster grouped by class, with aliases included
// so the extraction prompt can match nicknames/variants. One query for the
// whole roster; nil when the user has no classes.
func (r *dbRoster) Students(ctx context.Context) ([]ClassGroup, error) {
	classes, err := r.classRepo.ListWithStudents(ctx, r.userID)
	if err != nil {
		return nil, err
	}
	if len(classes) == 0 {
		return nil, nil
	}

	result := make([]ClassGroup, len(classes))
	for i, c := range classes {
		cg := ClassGroup{Name: c.Name, Students: make([]ClassStudent, len(c.Students))}
		for j, s := range c.Students {
			cg.Students[j] = ClassStudent{Name: s.Name, Aliases: s.Aliases}
		}
		result[i] = cg
	}
	return result, nil
}
