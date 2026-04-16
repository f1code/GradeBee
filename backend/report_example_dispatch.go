// report_example_dispatch.go contains the dispatch logic for saving a file
// to disk, creating a pending DB row, and publishing an extraction job.
package handler

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// dispatchExtraction saves a file to disk, creates a pending DB row, and
// publishes an ExtractionJob. Returns the pending example for the API response.
// extOverride, if non-empty, is used instead of the file extension from name
// (useful when the MIME type is more reliable than the filename).
func dispatchExtraction(ctx context.Context, userID, name string, data []byte, extOverride string, classNames []string) (*ReportExample, error) {
	ext := filepath.Ext(name)
	if extOverride != "" {
		ext = extOverride
	}

	diskPath, err := saveToUploadsDir(data, ext)
	if err != nil {
		return nil, err
	}

	queue, err := serviceDeps.GetExtractionQueue()
	if err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	store := serviceDeps.GetExampleStore()
	example, err := store.CreatePendingExample(ctx, userID, name, diskPath, classNames)
	if err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	err = publishOrCleanup(ctx, queue, ExtractionJob{
		UserID:    userID,
		ExampleID: example.ID,
		FilePath:  diskPath,
		FileName:  name,
		Status:    JobStatusQueued,
		CreatedAt: time.Now(),
	},
		func() { os.Remove(diskPath) },
		func() { _ = store.DeleteExample(ctx, userID, example.ID) }, //nolint:errcheck // best-effort
	)
	if err != nil {
		return nil, err
	}

	return example, nil
}
