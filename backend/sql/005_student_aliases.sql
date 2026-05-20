-- 005_student_aliases.sql: Add student_aliases table for nickname/variant matching.

CREATE TABLE IF NOT EXISTS student_aliases (
    id         INTEGER PRIMARY KEY,
    student_id INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    class_id   INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    alias      TEXT    NOT NULL COLLATE NOCASE,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(class_id, alias)
);

CREATE INDEX IF NOT EXISTS idx_student_aliases_class_alias ON student_aliases(class_id, alias);
CREATE INDEX IF NOT EXISTS idx_student_aliases_student     ON student_aliases(student_id);
