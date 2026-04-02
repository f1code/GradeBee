# Edit Sample Report Cards

## Goal
Allow users to edit the name and content of existing sample report cards (report examples).

## Proposed Changes

### Backend

1. **`backend/repo_example.go`** — Add `Update(ctx, userID, id, name, content)` method to `ReportExampleRepo`
   - SQL: `UPDATE report_examples SET name = ?, content = ? WHERE id = ? AND user_id = ? RETURNING ...`

2. **`backend/report_examples.go`** — Add `UpdateExample(ctx, userID, id, name, content)` to `ExampleStore` interface + `dbExampleStore` implementation

3. **`backend/report_examples_handler.go`** — Add `handleUpdateReportExample` handler
   - Accepts JSON body `{ id, name, content }`
   - Uses `PUT /report-examples/{id}`

4. **`backend/handler.go`** — Add route: `PUT` on `report-examples/` prefix → `handleUpdateReportExample`

5. **`backend/ARCHITECTURE.md`** — Add the new route to the table

### Frontend

6. **`frontend/src/api.ts`** — Add `updateReportExample(id, name, content, getToken)` function
   - `PUT /report-examples/{id}` with JSON body

7. **`frontend/src/components/ReportExamples.tsx`** — Add edit UI in the expanded content area:
   - Make the content `<pre>` editable: replace with a `<textarea>` when editing
   - Add an edit (pencil) icon button to ItemRow's `actions` prop
   - Clicking edit enters edit mode: name becomes an input, content becomes a textarea
   - Save/Cancel buttons appear during edit mode
   - On save, call `updateReportExample` and refresh

8. **`frontend/src/components/ItemRow.tsx`** — No changes needed (already supports `actions` prop)

### Tests

9. **`backend/report_examples_handler_test.go`** — Add test for the update endpoint (auth, ownership, happy path)

10. **`frontend/src/components/__tests__/ReportExamples.test.tsx`** — Add test for edit flow

## Open Questions
None — full object (name + content) is always sent on update.
