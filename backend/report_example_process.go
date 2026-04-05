// report_example_process.go implements the extraction pipeline for async
// report example text extraction. Called by MemQueue workers.
package handler

import (
	"context"
	"fmt"
	"os"
)

// processExtraction runs the text extraction pipeline for a single job.
func processExtraction(ctx context.Context, d deps, q JobQueue[ExtractionJob], key string) error {
	log := loggerFromContext(ctx)

	job, err := q.GetJob(ctx, key)
	if err != nil {
		return fmt.Errorf("process extraction: get job: %w", err)
	}
	if job.Status != JobStatusQueued {
		return nil
	}

	fail := func(step string, err error) error {
		log.Error("process extraction failed", "step", step, "key", key, "error", err)
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("%s: %s", step, err.Error())
		if updateErr := q.UpdateJob(ctx, *job); updateErr != nil {
			log.Error("process extraction: failed to update job status", "error", updateErr)
		}
		store := d.GetExampleStore()
		if statusErr := store.UpdateExampleStatus(ctx, job.ExampleID, "failed", ""); statusErr != nil {
			log.Error("process extraction: failed to update example status", "error", statusErr)
		}
		return fmt.Errorf("process extraction: %s: %w", step, err)
	}

	// Read file from disk.
	data, err := os.ReadFile(job.FilePath)
	if err != nil {
		return fail("read file", err)
	}

	// Extract text.
	extractor, err := d.GetExampleExtractor()
	if err != nil {
		return fail("init extractor", err)
	}
	content, err := extractor.ExtractText(ctx, job.FileName, data)
	if err != nil {
		return fail("extract", err)
	}

	// Update example with extracted content.
	store := d.GetExampleStore()
	if err := store.UpdateExampleStatus(ctx, job.ExampleID, "ready", content); err != nil {
		return fail("update example", err)
	}

	// Clean up file.
	os.Remove(job.FilePath)

	job.Status = JobStatusDone
	job.Error = ""
	if err := q.UpdateJob(ctx, *job); err != nil {
		log.Error("process extraction: failed to update job status to done", "error", err)
	}

	log.Info("extraction completed", "key", key, "example_id", job.ExampleID)
	return nil
}
