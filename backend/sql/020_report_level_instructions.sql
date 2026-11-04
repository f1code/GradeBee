-- 020_report_level_instructions.sql
-- The Level's Report Instructions the prompt used, so the eval replays a
-- report on the text it was written under, not the Level's current text.
-- NULL on reports from before this column.
ALTER TABLE reports ADD COLUMN level_instructions TEXT;
