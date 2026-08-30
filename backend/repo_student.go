package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// StudentRepo provides CRUD operations for the students table.
type StudentRepo struct{ db *sql.DB }

// Student represents a row in the students table.
type Student struct {
	ID        int64    `json:"id"`
	ClassID   int64    `json:"classId"`
	Name      string   `json:"name"`
	CreatedAt string   `json:"createdAt"`
	Aliases   []string `json:"aliases"`
}

// StudentAlias represents a row in the student_aliases table.
type StudentAlias struct {
	ID        int64  `json:"id"`
	StudentID int64  `json:"studentId"`
	ClassID   int64  `json:"classId"`
	Alias     string `json:"alias"`
	CreatedAt string `json:"createdAt"`
}

// List returns all students in a class, ordered by name.
// Aliases are NOT loaded here for performance; use ListWithAliases when needed.
func (r *StudentRepo) List(ctx context.Context, classID int64) ([]Student, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, class_id, name, created_at FROM students WHERE class_id = ? ORDER BY name",
		classID)
	if err != nil {
		return nil, fmt.Errorf("list students: %w", err)
	}
	defer rows.Close()

	var result []Student
	for rows.Next() {
		var s Student
		if err := rows.Scan(&s.ID, &s.ClassID, &s.Name, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan student: %w", err)
		}
		s.Aliases = []string{}
		result = append(result, s)
	}
	return result, rows.Err()
}

// ListWithAliases returns all students in a class with their aliases loaded,
// ordered by name; each student's Aliases is non-nil and ordered by alias.
// One query: students LEFT JOIN student_aliases, folded by foldStudentAliasRow.
func (r *StudentRepo) ListWithAliases(ctx context.Context, classID int64) ([]Student, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.class_id, s.name, s.created_at, sa.alias
		FROM students s
		LEFT JOIN student_aliases sa ON sa.student_id = s.id
		WHERE s.class_id = ?
		ORDER BY s.name, s.id, sa.alias`, classID)
	if err != nil {
		return nil, fmt.Errorf("list students with aliases: %w", err)
	}
	defer rows.Close()

	var result []Student
	for rows.Next() {
		var s Student
		var alias sql.NullString
		if err := rows.Scan(&s.ID, &s.ClassID, &s.Name, &s.CreatedAt, &alias); err != nil {
			return nil, fmt.Errorf("scan student with alias: %w", err)
		}
		result = foldStudentAliasRow(result, s, alias)
	}
	return result, rows.Err()
}

// foldStudentAliasRow folds one row of a students LEFT JOIN student_aliases
// query, ordered by student then alias, into students: a row whose student id
// differs from the last entry's appends that student with a non-nil, empty
// Aliases; a non-NULL alias is appended to the last entry, which — given the
// ordering — is always the student it belongs to.
func foldStudentAliasRow(students []Student, s Student, alias sql.NullString) []Student {
	n := len(students)
	if n == 0 || students[n-1].ID != s.ID {
		s.Aliases = []string{}
		students = append(students, s)
		n++
	}
	if alias.Valid {
		students[n-1].Aliases = append(students[n-1].Aliases, alias.String)
	}
	return students
}

// GetByID returns a single student by ID (aliases not loaded).
func (r *StudentRepo) GetByID(ctx context.Context, id int64) (Student, error) {
	var s Student
	err := r.db.QueryRowContext(ctx,
		"SELECT id, class_id, name, created_at FROM students WHERE id = ?", id,
	).Scan(&s.ID, &s.ClassID, &s.Name, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Student{}, ErrNotFound
	}
	if err != nil {
		return Student{}, fmt.Errorf("get student: %w", err)
	}
	s.Aliases = []string{}
	return s, nil
}

// Create inserts a new student into a class.
func (r *StudentRepo) Create(ctx context.Context, classID int64, name string) (Student, error) {
	// Check collision with existing aliases in the same class.
	var collision int
	err := r.db.QueryRowContext(ctx,
		"SELECT 1 FROM student_aliases WHERE class_id = ? AND alias = ? COLLATE NOCASE LIMIT 1",
		classID, name).Scan(&collision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Student{}, fmt.Errorf("create student: check alias collision: %w", err)
	}
	if err == nil {
		return Student{}, fmt.Errorf("create student %q: %w", name, ErrDuplicate)
	}

	var s Student
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO students (class_id, name) VALUES (?, ?)
		RETURNING id, class_id, name, created_at`,
		classID, name,
	).Scan(&s.ID, &s.ClassID, &s.Name, &s.CreatedAt)
	if err != nil {
		if isDuplicateErr(err) {
			return Student{}, fmt.Errorf("create student %q: %w", name, ErrDuplicate)
		}
		return Student{}, fmt.Errorf("create student: %w", err)
	}
	s.Aliases = []string{}
	return s, nil
}

