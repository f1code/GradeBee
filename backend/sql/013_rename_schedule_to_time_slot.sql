-- 013_rename_schedule_to_time_slot.sql
-- Rename classes.schedule_name -> classes.time_slot. SQLite RENAME COLUMN
-- auto-updates indexes, FKs, and table constraints (UNIQUE(user_id, level_id,
-- schedule_name) becomes UNIQUE(user_id, level_id, time_slot)). Values are
-- preserved verbatim.

ALTER TABLE classes RENAME COLUMN schedule_name TO time_slot;
