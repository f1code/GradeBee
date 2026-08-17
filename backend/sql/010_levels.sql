-- 010_levels.sql
-- Level entity: Group-owned curriculum tier with shared Report Instructions.
-- Seeds the 8 hand-authored production Levels against the production Clerk
-- organisation ID, hardcoded as a literal (see plans/2026-08-12-phase-2-levels-entity.md).
-- This is a one-shot data migration; RunMigrations takes no parameters and
-- the org ID exists only in the Clerk dashboard. Dev/test DBs get the same
-- literal — harmless, since tests build their own Levels via a fixture.

CREATE TABLE levels (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  group_id            TEXT NOT NULL,
  name                TEXT NOT NULL COLLATE NOCASE,
  report_instructions TEXT NOT NULL DEFAULT '',
  created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (group_id, name)
);

INSERT INTO levels (group_id, name) VALUES
  ('org_3HzJprFapIy18PHMdKc26VZr7yG', 'Marcia'),
  ('org_3HzJprFapIy18PHMdKc26VZr7yG', 'Mousy'),
  ('org_3HzJprFapIy18PHMdKc26VZr7yG', 'Oliver'),
  ('org_3HzJprFapIy18PHMdKc26VZr7yG', 'Linda'),
  ('org_3HzJprFapIy18PHMdKc26VZr7yG', 'Sam'),
  ('org_3HzJprFapIy18PHMdKc26VZr7yG', 'Time Zone'),
  ('org_3HzJprFapIy18PHMdKc26VZr7yG', 'Fairy Tales 2'),
  ('org_3HzJprFapIy18PHMdKc26VZr7yG', 'Emma');
