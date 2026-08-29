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

// errNoSpeechDetected marks an empty/silent recording. It's a user-input
// condition, not an application bug, so fail() logs it as a warning instead
// of an error — keeping it out of Sentry issues while still failing the job.
var errNoSpeechDetected = errors.New("no speech detected in audio")

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

	// failWith marks the job failed and returns the error, writing to two audiences.
	//
	// step is telemetry: it is logged here, and the returned error is logged again by
	// the queue worker, so both reach Sentry and neither may carry a student name
	// (docs/adr/0003). detail is what the teacher reads — JobStatus renders job.Error
	// — so it names the student they are entitled to see.
	failWith := func(step, detail string, err error) error {
		if errors.Is(err, errNoSpeechDetected) {
			log.Warn("process voice note failed", "step", step, "key", key, "error", err)
		} else {
			log.Error("process voice note failed", "step", step, "key", key, "error", err)
		}
		now := time.Now()
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("%s: %s", detail, err.Error())
		job.FailedAt = &now
		if updateErr := q.UpdateJob(ctx, *job); updateErr != nil {
			log.Error("process voice note: failed to update job status to failed", "error", updateErr)
		}
		return fmt.Errorf("process voice note: %s: %w", step, err)
	}

	// Helper to mark job as failed and return the error, where the same wording
	// serves both audiences because no student is named.
	fail := func(step string, err error) error {
		return failWith(step, step, err)
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

		var classNames []string
		names, err := roster.ClassNames(ctx)
		if err != nil {
			log.Warn("process voice note: could not read class names", "error", err)
		} else {
			classNames = names
		}

		transcriber, err := d.GetTranscriber()
		if err != nil {
			return fail("init transcriber", err)
		}

		transcript, err = transcriber.Transcribe(ctx, job.FileName, audioFile, classNames)
		if err != nil {
			return fail("transcribe", err)
		}
		if strings.TrimSpace(transcript) == "" {
			return fail("transcribe", errNoSpeechDetected)
		}
		job.Transcript = transcript

		// Delete the audio file immediately after successful transcription —
		// the raw recording is no longer needed; the transcript is sufficient.
		if job.FilePath != "" {
			if removeErr := os.Remove(job.FilePath); removeErr != nil && !os.IsNotExist(removeErr) {
				log.Warn("process voice note: could not delete audio after transcription",
					"path", job.FilePath, "error", removeErr)
			} else {
				voiceNoteRepo := d.GetVoiceNoteRepo()
				if purgeErr := voiceNoteRepo.MarkPurged(ctx, uploadID); purgeErr != nil {
					log.Warn("process voice note: could not mark audio purged", "error", purgeErr)
				}
			}
		}
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
			// No student name in telemetry: these logs reach Sentry. See docs/adr/0003.
			log.Info("process voice note: skipping low-confidence match",
				"key", key, "confidence", student.Confidence)
			continue
		}

		studentID, err := studentRepo.FindByNameAndClass(ctx, student.Name, student.ClassName, userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// No student name in telemetry: these logs reach Sentry. See docs/adr/0003.
				log.Warn("process voice note: student not found in DB, skipping",
					"key", key, "class_name", student.ClassName)
				continue
			}
			// The failed lookup is the only source of an identifier here, so telemetry
			// carries none; the teacher still gets the name that failed to resolve.
			return failWith("find student", "find student "+student.Name, err)
		}

		result, err := noteCreator.CreateNote(ctx, CreateNoteRequest{
			StudentID:    studentID,
			StudentName:  student.Name,
			QuotedText:   student.QuotedText,  // Changed from Summary
			Transcript:   transcript,
			Date:         extractResult.Date,
			ModelVersion: extractor.Model(),
		})
		if err != nil {
			return failWith(
				fmt.Sprintf("create note for student %d", studentID),
				"create note for "+student.Name,
				err)
		}
		noteLinks = append(noteLinks, NoteLink{
			Name: student.Name, NoteID: result.NoteID,
			StudentID: studentID, ClassName: student.ClassName,
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
