# Report Example Categories

## Goal

Different classes (e.g. "Mousy", "Emma") need different report card styles. Allow teachers to tag report examples with class names so that report generation uses only relevant examples.

## Data Model Changes

### Classes table

The current `name` column is split into two concepts:

- **`class_name`** — the class identity (e.g. "Mousy"). Autocomplete from existing distinct values for the user.
- **`group_name`** — the scheduling instance (e.g. "Thursday", "Wednesday").

Display name is `"{class_name} {group_name}"` (or just `class_name` if group is empty).

### Report example classes (new join table)

```sql
CREATE TABLE report_example_classes (
    example_id INTEGER NOT NULL REFERENCES report_examples(id) ON DELETE CASCADE,
    class_name TEXT NOT NULL,
    PRIMARY KEY (example_id, class_name)
);
```

Keyed on `class_name` string (not class ID), since multiple class rows share the same class name. At least one class_name required per example (enforced in application code).

### Report generation

When generating for a student in class with `class_name = "Mousy"`, load only examples where `report_example_classes` contains "Mousy".

## API Changes

### Classes

- `POST /classes` and `PUT /classes/{id}` — body changes from `{ name }` to `{ className, group }`.
- `GET /classes` — response includes `className`, `group` fields plus computed display name.
- **New** `GET /classes/class-names` — returns distinct class names for the current user (for autocomplete).

### Report examples

- `POST /report-examples` — body gets `classNames: string[]` (required, non-empty).
- `PUT /report-examples/{id}` — can update `classNames` along with name/content.
- `GET /report-examples` — response includes `classNames: string[]` per example.

### Report generation

No API change — backend resolves student → class → class_name → matching examples internally.

## Frontend Changes

### Class creation/editing

- Two fields: "Class" (autocomplete input) and "Group" (text input).
- Autocomplete populated from `GET /classes/class-names`.

### Report examples

- Upload flow adds required multi-select for class names (from same endpoint).
- Example list shows class name tags as small badges on each row.
- Edit form includes multi-select to update class assignments.
- Validation: can't save without at least one class selected.

## Migration

### Classes

Split existing `name` on first `" - "` (trimmed):

- `"Mousy - Thursday"` → `class_name = "Mousy"`, `group_name = "Thursday"`
- `"Mousy"` → `class_name = "Mousy"`, `group_name = ""`

### Report examples

Drop all existing examples. Teachers re-upload with class tags. (Current examples are few and easy to re-add.)

## Open Questions

None.
