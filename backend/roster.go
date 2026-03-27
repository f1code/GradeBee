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
	Students(ctx context.Context) ([]classGroup, error)
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

// ClassNames returns unique class names for the user.
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

// Students returns the full roster grouped by class.
func (r *dbRoster) Students(ctx context.Context) ([]classGroup, error) {
	classes, err := r.classRepo.List(ctx, r.userID)
	if err != nil {
		return nil, err
	}
	if len(classes) == 0 {
		return nil, nil
	}

	var result []classGroup
	for _, c := range classes {
		students, err := r.studentRepo.List(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		cg := classGroup{Name: c.Name, Students: make([]student, len(students))}
		for j, s := range students {
			cg.Students[j] = student{Name: s.Name}
		}
		result = append(result, cg)
	}
	return result, nil
}
