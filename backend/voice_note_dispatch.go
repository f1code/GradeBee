// voice_note_dispatch.go contains the dispatch logic for saving a voice note
// file to disk, creating a DB row, and publishing a processing job.
package handler

import (
	"context"
	"os"
	"time"
)

// dispatchVoiceNote saves audio data to disk, creates a voice_notes row, and
// publishes a VoiceNoteJob. Returns the created VoiceNote for the API response.
func dispatchVoiceNote(ctx context.Context, userID, fileName, ext, mimeType, source string, data []byte) (*VoiceNote, error) {
	diskPath, err := saveToUploadsDir(data, ext)
	if err != nil {
		return nil, err
	}

	queue, err := serviceDeps.GetVoiceNoteQueue()
	if err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	repo := serviceDeps.GetVoiceNoteRepo()
	upload, err := repo.Create(ctx, userID, fileName, diskPath)
	if err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	err = publishOrCleanup(ctx, queue, VoiceNoteJob{
		UserID:    userID,
		UploadID:  upload.ID,
		FilePath:  diskPath,
		FileName:  fileName,
		MimeType:  mimeType,
		Source:    source,
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	},
		func() { os.Remove(diskPath) },
		func() { _ = repo.Delete(ctx, upload.ID) }, //nolint:errcheck // best-effort cleanup
	)
	if err != nil {
		return nil, err
	}

	return &upload, nil
}
