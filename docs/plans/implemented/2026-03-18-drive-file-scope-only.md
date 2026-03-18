# Remove `spreadsheets` scope — use `drive.file` only

## Goal
Avoid the sensitive `https://www.googleapis.com/auth/spreadsheets` scope by creating spreadsheets via the Drive API and reading/writing cell data using the Sheets API authorized by `drive.file` scope alone (since the app created the file).

## Proposed changes

### 1. `backend/setup.go` — `createAndMoveClassSetup()`
- Replace `svc.Sheets.Spreadsheets.Create(...)` with `svc.Drive.Files.Create(...)` using mimeType `application/vnd.google-apps.spreadsheet`, name `ClassSetup`, and `Parents: []string{rootID}` (no need for the separate move step)
- After creation, populate the sheet using `svc.Sheets.Spreadsheets.BatchUpdate()` or `svc.Sheets.Spreadsheets.Values.Update()` to write the header + sample rows and rename the sheet to "Students"

### 2. `backend/google.go` — `newGoogleServices()`
- No changes needed; Sheets service is still constructed the same way, it just uses the `drive.file` token

### 3. `backend/students.go`
- No changes needed; `Spreadsheets.Values.Get` should work with `drive.file` on app-created files

### 4. Clerk dashboard
- Remove `https://www.googleapis.com/auth/spreadsheets` scope
- Keep `https://www.googleapis.com/auth/drive.file`

## Open questions
- Will `Spreadsheets.Values.Get/Update` and `Spreadsheets.BatchUpdate` actually accept `drive.file` scope for app-created files? (Google docs suggest yes, but this is what we're testing)
