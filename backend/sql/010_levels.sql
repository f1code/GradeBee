-- 010_levels.sql
-- Level entity: Group-owned curriculum tier with shared Report Instructions.

CREATE TABLE levels (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  group_id            TEXT NOT NULL,
  name                TEXT NOT NULL COLLATE NOCASE,
  report_instructions TEXT NOT NULL DEFAULT '',
  created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (group_id, name)
);
