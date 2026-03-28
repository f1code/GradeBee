# Introduce Drive Client Interface

## Goal
Replace direct usage of `*drive.Service` with a thin interface so Drive-dependent handlers can be unit-tested with simple stubs instead of fake HTTP servers.

## Current State
- `ServiceDeps.GetDriveClient()` returns `*drive.Service` (concrete Google SDK type)
- Both `handleDriveImport` and `handleDriveImportExample` use exactly two operations:
  1. `driveSvc.Files.Get(fileID).Fields("mimeType").Context(ctx).Do()` → get metadata
  2. `driveSvc.Files.Get(fileID).Context(ctx).Download()` → download file content
- Testing requires faking Google's HTTP API behind a real `*drive.Service`

## Plan

### 1. Define `DriveClient` interface — `backend/drive_client.go` (new)
```go
type DriveFile struct {
    MimeType string
}

type DriveClient interface {
    // GetFileMeta returns the MIME type (and potentially other metadata) for a file.
    GetFileMeta(ctx context.Context, fileID string) (*DriveFile, error)
    // DownloadFile returns the file content as an io.ReadCloser.
    DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, error)
}
```

### 2. Add production implementation — same file
```go
type googleDriveClient struct {
    svc *drive.Service
}

func (g *googleDriveClient) GetFileMeta(ctx context.Context, fileID string) (*DriveFile, error) {
    meta, err := g.svc.Files.Get(fileID).Fields("mimeType").Context(ctx).Do()
    if err != nil { return nil, err }
    return &DriveFile{MimeType: meta.MimeType}, nil
}

func (g *googleDriveClient) DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, error) {
    resp, err := g.svc.Files.Get(fileID).Context(ctx).Download()
    if err != nil { return nil, err }
    return resp.Body, nil
}
```

### 3. Update `ServiceDeps` interface — `backend/deps.go`
- Change `GetDriveClient(ctx, userID) (*drive.Service, error)` → `GetDriveClient(ctx, userID) (DriveClient, error)`
- Update `prodDeps.GetDriveClient` to return `&googleDriveClient{svc}` wrapping the existing `newDriveReadClient` result

### 4. Update `handleDriveImport` — `backend/drive_import.go`
Replace:
- `driveSvc.Files.Get(req.FileID).Fields("mimeType").Context(ctx).Do()` → `driveSvc.GetFileMeta(ctx, req.FileID)`
- `fileMeta.MimeType` → `fileMeta.MimeType` (same field name on `DriveFile`)
- `driveSvc.Files.Get(req.FileID).Context(ctx).Download()` → `driveSvc.DownloadFile(ctx, req.FileID)`
- `resp.Body` → the returned `io.ReadCloser` directly

### 5. Update `handleDriveImportExample` — `backend/drive_import_example.go`
Same substitutions as step 4.

### 6. Update `mockDepsAll` in tests — `backend/testutil_test.go`
- Change `driveClient *drive.Service` → `driveClient DriveClient`
- Return type already matches after step 3

### 7. Add `stubDriveClient` — `backend/testutil_test.go`
```go
type stubDriveClient struct {
    meta    *DriveFile
    metaErr error
    data    io.ReadCloser
    dlErr   error
}
func (s *stubDriveClient) GetFileMeta(ctx context.Context, fileID string) (*DriveFile, error) {
    return s.meta, s.metaErr
}
func (s *stubDriveClient) DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, error) {
    return s.data, s.dlErr
}
```

## Files to Modify
- `backend/deps.go` — change `GetDriveClient` return type, wrap in `googleDriveClient`
- `backend/drive_import.go` — use `DriveClient` interface methods
- `backend/drive_import_example.go` — use `DriveClient` interface methods
- `backend/testutil_test.go` — update `mockDepsAll.driveClient` type, add `stubDriveClient`

## New Files
- `backend/drive_client.go` — `DriveClient` interface, `DriveFile` struct, `googleDriveClient` implementation

## Risks
- The `google/api/drive/v3` import can be removed from handler files (only needed in `drive_client.go` and `google.go` now) — verify no other usages.
- `resp.Body.Close()` is currently called via `defer` in both handlers. After the change, the `io.ReadCloser` returned by `DownloadFile` must still be closed — callers keep the same `defer rc.Close()` pattern.
