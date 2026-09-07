// voice_note_assign.go implements POST /api/voice-notes/{uploadId}/assign: the
// teacher files passages the recording could not place — a pronoun block, a
// name nobody on the roster answers to — to a child they pick on the done
// card, and a note exists for that child, dated from the recording (#134).
// When the card already holds a note for that child from this recording, the
// rows land on it instead (#135).
//
// The passages come from the card, not from the job. The job is in memory and
// gone on restart, and pass 2 is non-deterministic, so an index into a job's
// passage list could point at other text than the teacher ticked. That makes
// the text client-supplied: the server holds no copy to check it against, and
// the note is filed under NoteSourceAssigned rather than a model-written
// source for exactly that reason.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// AssignPassagesRequest is the body of the assign call: the class in force on
// the card, the child to file to, and the passages the teacher ticked.
//
// One student per call. The card confirms per child, so a confirm is one
// request and one note: atomic by construction, with no transaction across
// notes to get wrong.
type AssignPassagesRequest struct {
	// ClassID is the class whose roster the card offered. The server checks
	// that the caller owns it and that StudentID is on it, which proves the
	// picker showed the right roster and forbids filing to a child of another
	// class.
	ClassID   int64           `json:"classId"`
	StudentID int64           `json:"studentId"`
	Passages  []AssignPassage `json:"passages"`
	// AppendToNoteID is the note this recording already made for StudentID,
	// when the card holds one: the rows are appended to it and no second note
	// is made. The server checks that the note exists, belongs to StudentID
	// and was made from this recording (notes.trace_id). Not for ownership:
	// a forged id lets a teacher append their own text to their own note,
	// which the edit endpoint already allows. For the replay guard: it sees
	// this recording's notes only, so an append onto any other note could
	// not be found on a retry, and the rows would be written twice.
	AppendToNoteID int64 `json:"appendToNoteId,omitempty"`
}

// AssignPassage is one passage as the card sends it back: what kind it was,
// and the words. Nothing else — spoken labels and the student are the
// server's business, and the server does not read them here.
type AssignPassage struct {
	Kind    PassageKind `json:"kind"`
	Summary string      `json:"summary"`
}

// AssignPassagesResponse is the note link the call made, in the shape the card
// already merges into its note links, plus whether it was an append.
type AssignPassagesResponse struct {
	NoteID    int64  `json:"noteId"`
	StudentID int64  `json:"studentId"`
	Name      string `json:"name"`
	ClassName string `json:"className"`
	// Appended says the rows joined the note the card named in
	// appendToNoteId; false means a note was created. A replay of an append
	// answers with the grown note and says true too.
	Appended bool `json:"appended"`
}

