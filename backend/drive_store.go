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
	// Copy duplicates a file into the given folder, returning the new file ID.
	Copy(ctx context.Context, fileID string, destFolderID string, newName string) (string, error)
	// GetMimeType returns the MIME type of a file.
	GetMimeType(ctx context.Context, fileID string) (string, error)
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

func (s *sheetsDriveStore) Copy(ctx context.Context, fileID, destFolderID, newName string) (string, error) {
	copyFile := &drive.File{
		Name:    newName,
		Parents: []string{destFolderID},
	}
	copied, err := s.svc.Files.Copy(fileID, copyFile).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("drive copy: %w", err)
	}
	return copied.Id, nil
}

func (s *sheetsDriveStore) GetMimeType(ctx context.Context, fileID string) (string, error) {
	fileMeta, err := s.svc.Files.Get(fileID).Fields("mimeType").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("drive get mime type: %w", err)
	}
	return fileMeta.MimeType, nil
}
