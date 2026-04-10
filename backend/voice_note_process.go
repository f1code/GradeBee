// voice_note_process.go implements the voice note processing pipeline
// (transcribe → extract → create notes). Called by MemQueue workers.
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

// processVoiceNote runs the voice note pipeline for a single job.
// It is the ProcessFunc for the voice note MemQueue — receives the queue
// (for status updates) and the job key.
func processVoiceNote(ctx context.Context, d deps, q JobQueue[VoiceNoteJob], key string) error {
	log := loggerFromContext(ctx)

	job, err := q.GetJob(ctx, key)
	if err != nil {
		return fmt.Errorf("process voice note: get job: %w", err)
	}

	// Idempotency: only process jobs that are queued.
	if job.Status != JobStatusQueued {
		log.Info("process voice note: skipping non-queued job", "key", key, "status", job.Status)
		return nil
	}

	userID := job.UserID
	uploadID := job.UploadID

	// Helper to mark job as failed and return the error.
	fail := func(step string, err error) error {
		log.Error("process voice note failed", "step", step, "key", key, "error", err)
		now := time.Now()
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("%s: %s", step, err.Error())
		job.FailedAt = &now
		if updateErr := q.UpdateJob(ctx, *job); updateErr != nil {
			log.Error("process voice note: failed to update job status to failed", "error", updateErr)
		}
		return fmt.Errorf("process voice note: %s: %w", step, err)
	}

	// --- Step 1: Transcribe (skip if text was pasted) ---
	roster := d.GetRoster(ctx, userID)
	var transcript string
	if job.Transcript != "" {
		// Text input — skip transcription entirely.
		transcript = job.Transcript
		log.Info("process voice note: skipping transcription (text input)", "key", key)
	} else {
		job.Status = JobStatusTranscribing
		if err := q.UpdateJob(ctx, *job); err != nil {
			return fail("update status to transcribing", err)
		}

		audioFile, err := os.Open(job.FilePath)
		if err != nil {
			return fail("open audio file", err)
		}
		defer audioFile.Close()

		var whisperPrompt string
		names, err := roster.ClassNames(ctx)
		if err != nil {
			log.Warn("process voice note: could not read class names", "error", err)
		} else if len(names) > 0 {
			whisperPrompt = "Classes: " + strings.Join(names, ", ")
		}

		transcriber, err := d.GetTranscriber()
		if err != nil {
			return fail("init transcriber", err)
		}

		transcript, err = transcriber.Transcribe(ctx, job.FileName, audioFile, whisperPrompt)
		if err != nil {
			return fail("transcribe", err)
		}
		job.Transcript = transcript
	}

	// --- Step 2: Extract ---
	job.Status = JobStatusExtracting
	if err := q.UpdateJob(ctx, *job); err != nil {
		return fail("update status to extracting", err)
	}

	classes, err := roster.Students(ctx)
	if err != nil {
		log.Warn("process voice note: could not read students for extraction", "error", err)
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
	if err := q.UpdateJob(ctx, *job); err != nil {
		return fail("update status to creating_notes", err)
	}

	noteCreator := d.GetNoteCreator()
	studentRepo := d.GetStudentRepo()

	var noteLinks []NoteLink
	for _, student := range extractResult.Students {
		if student.Confidence < autoCreateConfidenceThreshold {
			log.Info("process voice note: skipping low-confidence match",
				"student", student.Name, "confidence", student.Confidence)
			continue
		}

		studentID, err := studentRepo.FindByNameAndClass(ctx, student.Name, student.Class, userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				log.Warn("process voice note: student not found in DB, skipping",
					"student", student.Name, "class", student.Class)
				continue
			}
			return fail("find student "+student.Name, err)
		}

		result, err := noteCreator.CreateNote(ctx, CreateNoteRequest{
			StudentID:   studentID,
			StudentName: student.Name,
			QuotedText:  student.QuotedText,  // Changed from Summary
			Transcript:  transcript,
			Date:        extractResult.Date,
		})
		if err != nil {
			return fail("create note for "+student.Name, err)
		}
		noteLinks = append(noteLinks, NoteLink{
			Name: student.Name, NoteID: result.NoteID,
			StudentID: studentID, ClassName: student.Class,
		})
	}

	// --- Done ---
	voiceNoteRepo := d.GetVoiceNoteRepo()
	if err := voiceNoteRepo.MarkProcessed(ctx, uploadID); err != nil {
		log.Warn("process voice note: failed to mark voice note processed", "error", err)
	}

	job.Status = JobStatusDone
	job.NoteLinks = noteLinks
	job.Error = ""
	job.FailedAt = nil
	if err := q.UpdateJob(ctx, *job); err != nil {
		return fmt.Errorf("process voice note: update status to done: %w", err)
	}

	log.Info("process voice note completed",
		"key", key, "user_id", userID, "upload_id", uploadID,
		"note_count", len(noteLinks))
	return nil
}
