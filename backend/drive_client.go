// drive_client.go defines the DriveClient interface and its production
// implementation backed by the Google Drive SDK.
package handler

import (
	"context"
	"io"
	"time"

	"google.golang.org/api/drive/v3"
)

// driveTimeout bounds each Drive API call. For DownloadFile it covers the
// whole transfer, including streaming the body, since the deadline lives on
// the request ctx.
const driveTimeout = 120 * time.Second

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

// GetFileMeta fetches the file's MIME type, bounded by driveTimeout.
func (g *googleDriveClient) GetFileMeta(ctx context.Context, fileID string) (*DriveFile, error) {
	ctx, cancel := context.WithTimeout(ctx, driveTimeout)
	defer cancel()
	meta, err := g.svc.Files.Get(fileID).Fields("mimeType").Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return &DriveFile{MimeType: meta.MimeType}, nil
}

// DownloadFile starts a media download bounded by driveTimeout. The deadline
// applies to the returned body too, so a `defer cancel()` here would kill the
// stream on return; instead the cancel is released when the caller closes
// the body.
func (g *googleDriveClient) DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, driveTimeout)
	resp, err := g.svc.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		cancel()
		return nil, err
	}
	return &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}, nil
}

// cancelOnClose releases a ctx's cancel func when the wrapped body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	defer c.cancel()
	return c.ReadCloser.Close()
}
