-- 009_rename_class_group_columns.sql
-- Rename class_name → level_name, group_name → schedule_name in classes.
-- Rename class_name → level_name in report_example_classes.
-- SQLite RENAME COLUMN auto-updates indexes, FKs, and table constraints.

ALTER TABLE classes RENAME COLUMN class_name TO level_name;
ALTER TABLE classes RENAME COLUMN group_name TO schedule_name;

ALTER TABLE report_example_classes RENAME COLUMN class_name TO level_name;