// Rename updates a student's name.
func (r *StudentRepo) Rename(ctx context.Context, id int64, name string) error {
	s, err := r.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("rename student: %w", err)
	}

	// Check collision with existing aliases in the same class (excluding this student's own aliases).
	var collision int
	err = r.db.QueryRowContext(ctx,
		"SELECT 1 FROM student_aliases WHERE class_id = ? AND alias = ? COLLATE NOCASE AND student_id != ? LIMIT 1",
		s.ClassID, name, id).Scan(&collision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("rename student: check alias collision: %w", err)
	}
	if err == nil {
		return fmt.Errorf("rename student: %w", ErrDuplicate)
	}

	res, err := r.db.ExecContext(ctx,
		"UPDATE students SET name = ? WHERE id = ?", name, id)
	if err != nil {
		if isDuplicateErr(err) {
			return fmt.Errorf("rename student: %w", ErrDuplicate)
		}
		return fmt.Errorf("rename student: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// Move transfers a student, and their aliases, to a different class in one
// transaction. If the student's canonical name collides (case-insensitively)
// with a name or alias already in the target class, the move is aborted and
// *ErrDuplicateStudentName is returned; nothing is mutated. Aliases that
// merely collide with the target class are dropped rather than blocking the
// move; their text is returned in droppedAliases so the caller can tell the
// teacher.
func (r *StudentRepo) Move(ctx context.Context, id, newClassID int64) (droppedAliases []string, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("move student: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	var name string
	err = tx.QueryRowContext(ctx, "SELECT name FROM students WHERE id = ?", id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("move student: get student: %w", err)
	}

	if conflict, err := findNameOrAliasOwner(ctx, tx, newClassID, name, id); err != nil {
		return nil, fmt.Errorf("move student: check name collision: %w", err)
	} else if conflict != "" {
		return nil, &ErrDuplicateStudentName{ConflictName: conflict}
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE students SET class_id = ? WHERE id = ?", newClassID, id); err != nil {
		if isDuplicateErr(err) {
			return nil, &ErrDuplicateStudentName{ConflictName: name}
		}
		return nil, fmt.Errorf("move student: %w", err)
	}

	rows, err := tx.QueryContext(ctx,
		"SELECT id, alias FROM student_aliases WHERE student_id = ?", id)
	if err != nil {
		return nil, fmt.Errorf("move student: list aliases: %w", err)
	}
	type aliasRow struct {
		id    int64
		alias string
	}
	var aliases []aliasRow
	for rows.Next() {
		var ar aliasRow
		if err := rows.Scan(&ar.id, &ar.alias); err != nil {
			rows.Close()
			return nil, fmt.Errorf("move student: scan alias: %w", err)
		}
		aliases = append(aliases, ar)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("move student: alias rows: %w", err)
	}
	rows.Close()

	for _, ar := range aliases {
		conflict, err := findNameOrAliasOwner(ctx, tx, newClassID, ar.alias, id)
		if err != nil {
			return nil, fmt.Errorf("move student: check alias collision: %w", err)
		}
		if conflict != "" {
			if _, err := tx.ExecContext(ctx, "DELETE FROM student_aliases WHERE id = ?", ar.id); err != nil {
				return nil, fmt.Errorf("move student: drop colliding alias: %w", err)
			}
			droppedAliases = append(droppedAliases, ar.alias)
			continue
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE student_aliases SET class_id = ? WHERE id = ?", newClassID, ar.id); err != nil {
			return nil, fmt.Errorf("move student: update alias class: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("move student: commit: %w", err)
	}
	return droppedAliases, nil
}

// findNameOrAliasOwner returns the canonical name of the student in classID
// whose name or alias case-insensitively matches value, excluding
// excludeStudentID. Returns "" if there is no match.
func findNameOrAliasOwner(ctx context.Context, tx *sql.Tx, classID int64, value string, excludeStudentID int64) (string, error) {
	var owner string
	err := tx.QueryRowContext(ctx, `
		SELECT name FROM students
		WHERE class_id = ? AND name = ? COLLATE NOCASE AND id != ? LIMIT 1`,
		classID, value, excludeStudentID).Scan(&owner)
	if err == nil {
		return owner, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT s.name FROM student_aliases sa
		JOIN students s ON s.id = sa.student_id
		WHERE sa.class_id = ? AND sa.alias = ? COLLATE NOCASE AND sa.student_id != ? LIMIT 1`,
		classID, value, excludeStudentID).Scan(&owner)
	if err == nil {
		return owner, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return "", err
}

// Delete removes a student. Notes and aliases cascade via FK.
func (r *StudentRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM students WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete student: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// FindByNameAndClass looks up a student by canonical name OR alias (case-insensitive)
// for a given class name and user. Returns the student ID or ErrNotFound.
func (r *StudentRepo) FindByNameAndClass(ctx context.Context, name, className, userID string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id FROM students s
		JOIN classes c ON s.class_id = c.id
		JOIN levels l ON l.id = c.level_id
		LEFT JOIN student_aliases sa ON sa.student_id = s.id
		WHERE `+classDisplayNameSQL+` = ?
		  AND c.user_id = ?
		  AND (s.name = ? COLLATE NOCASE OR sa.alias = ? COLLATE NOCASE)
		LIMIT 1`,
		className, userID, name, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("find student by name and class: %w", err)
	}
	return id, nil
}

// BelongsToUser checks if a student belongs to a class owned by the given user.
func (r *StudentRepo) BelongsToUser(ctx context.Context, studentID int64, userID string) (bool, error) {
	var exists int
	err := r.db.QueryRowContext(ctx, `
		SELECT 1 FROM students s
		JOIN classes c ON s.class_id = c.id
		WHERE s.id = ? AND c.user_id = ?`,
		studentID, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check student ownership: %w", err)
	}
	return true, nil
}

// --- Alias methods ---

// AddAlias adds an alias for a student. Returns *ErrDuplicateAlias (with the
// conflicting student's canonical name) if the alias collides with another
// student's name or alias in the same class (case-insensitive).
func (r *StudentRepo) AddAlias(ctx context.Context, studentID int64, alias string) (StudentAlias, error) {
	// Fetch the class_id for this student.
	s, err := r.GetByID(ctx, studentID)
	if err != nil {
		return StudentAlias{}, fmt.Errorf("add alias: get student: %w", err)
	}

	// Check collision with canonical names in the same class.
	var conflictName string
	err = r.db.QueryRowContext(ctx,
		"SELECT name FROM students WHERE class_id = ? AND name = ? COLLATE NOCASE AND id != ? LIMIT 1",
		s.ClassID, alias, studentID).Scan(&conflictName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return StudentAlias{}, fmt.Errorf("add alias: check name collision: %w", err)
	}
	if err == nil {
		// alias matches another student's canonical name
		return StudentAlias{}, &ErrDuplicateAlias{ConflictStudentName: conflictName}
	}

	var a StudentAlias
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO student_aliases (student_id, class_id, alias)
		VALUES (?, ?, ?)
		RETURNING id, student_id, class_id, alias, created_at`,
		studentID, s.ClassID, alias,
	).Scan(&a.ID, &a.StudentID, &a.ClassID, &a.Alias, &a.CreatedAt)
	if err != nil {
		if isDuplicateErr(err) {
			// alias matches an existing alias — look up who owns it (best-effort; empty name is acceptable)
			var ownerName string
			if scanErr := r.db.QueryRowContext(ctx, `
				SELECT s.name FROM student_aliases sa
				JOIN students s ON s.id = sa.student_id
				WHERE sa.class_id = ? AND sa.alias = ? COLLATE NOCASE
				LIMIT 1`,
				s.ClassID, alias).Scan(&ownerName); scanErr != nil {
				ownerName = ""
			}
			return StudentAlias{}, &ErrDuplicateAlias{ConflictStudentName: ownerName}
		}
		return StudentAlias{}, fmt.Errorf("add alias: %w", err)
	}
	return a, nil
}

// RemoveAlias deletes an alias by ID, verifying it belongs to studentID. Returns ErrNotFound if not found.
func (r *StudentRepo) RemoveAlias(ctx context.Context, studentID, aliasID int64) error {
	res, err := r.db.ExecContext(ctx,
		"DELETE FROM student_aliases WHERE id = ? AND student_id = ?", aliasID, studentID)
	if err != nil {
		return fmt.Errorf("remove alias: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// ListAliases returns all aliases for a student.
func (r *StudentRepo) ListAliases(ctx context.Context, studentID int64) ([]StudentAlias, error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT id, student_id, class_id, alias, created_at FROM student_aliases WHERE student_id = ? ORDER BY alias",
		studentID)
	if err != nil {
		return nil, fmt.Errorf("list aliases: %w", err)
	}
	defer rows.Close()

	var result []StudentAlias
	for rows.Next() {
		var a StudentAlias
		if err := rows.Scan(&a.ID, &a.StudentID, &a.ClassID, &a.Alias, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alias: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
