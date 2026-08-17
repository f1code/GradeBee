# Derive relational facts, don't denormalize them

**Status:** accepted

## Context & Decision

Making **Level** a Group-owned entity (`classes.level_id`) raised two places where an
existing fact could be cached on the row or derived through the FK: the composite Class
display name (`"Fairy Tales 2-Sat@10:35"`), and the owning Group. We **derive both**.
`classes.name` is dropped and the display string is composed in SQL from `levels.name` +
`classes.schedule_name`; no table gets a `group_id` column except `levels`, where the
Group is the fact itself. Everything else reaches the Group through `level_id`.

This reverses the parent plan and ADR-0001, both of which specified `group_id` columns on
`classes` and `report_examples`.

## Considered Options

- **Keep `classes.name` denormalized**, recomputed on write. Rejected: renaming a Level is
  a first-class Admin action in the new Levels screen, so every cached copy must be fanned
  out and rewritten. A copy that can only ever be wrong-by-bug is a copy that must be
  verified forever.
- **Denormalize `group_id` onto `classes` and `report_examples`** for cheaper queries and a
  defence-in-depth `WHERE group_id = ?`. Rejected: `level_id` is `NOT NULL` on both, so the
  join always resolves and the Group is never ambiguous. The column adds a second thing to
  keep consistent and no new capability.

## Consequences

- Tenancy-scoped queries join `levels`. In practice the join was already there — group
  examples are found by `level_id` and teacher examples by `user_id`.
- `UNIQUE(user_id, name)` on `classes` is replaced by `UNIQUE(user_id, level_id, schedule_name)`.
- The Go field `Class.Name` and the JSON `name` survive unchanged, so API consumers and the
  extraction/transcription prompts are unaffected — only the SQL changes.
- Renaming a Level propagates to every Class display and to how past Reports appear. That is
  intended: renaming a course does not change which course a past Report was for.
