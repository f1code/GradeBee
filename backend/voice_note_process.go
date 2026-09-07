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

	// failJob marks the job failed and returns the error, writing to two audiences.
	//
	// step is telemetry: it is logged here, and the returned error is logged again by
	// the queue worker, so both reach Sentry and neither may carry a student name
	// (docs/adr/0003). message is what the teacher reads verbatim — JobStatus
	// renders job.Error.
	failJob := func(step, message string, err error) error {
		if errors.Is(err, errNoSpeechDetected) {
			log.Warn("process voice note failed", "step", step, "key", key, "trace_id", job.TraceID, "error", err)
		} else {
			log.Error("process voice note failed", "step", step, "key", key, "trace_id", job.TraceID, "error", err)
		}
		now := time.Now()
		job.Status = JobStatusFailed
		job.Error = message
		job.FailedAt = &now
		if updateErr := q.UpdateJob(ctx, *job); updateErr != nil {
			log.Error("process voice note: failed to update job status to failed", "error", updateErr)
		}
		return fmt.Errorf("process voice note: %s: %w", step, err)
	}
	// failWith composes the teacher's message from detail plus the raw error. detail
	// may name the student they are entitled to see; step must not.
	failWith := func(step, detail string, err error) error {
		return failJob(step, fmt.Sprintf("%s: %s", detail, err.Error()), err)
	}

	// failTooOld is for a retry that outlived the retention cleanup: the audio file
	// or the voice_notes row is gone, and no wording of the raw error helps the
	// teacher. Same telemetry step as the raw failure would have carried.
	failTooOld := func(step string, err error) error {
		return failJob(step, "this upload is too old to retry", err)
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
		// Text input, or a retry after transcription — skip transcription entirely.
		transcript = job.Transcript
		log.Info("process voice note: skipping transcription, job already carries the text", "key", key)
	} else {
		job.Status = JobStatusTranscribing
		if err := q.UpdateJob(ctx, *job); err != nil {
			return fail("update status to transcribing", err)
		}

		audioFile, err := os.Open(job.FilePath)
		if err != nil {
			if os.IsNotExist(err) {
				// The retention cleanup removed the file: a job that failed before
				// transcription and was retried after the window.
				return failTooOld("open audio file", err)
			}
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
		// This comes before the transcript is persisted, on purpose: the privacy
		// page promises the recording is gone right after transcription, so no
		// failure below may leave it on disk.
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

	// Persist the transcript on the voice_notes row before extraction, so it exists
	// whether or not any note is created — the only other copy is notes.transcript,
	// written per created note. A failed write fails the job: the job struct keeps
	// the transcript, so a retry skips transcription and lands here again. The
	// error carries no transcript (docs/adr/0003).
	if err := d.GetVoiceNoteRepo().SetTranscript(ctx, uploadID, transcript); err != nil {
		if errors.Is(err, ErrNotFound) {
			// The retention cleanup removed the row: a retry that outlived the window.
			return failTooOld("persist transcript", err)
		}
		return fail("persist transcript", err)
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

	// One note per child, and the passages the done card gets back. The card
	// shows them as what the recording held; it does not hand them back to the
	// assemble endpoint, which since #127 runs pass 2 itself against the class
	// the teacher picks.
	notes, passages := assemblePassages(extractResult.Passages)

	var noteLinks []NoteLink

	// The note's date is the day the teacher recorded, which is the day the job was
	// created at upload (voice_note_dispatch.go) — not time.Now(). Processing is queued
	// and retryable (handleJobRetry republishes the same job struct, and Publish stores
	// it verbatim), so the processing clock can land days after the recording and would
	// drop the note out of the report window it belongs to. Do not "simplify" this to
	// time.Now().
	//
	// UTC, matching every created_at column. No teacher timezone is stored anywhere, so
	// a recording whose local day differs from its UTC day is dated to the UTC one.
	// West of Greenwich that window is late afternoon to local midnight — class time:
	// 16:15 at UTC-8 is 00:15 the next day in UTC. East of Greenwich it is local
	// midnight to the offset, which is not class time. Today's teachers are all east of
	// Greenwich; plumbing a client timezone is deferred, not judged unnecessary.
	noteDate := job.CreatedAt.UTC().Format(time.DateOnly)

	// Drops are only interpretable as a rate, so the completion record below needs a
	// denominator and a per-reason breakdown. note_count alone yields notes-created,
	// never passages-extracted.
	kinds := countKinds(extractResult.Passages)
	droppedNoRosterMatch, droppedUnattributed := 0, 0

	// Every passage about one child that reached none, counted once and logged
	// once. Under the old contract this was the low-confidence drop; there is
	// no confidence score any more, and a passage nobody is named in is not a
	// low-confidence guess about who — it is the recording not saying. A group
	// passage is not counted: it has no student because it belongs to all of
	// them.
	for _, p := range passages {
		if p.Kind != PassageUnknown && !(p.Kind == PassageChild && p.Student == "") {
			continue
		}
		droppedUnattributed++
		// "process voice note: mention dropped" is a stable query key: the Sentry
		// readout filters on this exact string paired with reason, and reason is
		// already a live attribute elsewhere in this project, so it is not
		// selective on its own. Both drop sites share the string deliberately;
		// do not reword either without updating the saved queries.
		// No student name in telemetry: these logs reach Sentry. See docs/adr/0003.
		log.Info("process voice note: mention dropped",
			"reason", "unattributed",
			"key", key, "user_id", userID, "upload_id", uploadID, "trace_id", job.TraceID,
			"kind", string(p.Kind),
			"label_count", len(p.SpokenLabels),
			"class_name", extractResult.ClassName,
			"model", extractor.Model(), "prompt_hash", ExtractionPromptHash)
	}

	for _, n := range notes {
		studentID, err := studentRepo.FindByNameAndClass(ctx, n.Name, extractResult.ClassName, userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				droppedNoRosterMatch++
				// Same stable query key as the unattributed site; only reason
				// separates them. Pass 2's schema constrains the student to
				// this class's roster, so reaching here means the roster read
				// and this lookup disagree — a child deleted mid-run.
				// No student name in telemetry: these logs reach Sentry. See docs/adr/0003.
				log.Info("process voice note: mention dropped",
					"reason", "no_roster_match",
					"key", key, "user_id", userID, "upload_id", uploadID, "trace_id", job.TraceID,
					"passage_count", n.Passages,
					"class_name", extractResult.ClassName,
					"model", extractor.Model(), "prompt_hash", ExtractionPromptHash)
				continue
			}
			// The failed lookup is the only source of an identifier here, so telemetry
			// carries none; the teacher still gets the name that failed to resolve.
			return failWith("find student", "find student "+n.Name, err)
		}

		result, err := noteCreator.CreateNote(ctx, CreateNoteRequest{
			StudentID:    studentID,
			StudentName:  n.Name,
			QuotedText:   n.Summary,
			Transcript:   transcript,
			Date:         noteDate,
			ModelVersion: extractor.Model(),
			TraceID:      job.TraceID,
		})
		if err != nil {
			return failWith(
				fmt.Sprintf("create note for student %d", studentID),
				"create note for "+n.Name,
				err)
		}
		noteLinks = append(noteLinks, NoteLink{
			Name: n.Name, NoteID: result.NoteID,
			StudentID: studentID, ClassName: extractResult.ClassName,
		})
	}

	// --- Done ---
	voiceNoteRepo := d.GetVoiceNoteRepo()
	if err := voiceNoteRepo.MarkProcessed(ctx, uploadID); err != nil {
		log.Warn("process voice note: failed to mark voice note processed", "error", err)
	}

	job.Status = JobStatusDone
	job.NoteLinks = noteLinks
	job.Passages = passages
	// The class pass 1 pinned, whether or not any note was filed under it. A
	// pinned recording whose passages all reached nobody still has to tell the
	// card which roster to offer, so the teacher can file those passages by
	// hand; "" is a decline. The reason switch below and assembleOutcome read
	// this as "the class in force", never as "notes exist", and the card gates
	// its class picker on CanPickClass alone.
	job.ClassName = extractResult.ClassName
	if pinned, ok := findClass(classes, extractResult.ClassName); ok {
		job.ClassID = pinned.ID
	}
	// One reason, chosen once. A decline is not a noNotesReason case at all:
	// pass 1 could not pin a class, so pass 2 never ran and there are no
	// passages, and anySpokenLabel(nil) is false — noNotesReason would answer
	// nobody_named and the card would suppress the class picker on exactly the
	// recording that needs it.
	switch {
	case extractResult.ClassName == "":
		job.NoNotesReason = NoNotesClassUnclear
	default:
		job.NoNotesReason = noNotesReason(len(noteLinks), passages)
	}
	// The card's gate for the class picker, decided here because this is the
	// only place that knows both the pinned class and what came back. The
	// assemble handler carries this value rather than recomputing it.
	job.CanPickClass = canPickClass(job.NoNotesReason)
	job.Error = ""
	job.FailedAt = nil
	if err := q.UpdateJob(ctx, *job); err != nil {
		return fmt.Errorf("process voice note: update status to done: %w", err)
	}

	// passages_total is the denominator the drop records are read against; 0 also
	// names the third silent-nothing mode, where extraction cut the recording into
	// nothing at all and there is consequently nothing to drop. The per-kind counts
	// beside it say which shape a recording came back as, which is what tells a
	// prompt regression (every block unknown) from a quiet recording.
	//
	// These attribute names replaced mentions_total, mentions_below_0_7 and
	// dropped_low_confidence when the two-pass contract landed (#125). There is no
	// confidence score any more, so any saved Sentry query on those three is
	// measuring a field nothing writes.
	//
	// This record is the authoritative numerator as well as the denominator: derive the
	// drop rate from its own dropped_* fields over its own passages_total. Counting the
	// per-passage drop records instead mixes populations — a job that fails partway
	// emits drop records but never reaches this line — and that mistake has already
	// produced one wrong figure for this task.
	log.Info("process voice note completed",
		"key", key, "user_id", userID, "upload_id", uploadID, "trace_id", job.TraceID,
		"note_count", len(noteLinks),
		// Without this a decline is unreadable here: it stores no passages and
		// emits no drop record, so it lands as passages_total=0 with every kind
		// zero — the same shape as an extraction that cut the recording into
		// nothing. The decline rate is the number that says whether declining
		// is helping teachers or stranding them, so it has to be legible.
		"no_notes_reason", job.NoNotesReason,
		"passages_total", len(extractResult.Passages),
		"passages_child", kinds[PassageChild],
		"passages_unknown", kinds[PassageUnknown],
		"passages_group", kinds[PassageGroup],
		"passages_none", kinds[PassageNone],
		"dropped_unattributed", droppedUnattributed,
		"dropped_no_roster_match", droppedNoRosterMatch,
		"model", extractor.Model(), "prompt_hash", ExtractionPromptHash)
	return nil
}