// handleAssignPassages files ticked passages to one child: a new note, or an
// append to the note the card already holds for that child.
//
// Order matters, and it is the assemble handler's:
//
//   - Every ownership gate runs before the lock and before any read a write
//     depends on: the recording (bare 404 either way, so a probe learns
//     nothing), the class, the student's membership of that class, the
//     transcript's presence. A body that fails validation is refused, never
//     repaired.
//   - The lock is taken after the checks and held across the note write and
//     the job update. A double-click gets 409 at once.
//   - The note named for an append is read inside the lock, so its text is
//     the text the append lands on: nothing else can move it between the
//     read and the write. A note that is not the student's is a 404 before
//     any write.
//   - A replay makes no second note (#136). Inside the lock, before any
//     write, the child's notes from this recording are read; one holding the
//     sent text is answered with 200 and its link. Covers what the lock
//     cannot: a retry after a lost response, a second tab, a reload then a
//     re-pick.
//   - The job is read inside the lock: a mid-run or failed job is refused,
//     and a done one gets the link appended on a create, nothing more. An
//     append writes nothing to the job: the link is already there. No passage
//     state, no class, no review state moves. Review
//     lives on the card; the link is the filing result, and a reload rebuilds
//     the card from the job, so the job has to carry it or the count is wrong,
//     the class picker comes back, and a re-pick on a declined recording
//     makes a second note for a child who has one.
func handleAssignPassages(w http.ResponseWriter, r *http.Request) {
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

	// The passages are bounded by the transcript, and the transcript by
	// maxTextSize; the envelope allowance is text_notes.go's.
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxTextSize)+1024)
	var req AssignPassagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.ClassID <= 0 || req.StudentID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "classId and studentId are required"})
		return
	}
	if req.AppendToNoteID < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "appendToNoteId must be a note id"})
		return
	}

	// Ownership, part one: the recording. GetByID is unscoped, so the user
	// match is the gate, and it runs first so a caller probing another
	// tenant's upload id always gets the same body whatever else they send.
	row, err := serviceDeps.GetVoiceNoteRepo().GetByID(r.Context(), uploadID)
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "recording not found"})
		return
	case err != nil:
		writeInternalError(w, r, err)
		return
	case row.UserID != userID:
		log.Warn("assign passages: recording not owned", "upload_id", uploadID, "user_id", userID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "recording not found"})
		return
	}

	// Part two: the class, scanned over the caller's own list.
	class, err := ownedClass(r.Context(), req.ClassID, userID)
	switch {
	case errors.Is(err, ErrNotFound):
		log.Warn("assign passages: class not owned", "upload_id", uploadID, "user_id", userID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "class not found"})
		return
	case err != nil:
		writeInternalError(w, r, err)
		return
	}

	// Part three: the student is on that class. Membership, not just
	// ownership: the roster the card offered is the one the note is filed
	// against, and a child of another class must not be reachable through it.
	student, err := serviceDeps.GetStudentRepo().GetByID(r.Context(), req.StudentID)
	switch {
	case errors.Is(err, ErrNotFound), err == nil && student.ClassID != req.ClassID:
		log.Warn("assign passages: student not on class", "upload_id", uploadID, "user_id", userID, "student_id", req.StudentID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "student not found"})
		return
	case err != nil:
		writeInternalError(w, r, err)
		return
	}

	if row.Transcript == nil || *row.Transcript == "" {
		// A job that failed before transcription, or a row the retention
		// cleanup emptied. Same as assemble: nothing to file a note against.
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this recording has no transcript"})
		return
	}
	transcript := *row.Transcript

	noteDate, err := uploadDay(row.CreatedAt)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	own, group, kindCounts, err := splitAssignPassages(req.Passages, transcript)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	extractor, err := serviceDeps.GetExtractor()
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	// One write at a time per recording. Immediately, not a blocking wait: a
	// double-click gets told no rather than making a second note.
	key := voiceNoteKey(userID, uploadID)
	if !takeUploadLock(key) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this recording is already being filed"})
		return
	}
	defer releaseUploadLock(key)

	// The note an append lands on, read inside the lock so the text it holds
	// is the text the write extends. Missing, another child's, or another
	// recording's, is a 404 and nothing is written. The card can only have
	// sent an id it holds a link for, and every link is this recording's;
	// the recording check is for the guard below, which finds a replay among
	// this recording's notes and nowhere else.
	var appendTo *Note
	if req.AppendToNoteID != 0 {
		n, err := serviceDeps.GetNoteRepo().GetByID(r.Context(), req.AppendToNoteID)
		switch {
		case errors.Is(err, ErrNotFound),
			err == nil && n.StudentID != student.ID,
			err == nil && (n.TraceID == nil || *n.TraceID != row.TraceID):
			log.Warn("assign passages: note not the student's", "upload_id", uploadID, "user_id", userID, "student_id", student.ID, "note_id", req.AppendToNoteID)
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
			return
		case err != nil:
			writeInternalError(w, r, err)
			return
		}
		appendTo = &n
	}

	// The job, read once inside the lock. Two of assemble's refusals apply
	// and one does not. A job mid-run would have the pipeline's closing write
	// replace the link list seconds later, and a failed one's route is retry,
	// which rebuilds the job the same way: the note would survive, the card
	// would lose it, and the child might get the pipeline's copy too. "Already
	// has notes" is not copied: a card with one child filed and two rows open
	// is the case this endpoint exists for. Not reachable from the card, which
	// renders these controls on a done card only; this is the API's own line.
	// A gone job is fine: after a restart the card is gone on the next reload
	// too, and nothing is lost.
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

	// Duplicate guard, by text: the server holds no passage list to index
	// into. A replay resends the strings the note was built from, stored
	// verbatim, so a note holding them is that call's note. The card merges
	// links by noteId, so the reply is a create's shape. Misses after an edit
	// (a second note is then fair) and after a restart (a re-pick makes new
	// rows). Inside the lock, so a double-click cannot slip between check
	// and write. A replay of an append hits the note it grew, and the reply
	// says appended, as the lost one did; a hit on some other note is
	// reported as the create it was.
	if hit, err := findAssignedNote(r.Context(), student.ID, row.TraceID, own, group); err != nil {
		writeInternalError(w, r, err)
		return
	} else if hit != nil {
		appended := appendTo != nil && hit.ID == appendTo.ID
		matched := len(own) + len(group)
		if appended {
			matched = len(own)
		}
		logPassageRecovered(log, appended, true, kindCounts, matched, userID, uploadID, row.TraceID, student.ID, extractor.Model())
		writeJSON(w, http.StatusOK, AssignPassagesResponse{
			NoteID:    hit.ID,
			StudentID: student.ID,
			Name:      student.Name,
			ClassName: class.Name,
			Appended:  appended,
		})
		return
	}

	var link NoteLink
	written := len(own) + len(group)
	if appendTo != nil {
		// The rows only, after a blank line, in the order sent. Group text is
		// dropped: the pipeline note already holds it, and a second copy
		// would say "everyone worked hard" twice. The source stays what it
		// was, so a later edit of an auto note still fires the implicit
		// thumbs-down: the model wrote most of it. No feedback record here;
		// the teacher is filing, not correcting.
		written = len(own)
		text := appendTo.Summary + "\n\n" + joinPassageText(own, nil)
		if err := serviceDeps.GetNoteRepo().Update(r.Context(), appendTo.ID, text); err != nil {
			writeInternalError(w, r, err)
			return
		}
		link = NoteLink{Name: student.Name, NoteID: appendTo.ID, StudentID: student.ID, ClassName: class.Name}
	} else {
		// The extractor's model, as the pipeline and assemble stamp it: it
		// produced the words, it only missed the assignment. The job carries
		// no version, and the window for a mismatch is the card's lifetime.
		result, err := serviceDeps.GetNoteCreator().CreateNote(r.Context(), CreateNoteRequest{
			StudentID:    student.ID,
			StudentName:  student.Name,
			QuotedText:   joinPassageText(own, group),
			Transcript:   transcript,
			Date:         noteDate,
			ModelVersion: extractor.Model(),
			Source:       NoteSourceAssigned,
			TraceID:      row.TraceID,
		})
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		link = NoteLink{Name: student.Name, NoteID: result.NoteID, StudentID: student.ID, ClassName: class.Name}

		// The link goes on the queued job, as assemble writes its links at
		// the end of its handler. Nothing else on the job moves. An append
		// skips this: the link is already there.
		if job != nil {
			job.NoteLinks = append(job.NoteLinks, link)
			if err := queue.UpdateJob(r.Context(), *job); err != nil {
				// The note exists; only the card's next poll is stale.
				log.Warn("assign passages: could not update queued job", "upload_id", uploadID, "error", err)
			}
		}
	}

	logPassageRecovered(log, appendTo != nil, false, kindCounts, written, userID, uploadID, row.TraceID, student.ID, extractor.Model())

	writeJSON(w, http.StatusOK, AssignPassagesResponse{
		NoteID:    link.NoteID,
		StudentID: link.StudentID,
		Name:      link.Name,
		ClassName: link.ClassName,
		Appended:  appendTo != nil,
	})
}

