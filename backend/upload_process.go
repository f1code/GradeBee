// upload_process.go implements the async upload processing pipeline
// (transcribe → extract → create notes). Called by memQueue worker goroutines.
package handler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Minimum extraction confidence to auto-create a note.
const autoCreateConfidenceThreshold = 0.5

// processUploadJob runs the full pipeline: transcribe → extract → create notes.
// It reads/writes job state via the UploadQueue and uses the deps interface
// for all external service calls.
func processUploadJob(ctx context.Context, d deps, userID string, uploadID int64) error {
	log := loggerFromContext(ctx)

	queue, err := d.GetUploadQueue()
	if err != nil {
		return fmt.Errorf("process job: get queue: %w", err)
	}

	job, err := queue.GetJob(ctx, userID, uploadID)
	if err != nil {
		return fmt.Errorf("process job: get job: %w", err)
	}

	// Idempotency: only process jobs that are queued.
	if job.Status != JobStatusQueued {
		log.Info("process job: skipping non-queued job", "user_id", userID, "upload_id", uploadID, "status", job.Status)
		return nil
	}

	// Helper to mark job as failed and return the error.
	fail := func(step string, err error) error {
		log.Error("process job failed", "step", step, "user_id", userID, "upload_id", uploadID, "error", err)
		now := time.Now()
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("%s: %s", step, err.Error())
		job.FailedAt = &now
		if updateErr := queue.UpdateJob(ctx, *job); updateErr != nil {
			log.Error("process job: failed to update job status to failed", "error", updateErr)
		}
		return fmt.Errorf("process job: %s: %w", step, err)
	}

	// --- Step 1: Transcribe ---
	job.Status = JobStatusTranscribing
	if err := queue.UpdateJob(ctx, *job); err != nil {
		return fail("update status to transcribing", err)
	}

	// Read audio from local disk.
	audioFile, err := os.Open(job.FilePath)
	if err != nil {
		return fail("open audio file", err)
	}
	defer audioFile.Close()

	// Build Whisper prompt from roster class names (best-effort).
	var whisperPrompt string
	roster := d.GetRoster(ctx, userID)
	names, err := roster.ClassNames(ctx)
	if err != nil {
		log.Warn("process job: could not read class names", "error", err)
	} else if len(names) > 0 {
		whisperPrompt = "Classes: " + strings.Join(names, ", ")
	}

	transcriber, err := d.GetTranscriber()
	if err != nil {
		return fail("init transcriber", err)
	}

	transcript, err := transcriber.Transcribe(ctx, job.FileName, audioFile, whisperPrompt)
	if err != nil {
		return fail("transcribe", err)
	}

	// --- Step 2: Extract ---
	job.Status = JobStatusExtracting
	if err := queue.UpdateJob(ctx, *job); err != nil {
		return fail("update status to extracting", err)
	}

	classes, err := roster.Students(ctx)
	if err != nil {
		log.Warn("process job: could not read students for extraction", "error", err)
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

	noteCreator := d.GetNoteCreator()
	studentRepo := d.GetStudentRepo()

	var noteLinks []NoteLink
	for _, student := range extractResult.Students {
		if student.Confidence < autoCreateConfidenceThreshold {
			log.Info("process job: skipping low-confidence match",
				"student", student.Name, "confidence", student.Confidence)
			continue
		}

		// Resolve student name → DB ID.
		studentID, err := studentRepo.FindByNameAndClass(ctx, student.Name, student.Class, userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				log.Warn("process job: student not found in DB, skipping",
					"student", student.Name, "class", student.Class)
				continue
			}
			return fail("find student "+student.Name, err)
		}

		result, err := noteCreator.CreateNote(ctx, CreateNoteRequest{
			StudentID:   studentID,
			StudentName: student.Name,
			Summary:     student.Summary,
			Transcript:  transcript,
			Date:        extractResult.Date,
		})
		if err != nil {
			return fail("create note for "+student.Name, err)
		}
		noteLinks = append(noteLinks, NoteLink{Name: student.Name, NoteID: result.NoteID, StudentID: studentID, ClassName: student.Class})
	}

	// --- Done ---
	// Mark upload as processed.
	uploadRepo := d.GetUploadRepo()
	if err := uploadRepo.MarkProcessed(ctx, uploadID); err != nil {
		log.Warn("process job: failed to mark upload processed", "error", err)
	}

	job.Status = JobStatusDone
	job.NoteLinks = noteLinks
	job.Error = ""
	job.FailedAt = nil
	if err := queue.UpdateJob(ctx, *job); err != nil {
		return fmt.Errorf("process job: update status to done: %w", err)
	}

	log.Info("process job completed",
		"user_id", userID, "upload_id", uploadID, "note_count", len(noteLinks))
	return nil
}
