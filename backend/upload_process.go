// upload_process.go implements the async upload processing pipeline
// (transcribe → extract → create notes). Called by memQueue worker goroutines.
package handler

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Minimum extraction confidence to auto-create a note.
const autoCreateConfidenceThreshold = 0.5

// processUploadJob runs the full pipeline: transcribe → extract → create notes.
// It reads/writes job state via the UploadQueue and uses the deps interface
// for all external service calls.
func processUploadJob(ctx context.Context, d deps, userID, fileID string) error {
	log := loggerFromContext(ctx)

	queue, err := d.GetUploadQueue()
	if err != nil {
		return fmt.Errorf("process job: get queue: %w", err)
	}

	job, err := queue.GetJob(ctx, userID, fileID)
	if err != nil {
		return fmt.Errorf("process job: get job: %w", err)
	}

	// Idempotency: only process jobs that are queued.
	if job.Status != JobStatusQueued {
		log.Info("process job: skipping non-queued job", "user_id", userID, "file_id", fileID, "status", job.Status)
		return nil
	}

	// Helper to mark job as failed and return the error.
	fail := func(step string, err error) error {
		log.Error("process job failed", "step", step, "user_id", userID, "file_id", fileID, "error", err)
		now := time.Now()
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("%s: %s", step, err.Error())
		job.FailedAt = &now
		if updateErr := queue.UpdateJob(ctx, *job); updateErr != nil {
			log.Error("process job: failed to update job status to failed", "error", updateErr)
		}
		return fmt.Errorf("process job: %s: %w", step, err)
	}

	// Build Google services for the user (no HTTP request needed).
	svc, err := d.GoogleServicesForUser(ctx, userID)
	if err != nil {
		return fail("google services", err)
	}

	// --- Step 1: Transcribe ---
	job.Status = JobStatusTranscribing
	if err := queue.UpdateJob(ctx, *job); err != nil {
		return fail("update status to transcribing", err)
	}

	store := d.GetDriveStore(svc)

	body, err := store.Download(ctx, fileID)
	if err != nil {
		return fail("download audio", err)
	}
	defer body.Close()

	fileName, err := store.FileName(ctx, fileID)
	if err != nil || fileName == "" {
		fileName = "audio.webm"
	}

	// Build Whisper prompt from roster class names (best-effort).
	var whisperPrompt string
	roster, err := d.GetRoster(ctx, svc)
	if err != nil {
		log.Warn("process job: roster unavailable for prompt", "error", err)
	} else {
		names, err := roster.ClassNames(ctx)
		if err != nil {
			log.Warn("process job: could not read class names", "error", err)
		} else if len(names) > 0 {
			whisperPrompt = "Classes: " + strings.Join(names, ", ")
		}
	}

	transcriber, err := d.GetTranscriber()
	if err != nil {
		return fail("init transcriber", err)
	}

	transcript, err := transcriber.Transcribe(ctx, fileName, body, whisperPrompt)
	if err != nil {
		return fail("transcribe", err)
	}

	// --- Step 2: Extract ---
	job.Status = JobStatusExtracting
	if err := queue.UpdateJob(ctx, *job); err != nil {
		return fail("update status to extracting", err)
	}

	var classes []classGroup
	if roster != nil {
		classes, err = roster.Students(ctx)
		if err != nil {
			log.Warn("process job: could not read students for extraction", "error", err)
		}
	}

	extractor, err := d.GetExtractor()
	if err != nil {
		return fail("init extractor", err)
	}

	extractResult, err := extractor.Extract(ctx, ExtractRequest{
		Transcript: transcript,
		Classes:    classes,
	})
	if err != nil {
		return fail("extract", err)
	}

	// --- Step 3: Create notes ---
	job.Status = JobStatusCreatingNotes
	if err := queue.UpdateJob(ctx, *job); err != nil {
		return fail("update status to creating_notes", err)
	}

	meta, err := d.GetGradeBeeMetadata(ctx, userID)
	if err != nil || meta == nil || meta.NotesID == "" {
		return fail("get metadata", fmt.Errorf("notes folder not configured, run setup first"))
	}

	noteCreator := d.GetNoteCreator(svc)

	var noteLinks []NoteLink
	for _, student := range extractResult.Students {
		if student.Confidence < autoCreateConfidenceThreshold {
			log.Info("process job: skipping low-confidence match",
				"student", student.Name, "confidence", student.Confidence)
			continue
		}

		result, err := noteCreator.CreateNote(ctx, CreateNoteRequest{
			NotesRootID: meta.NotesID,
			StudentName: student.Name,
			ClassName:   student.Class,
			Summary:     student.Summary,
			Transcript:  transcript,
			Date:        extractResult.Date,
		})
		if err != nil {
			return fail("create note for "+student.Name, err)
		}
		noteLinks = append(noteLinks, NoteLink{Name: student.Name, URL: result.DocURL})
	}

	// --- Done ---
	job.Status = JobStatusDone
	job.NoteLinks = noteLinks
	job.Error = ""
	job.FailedAt = nil
	if err := queue.UpdateJob(ctx, *job); err != nil {
		return fmt.Errorf("process job: update status to done: %w", err)
	}

	log.Info("process job completed",
		"user_id", userID, "file_id", fileID, "note_count", len(noteLinks))
	return nil
}