// splitAssignPassages validates the passages the card sent and sorts them into
// the child's own and the recording's class-wide ones, in the order sent.
//
// Every rule refuses rather than repairs: a kind outside the three the card
// can hold, an empty summary, a summary longer than the transcript it claims
// to summarise, or nothing but group passages — a note that says only
// "everyone did well" is not a filing of anything that reached nobody. There
// is no `none`: those are dropped at assembly and never on the wire.
//
// The length cap is the whole of the text check. A substring rule would fail
// the first time the model rewrote a sentence, which is what a summary is.
func splitAssignPassages(passages []AssignPassage, transcript string) (own, group []string, kindCounts map[PassageKind]int, err error) {
	kindCounts = map[PassageKind]int{}
	for _, p := range passages {
		switch p.Kind {
		case PassageChild, PassageUnknown, PassageGroup:
		default:
			return nil, nil, nil, errors.New("passage kind must be child, unknown or group")
		}
		if strings.TrimSpace(p.Summary) == "" {
			return nil, nil, nil, errors.New("passage summary is required")
		}
		if len(p.Summary) > len(transcript) {
			return nil, nil, nil, errors.New("passage summary is longer than the recording")
		}
		kindCounts[p.Kind]++
		if p.Kind == PassageGroup {
			group = append(group, p.Summary)
		} else {
			own = append(own, p.Summary)
		}
	}
	if len(own) == 0 {
		return nil, nil, nil, errors.New("at least one child or unknown passage is required")
	}
	return own, group, kindCounts, nil
}

