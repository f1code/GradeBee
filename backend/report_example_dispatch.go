// extraction_dispatch.go contains the shared logic for saving a file to disk,
// creating a pending DB row, and publishing an extraction job to the queue.
package handler

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// dispatchExtraction saves a file to disk, creates a pending DB row, and
// publishes an ExtractionJob. Returns the pending example for the API response.
// extOverride, if non-empty, is used instead of the file extension from name
// (useful when the MIME type is more reliable than the filename).
func dispatchExtraction(ctx context.Context, userID, name string, data []byte, extOverride string) (*ReportExample, error) {
	uploadsDir := serviceDeps.GetUploadsDir()
	ext := filepath.Ext(name)
	if extOverride != "" {
		ext = extOverride
	}
	diskName := uuid.New().String() + ext
	diskPath := filepath.Join(uploadsDir, diskName)

	if err := os.WriteFile(diskPath, data, 0o644); err != nil {
		return nil, err
	}

	store := serviceDeps.GetExampleStore()
	example, err := store.CreatePendingExample(ctx, userID, name, diskPath)
	if err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	// publishOrCleanup attempts to publish the job and cleans up on failure.
	publishErr := func() error {
		queue, err := serviceDeps.GetExtractionQueue()
		if err != nil {
			return err
		}
		return queue.Publish(ctx, ExtractionJob{
			UserID:    userID,
			ExampleID: example.ID,
			FilePath:  diskPath,
			FileName:  name,
			Status:    JobStatusQueued,
			CreatedAt: time.Now(),
		})
	}()
	if publishErr != nil {
		os.Remove(diskPath)
		_ = store.DeleteExample(ctx, userID, example.ID) //nolint:errcheck // best-effort cleanup
		return nil, publishErr
	}

	return example, nil
}
