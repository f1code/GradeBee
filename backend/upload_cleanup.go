// upload_cleanup.go provides a background goroutine that deletes processed
// audio files from disk and their uploads rows after a retention period.
package handler

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// StartUploadCleanup runs a background loop that removes stale processed uploads.
// It deletes the audio file from disk and the row from the uploads table once
// the upload has been processed for longer than the retention duration.
// Stops when ctx is cancelled.
func StartUploadCleanup(ctx context.Context, repo *UploadRepo, uploadsDir string, retention, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanProcessedUploads(ctx, repo, retention)
		}
	}
}

func cleanProcessedUploads(ctx context.Context, repo *UploadRepo, retention time.Duration) {
	cutoff := time.Now().Add(-retention).UTC().Format("2006-01-02T15:04:05.000Z")
	stale, err := repo.ListStale(ctx, cutoff)
	if err != nil {
		slog.Error("upload cleanup: list stale", "error", err)
		return
	}

	for _, u := range stale {
		if err := os.Remove(u.FilePath); err != nil && !os.IsNotExist(err) {
			slog.Error("upload cleanup: remove file", "path", u.FilePath, "error", err)
			continue
		}
		if err := repo.Delete(ctx, u.ID); err != nil {
			slog.Error("upload cleanup: delete row", "id", u.ID, "error", err)
			continue
		}
		slog.Info("upload cleanup: removed", "id", u.ID, "file", u.FilePath)
	}
}
