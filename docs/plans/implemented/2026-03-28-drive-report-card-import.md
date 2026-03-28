# Drive Report Card Import

## Goal
Add a "Import from Google Drive" button to the Report Examples section that lets users pick PDF/image/text files from Drive and import them as example report cards.

## Plan

1. **Make `useDrivePicker` accept MIME types as a parameter** — `frontend/src/hooks/useDrivePicker.ts`
   - Change `openPicker` to accept `(accessToken: string, options?: { mimeTypes?: string; title?: string })`
   - Default to current `AUDIO_MIME_TYPES` for backward compat
   - Export `AUDIO_MIME_TYPES` constant so AudioUpload can pass it explicitly

2. **Add `importExampleFromDrive` API function** — `frontend/src/api.ts`
   - New function: `importExampleFromDrive(fileId: string, fileName: string, getToken)` → `POST /drive-import-example`
   - Returns `ReportExampleItem`

3. **Add Drive import button to ReportExamples component** — `frontend/src/components/ReportExamples.tsx`
   - Import `useDrivePicker` and `getGoogleToken`, `importExampleFromDrive` from api
   - Define `REPORT_MIME_TYPES` = `application/pdf,image/png,image/jpeg,image/webp,text/plain,text/markdown`
   - Add a "Import from Drive" button next to the drop zone (or inside it)
   - On click: `getGoogleToken` → `openPicker(token, { mimeTypes: REPORT_MIME_TYPES, title: 'Select a report card' })` → `importExampleFromDrive(id, name)` → `load()` to refresh list
   - Show uploading state while importing

4. **Add new backend endpoint `POST /drive-import-example`** — `backend/drive_import_example.go` (new file)
   - Accept JSON body: `{ fileId, fileName }`
   - Get user ID from request
   - Get Drive client via `serviceDeps.GetDriveClient(ctx, userID)`
   - Validate MIME type is in allowed set (PDF, images, text)
   - Download file from Drive
   - If extractable (PDF/image), use `serviceDeps.GetExampleExtractor()` to extract text
   - If text file, use content directly
   - Store via `serviceDeps.GetExampleStore().UploadExample()`
   - Return the created `ReportExample`

5. **Register the new route** — `backend/handler.go`
   - Add case: `path == "drive-import-example" && r.Method == http.MethodPost` → `handleDriveImportExample`

6. **Update AudioUpload to pass MIME types explicitly** — `frontend/src/components/AudioUpload.tsx`
   - Pass `AUDIO_MIME_TYPES` to `openPicker` to maintain current behavior after the hook signature change

## Files to Modify
- `frontend/src/hooks/useDrivePicker.ts` — parameterize MIME types and title
- `frontend/src/components/AudioUpload.tsx` — pass MIME types explicitly to `openPicker`
- `frontend/src/components/ReportExamples.tsx` — add Drive import button and flow
- `frontend/src/api.ts` — add `importExampleFromDrive` function
- `backend/handler.go` — register new route

## New Files
- `backend/drive_import_example.go` — handler for `POST /drive-import-example`: downloads file from Drive, extracts text if needed, stores as report example

## Risks
- **Google OAuth scopes**: The existing Clerk Google OAuth setup must include `drive.readonly` scope. Already works for audio import, so should be fine.
- **File size**: Drive files could be large. The existing report example handler caps multipart at 10MB (`10 << 20`). Apply same limit when downloading from Drive.
- **Google Docs/Sheets**: Native Google formats (Docs, Sheets) require `export` instead of `download`. Filter them out via MIME types in the picker (only allow standard file types, not `application/vnd.google-apps.*`). Alternatively, could support export as PDF in future.
- **Text file encoding**: Downloaded text files from Drive may have varied encodings. Treat as UTF-8 (same as current behavior for direct uploads).
