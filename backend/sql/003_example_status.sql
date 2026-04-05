-- 003_example_status.sql
ALTER TABLE report_examples ADD COLUMN status TEXT NOT NULL DEFAULT 'ready';
ALTER TABLE report_examples ADD COLUMN file_path TEXT NOT NULL DEFAULT '';
