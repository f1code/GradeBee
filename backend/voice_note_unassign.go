// voice_note_unassign.go implements DELETE /api/voice-notes/{uploadId}/assign/{studentId}:
// the teacher takes back what assign filed to one child from the done card
// (#138). The note goes, its link leaves the queued job, and the row is open
// again for the child it should have gone to.
//
// The note is found, not named. The card sends the child, and the server looks
// up that child's assigned notes from this recording by trace id
// (notes.trace_id, #139). A note id from the card would work for the create
// case and mislead on the rest: after an append the card holds the note the
// rows joined, which may be the pipeline's, and that one is not the server's
// to delete.
package handler

import (
	"errors"
	"log/slog"
	"net/http"
)

// UndoAssignmentResponse names the notes the call deleted, so the card can
// drop their links without waiting for the poll.
type UndoAssignmentResponse struct {
	NoteIDs []int64 `json:"noteIds"`
}

// handleUndoAssignment deletes the assigned notes one child got from this
// recording, and their links from the queued job.
//
// Only notes under NoteSourceAssigned. A row appended to the pipeline's note
// (#135) leaves it under `auto`, with the model's text around the row: not an
// assignment the server can take back, so a 404 and nothing written. The
// teacher edits that note by hand, as before.
//
// Every assigned note the child has from this recording goes, not the newest:
// a second tab that missed the first tab's link could have made a second one,
// and "undo" means the child has nothing assigned from this recording. In
// the one-tab case there is exactly one, grown by any later appends, and the
// card reopens every row that was on it.
//
// Order is assign's: ownership gates before the lock (the recording, bare 404
// either way; the child, through requireStudentOwnership), the lock across
// the note reads and writes and the job update, the job refused mid-run or
// failed and skipped when gone. No feedback record: assigned is not
// model-written, and deleting it says nothing about extraction.
func handleUndoAssignment(w http.ResponseWriter, r *http.Request) {
	log := loggerFromRequest(r)

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	uploadID, ok := idParam(r, "uploadId")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload id"})
		return
	}
	studentID, ok := idParam(r, "studentId")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student id"})
		return
	}

	row, err := serviceDeps.GetVoiceNoteRepo().GetByID(r.Context(), uploadID)
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "recording not found"})
		return
	case err != nil:
		writeInternalError(w, r, err)
		return
	case row.UserID != userID:
		log.Warn("undo assignment: recording not owned", "upload_id", uploadID, "user_id", userID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "recording not found"})
		return
	}
	if !requireStudentOwnership(w, r, studentID, userID, "student not found") {
		return
	}

	key := voiceNoteKey(userID, uploadID)
	if !takeUploadLock(key) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this recording is already being filed"})
		return
	}
	defer releaseUploadLock(key)

	queue, queueErr := serviceDeps.GetVoiceNoteQueue()
	var job *VoiceNoteJob
	if queueErr == nil {
		if j, err := queue.GetJob(r.Context(), key); err == nil {
			job = j
		}
	}
	switch {
	case job == nil:
	case job.Status == JobStatusFailed:
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this recording did not finish — retry it first"})
		return
	case job.Status != JobStatusDone:
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this recording is still being processed"})
		return
	}

	notes, err := serviceDeps.GetNoteRepo().ListForRecording(r.Context(), studentID, row.TraceID)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	// The job's links follow the deletes, whatever else happens: a delete
	// that fails after another succeeded (two tabs made two notes) still
	// returns 500, but the job must not keep a link to a note that is gone,
	// or the card's next poll rebuilds a count and a name with nothing
	// behind them.
	var gone []int64
	dropLinks := func() {
		if job == nil || len(gone) == 0 {
			return
		}
		job.NoteLinks = withoutLinks(job.NoteLinks, gone)
		if err := queue.UpdateJob(r.Context(), *job); err != nil {
			// The notes are gone; only the card's next poll is stale.
			log.Warn("undo assignment: could not update queued job", "upload_id", uploadID, "error", err)
		}
	}
	for _, n := range notes {
		if n.Source != NoteSourceAssigned {
			continue
		}
		if err := serviceDeps.GetNoteRepo().Delete(r.Context(), n.ID); err != nil && !errors.Is(err, ErrNotFound) {
			dropLinks()
			writeInternalError(w, r, err)
			return
		}
		gone = append(gone, n.ID)
	}
	if len(gone) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "nothing assigned to undo"})
		return
	}
	dropLinks()

	logAssignmentUndone(log, len(gone), userID, uploadID, row.TraceID, studentID)
	writeJSON(w, http.StatusOK, UndoAssignmentResponse{NoteIDs: gone})
}

// withoutLinks drops the links to the given notes, keeping order.
func withoutLinks(links []NoteLink, noteIDs []int64) []NoteLink {
	drop := make(map[int64]bool, len(noteIDs))
	for _, id := range noteIDs {
		drop[id] = true
	}
	kept := make([]NoteLink, 0, len(links))
	for _, l := range links {
		if !drop[l.NoteID] {
			kept = append(kept, l)
		}
	}
	return kept
}

// logAssignmentUndone is the recovery record's opposite number: a passage the
// teacher filed by hand and then took back. No name, no text (docs/adr/0003).
func logAssignmentUndone(log *slog.Logger, noteCount int, userID string, uploadID int64, traceID string, studentID int64) {
	log.Info("process voice note: assignment undone",
		"route", "manual",
		"note_count", noteCount,
		"user_id", userID, "upload_id", uploadID, "trace_id", traceID, "student_id", studentID)
}
