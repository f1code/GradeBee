// voice_note_cleanup.go provides a background goroutine that deletes voice_notes
// rows, their transcripts and any audio still on disk after a retention period —
// counted from processing for done jobs, from upload for jobs that never finished.
package handler

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// StartVoiceNoteCleanup runs a background loop that removes stale voice notes.
// It deletes the audio file from disk and the row from the voice_notes table once
// the voice note has been processed — or, if it never was, uploaded — for longer
// than the retention duration.
// Stops when ctx is cancelled.
func StartVoiceNoteCleanup(ctx context.Context, repo *VoiceNoteRepo, retention, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanProcessedVoiceNotes(ctx, repo, retention)
		}
	}
}

func cleanProcessedVoiceNotes(ctx context.Context, repo *VoiceNoteRepo, retention time.Duration) {
	cutoff := time.Now().Add(-retention).UTC().Format("2006-01-02T15:04:05.000Z")
	stale, err := repo.ListStale(ctx, cutoff)
	if err != nil {
		slog.Error("voice note cleanup: list stale", "error", err)
		return
	}

	for _, v := range stale {
		if v.FilePath != "" && v.PurgedAt == nil {
			if err := os.Remove(v.FilePath); err != nil && !os.IsNotExist(err) {
				slog.Error("voice note cleanup: remove file", "path", v.FilePath, "error", err)
				continue
			}
		}
		if err := repo.Delete(ctx, v.ID); err != nil {
			slog.Error("voice note cleanup: delete row", "id", v.ID, "error", err)
			continue
		}
		slog.Info("voice note cleanup: removed", "id", v.ID, "file", v.FilePath)
	}
}
