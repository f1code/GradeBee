-- 004_class_categories.sql: Add class_name and group_name columns to classes table.

ALTER TABLE classes ADD COLUMN class_name TEXT NOT NULL DEFAULT '';
ALTER TABLE classes ADD COLUMN group_name TEXT NOT NULL DEFAULT '';

-- Populate class_name and group_name from existing name column
-- Split on first "-": part before is class_name, part after is group_name (both trimmed)
-- If no "-", class_name = name, group_name = ''
UPDATE classes
SET
    class_name = CASE
        WHEN INSTR(name, '-') > 0 THEN TRIM(SUBSTR(name, 1, INSTR(name, '-') - 1))
        ELSE name
    END,
    group_name = CASE
        WHEN INSTR(name, '-') > 0 THEN TRIM(SUBSTR(name, INSTR(name, '-') + 1))
        ELSE ''
    END;

CREATE UNIQUE INDEX idx_classes_user_class_group ON classes(user_id, class_name, group_name);

CREATE TABLE IF NOT EXISTS report_example_classes (
    example_id  INTEGER NOT NULL REFERENCES report_examples(id) ON DELETE CASCADE,
    class_name  TEXT NOT NULL,
    PRIMARY KEY (example_id, class_name)
);

-- Remove all existing report examples
DELETE FROM report_examples;
