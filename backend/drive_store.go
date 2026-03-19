// drive_store.go defines the DriveStore interface and its production
// implementation backed by the Google Drive API.
package handler

import (
	"context"
	"fmt"
	"io"

	"google.golang.org/api/drive/v3"
)

// DriveStore abstracts file operations on Google Drive for testability.
type DriveStore interface {
	Download(ctx context.Context, fileID string) (io.ReadCloser, error)
	FileName(ctx context.Context, fileID string) (string, error)
	Upload(ctx context.Context, parentID string, name string, content io.Reader) (fileID string, err error)
}

// sheetsDriveStore is the production DriveStore backed by a *drive.Service.
type sheetsDriveStore struct {
	svc *drive.Service
}

func newDriveStore(svc *googleServices) DriveStore {
	return &sheetsDriveStore{svc: svc.Drive}
}

func (s *sheetsDriveStore) Download(ctx context.Context, fileID string) (io.ReadCloser, error) {
	resp, err := s.svc.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("drive download: %w", err)
	}
	return resp.Body, nil
}

func (s *sheetsDriveStore) FileName(ctx context.Context, fileID string) (string, error) {
	fileMeta, err := s.svc.Files.Get(fileID).Fields("name").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("drive get name: %w", err)
	}
	return fileMeta.Name, nil
}

func (s *sheetsDriveStore) Upload(ctx context.Context, parentID, name string, content io.Reader) (string, error) {
	driveFile := &drive.File{
		Name:    name,
		Parents: []string{parentID},
	}
	created, err := s.svc.Files.Create(driveFile).Media(content).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("drive upload: %w", err)
	}
	return created.Id, nil
}