// logPassageRecovered writes the recovery record: create, append, or replay
// hit. Same pinned query string as the pipeline's and the class picker's, so
// Sentry sees every route a passage reaches a child by. One writer, so no
// field lands on one path only. No name, no text (docs/adr/0003). kind_counts
// is what the card sent; passage_count is what the note gained, or on a hit
// what the lost call's did: group text is not counted on an append.
func logPassageRecovered(log *slog.Logger, appended, duplicate bool, kindCounts map[PassageKind]int, passageCount int, userID string, uploadID int64, traceID string, studentID int64, model string) {
	log.Info("process voice note: passage recovered",
		"route", "manual",
		"appended", appended,
		"duplicate", duplicate,
		"kind_counts", kindCounts,
		"passage_count", passageCount,
		"user_id", userID, "upload_id", uploadID, "trace_id", traceID, "student_id", studentID,
		"model", model, "prompt_hash", ExtractionPromptHash)
}

// findAssignedNote returns the note an earlier call with these passages made,
// or nil. Scope: the child's notes from this recording, by trace id. Not the
// day: two recordings on one day would share a scope, and an append's note
// dated by job dispatch could sit across a UTC midnight from the row.
//
// A hit holds the own passages as one block, joined as written, and each
// group passage anywhere. The block, not the words one by one: the pipeline's
// note for the same child already carries the group text and comes from the
// same transcript, so a scattered match could answer a fresh filing with the
// wrong note. Group passages stay loose because an append (#135) adds own
// text only. A single own passage is its own block, so the one-row case keeps
// that collision; nothing tighter survives the append.
//
// Containment, not equality: a note grown by an append still holds the block.
// A rewritten one does not, and a fresh note is then right. When several hold
// it (file A, then A and B, then replay A), the earliest is that call's.
func findAssignedNote(ctx context.Context, studentID int64, traceID string, own, group []string) (*Note, error) {
	notes, err := serviceDeps.GetNoteRepo().ListForRecording(ctx, studentID, traceID)
	if err != nil {
		return nil, err
	}
	block := joinPassageText(own, nil)
	var hit *Note
	for i := range notes {
		n := &notes[i]
		if !strings.Contains(n.Summary, block) || !holdsEvery(n.Summary, group) {
			continue
		}
		if hit == nil || n.ID < hit.ID {
			hit = n
		}
	}
	return hit, nil
}

func holdsEvery(text string, passages []string) bool {
	for _, p := range passages {
		if !strings.Contains(text, p) {
			return false
		}
	}
	return true
}
