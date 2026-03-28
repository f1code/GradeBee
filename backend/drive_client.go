// drive_client.go defines the DriveClient interface and its production
// implementation backed by the Google Drive SDK.
package handler

import (
	"context"
	"io"

	"google.golang.org/api/drive/v3"
)

// DriveFile holds the metadata returned by DriveClient.GetFileMeta.
type DriveFile struct {
	MimeType string
}

// DriveClient abstracts the two Drive operations used by import handlers so
// they can be tested without a real HTTP server.
type DriveClient interface {
	// GetFileMeta returns file metadata (currently only MimeType).
	GetFileMeta(ctx context.Context, fileID string) (*DriveFile, error)
	// DownloadFile returns the file content as an io.ReadCloser.
	// The caller is responsible for closing the returned reader.
	DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, error)
}

// googleDriveClient is the production DriveClient backed by *drive.Service.
type googleDriveClient struct {
	svc *drive.Service
}

func (g *googleDriveClient) GetFileMeta(ctx context.Context, fileID string) (*DriveFile, error) {
	meta, err := g.svc.Files.Get(fileID).Fields("mimeType").Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return &DriveFile{MimeType: meta.MimeType}, nil
}

func (g *googleDriveClient) DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, error) {
	resp, err := g.svc.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
