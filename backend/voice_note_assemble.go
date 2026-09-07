// voice_note_assemble.go implements POST /api/voice-notes/{uploadId}/assemble:
// the teacher picks the class the recording should have been read against, and
// the notes the pipeline would have made then exist (#115).
//
// It runs extraction's second pass itself (#127). Two recordings reach it and
// one call serves both: a decline, where pass 1 could not pin a class so pass 2
// never ran and this is its deferred first run; and a recording filed against
// the wrong sibling class, where pass 2 ran against the wrong roster and this
// runs it against the right one.
//
// The pass and the fold are the pipeline's own — ExtractPassages and
// assemblePassages — so there is one resolution path for a recording, not two.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// assemblePass2Timeout bounds the model call. Not the handler: a whole-handler
// deadline would cancel the note loop mid-write and newly cause the partial
// write this path only inherits.
//
// llmChatTimeout is 120s, which is past any teacher's patience behind a
// spinner. 30s turns a hung provider into "try again" rather than a card that
// looks stuck.
const assemblePass2Timeout = 30 * time.Second

// One in-flight write per recording, shared by this handler and the assign
// endpoint (voice_note_assign.go). A double-click or a client retry used to
// race two full runs through a job read that neither had written yet, and every
// child got two identical notes; a 2-3s model call in the middle makes that
// window wide enough to hit by hand.
//
// A set of keys under one mutex, not a map of *sync.Mutex: that grows an object
// per upload ever picked, and refcounting them back out is more code than the
// thing it guards. Package-level, matching how voiceNoteQueueInstance lives.
var (
	uploadLocksMu sync.Mutex
	uploadLocks   = map[string]struct{}{}
)

// takeUploadLock claims a recording, or reports that someone else holds it.
func takeUploadLock(key string) bool {
	uploadLocksMu.Lock()
	defer uploadLocksMu.Unlock()
	if _, held := uploadLocks[key]; held {
		return false
	}
	uploadLocks[key] = struct{}{}
	return true
}

func releaseUploadLock(key string) {
	uploadLocksMu.Lock()
	defer uploadLocksMu.Unlock()
	delete(uploadLocks, key)
}

// AssembleNotesRequest is the body of the assemble call: the class the teacher
// picked, and nothing else.
//
// The card no longer posts its passages back. They were the note's visible
// text, stamped with the extraction model's id under source `reviewed` — so a
// caller could put words the model never wrote behind the model's name. Running
// pass 2 here means the summaries stamped with the model's id are ones the
// model produced in this request.
type AssembleNotesRequest struct {
	ClassName string `json:"className"`
}

// AssembleNotesResponse is the done card, repainted. It carries what the job
// JSON carries at completion, so the card renders as a normal done card whether
// or not the job is still in the queue — and it says what the job says, so a
// refresh does not contradict it.
type AssembleNotesResponse struct {
	// ClassName and ClassID are the class the pick ran against, whether or
	// not it made a note: the card offers that class's roster for filing the
	// passages by hand. See assembleOutcome.
	ClassName string       `json:"className"`
	ClassID   int64        `json:"classId,omitempty"`
	NoteLinks []NoteLink   `json:"noteLinks"`
	Passages  []JobPassage `json:"passages"`
	// NoNotesReason is empty once a note exists; a pick that filed nothing comes
	// back with the reason the card already had, since nothing changed.
	NoNotesReason string `json:"noNotesReason,omitempty"`
	// CanPickClass gates the card's class picker, exactly as it does on the job.
	CanPickClass bool `json:"canPickClass,omitempty"`
}

// handleAssembleNotes runs extraction's second pass for one recording against a
// class the teacher picked, and files what it returns.
//
// The transcript is read from the voice_notes row, never from the body: it is
// what every created note stores, and a body-supplied one would let a caller
// file arbitrary text under another teacher's wording. Since #127 the same is
// true of the text of every note — it comes back from the model in this
// request, so NoteSourceReviewed is now literally true: the model wrote the
// words, the teacher supplied only the class.
//
// Order matters twice:
//
//   - Both ownership gates run before the lock and before the model call, so a
//     caller probing another tenant's upload id gets a 404 for free and cannot
//     take a lock on it.
//   - The job is read once, inside the lock, and the model call sits between
//     that read and the write. Reading outside would leave the window the lock
//     exists to close; the pass running before the first CreateNote is what
//     makes a provider error return with nothing written.
//
// A pick that made no note writes nothing, and what the teacher is told in that
// case is assembleOutcome's decision, not this function's.
func handleAssembleNotes(w http.ResponseWriter, r *http.Request) {
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

	// Go's decoder ignores unknown fields, so a tab still open from before #127
	// posts its passages, they are dropped, and the pick works.
	var req AssembleNotesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClassName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "className is required"})
		return
	}

	// Ownership, part one: the recording. GetByID is unscoped, so the user match
	// is the gate. It runs before the class scan so a caller probing another
	// tenant's upload id always gets the same body, whatever class they name.
	row, err := serviceDeps.GetVoiceNoteRepo().GetByID(r.Context(), uploadID)
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "recording not found"})
		return
	case err != nil:
		writeInternalError(w, r, err)
		return
	case row.UserID != userID:
		log.Warn("assemble notes: recording not owned", "upload_id", uploadID, "user_id", userID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "recording not found"})
		return
	}
	if row.Transcript == nil || *row.Transcript == "" {
		// A job that failed before transcription, or a row the retention
		// cleanup emptied.
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this recording has no transcript"})
		return
	}
	transcript := *row.Transcript

	// The note's date is the day the teacher recorded. voice_notes.created_at is
	// TEXT (strftime('%Y-%m-%dT%H:%M:%fZ')), so the day is its first ten
	// characters — and it is validated, because notes.date is a bare TEXT column
	// the report query compares with BETWEEN: a non-date there hides the note
	// from every report with no error at all (see migration 015).
	noteDate, err := uploadDay(row.CreatedAt)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	// Ownership, part two: the class. The scan is by name over the caller's own
	// roster, and it yields the class pass 2 runs against — so the gate and the
	// extraction cannot disagree about which class this is.
	classes, err := serviceDeps.GetRoster(r.Context(), userID).Students(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	class, ok := findClass(classes, req.ClassName)
	if !ok {
		log.Warn("assemble notes: class not owned", "upload_id", uploadID, "user_id", userID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "class not found"})
		return
	}

	extractor, err := serviceDeps.GetExtractor()
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	// One pick at a time per recording. Immediately, not a blocking wait: a
	// double-click should not sit through a 2-3s model call to be told no.
	key := voiceNoteKey(userID, uploadID)
	if !takeUploadLock(key) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this recording is already being filed"})
		return
	}
	defer releaseUploadLock(key)

	// Three refusals, all only while the job is in memory: with no passage
	// storage there is nothing left to check once it is gone. After a restart
	// job is nil, all three are skipped, and the in-process lock cannot help
	// either — pre-existing, and the fix is a passages table.
	//
	// This read is inside the lock and it is the mechanism. Read it outside and
	// two picks 100ms apart both see a clean job, both run pass 2, and every
	// child gets two identical notes; the lock alone only serialises them.
	queue, queueErr := serviceDeps.GetVoiceNoteQueue()
	var job *VoiceNoteJob
	if queueErr == nil {
		if j, err := queue.GetJob(r.Context(), key); err == nil {
			job = j
		}
	}
	switch {
	case job == nil:
		// Not in the queue: a restart, or a dismissed card still open in a tab.
	case job.Status == JobStatusFailed:
		// A failed job's route is retry, not a class: the pipeline gave up
		// somewhere, and re-running it is what tells the teacher where. A
		// decline is not a failure — it completes done with class_unclear — so
		// this does not block the recording this endpoint exists for.
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this recording did not finish — retry it first"})
		return
	case job.Status != JobStatusDone:
		// The pipeline is mid-run. The transcript is already on the row, so
		// assembly would work — and then the processor would create its own
		// notes for the same children seconds later. Not reachable from the
		// card, which renders the picker on a done job only.
		//
		// Status, not the row's processed_at: MarkProcessed failing is a logged
		// warning (voice_note_process.go), so a row can be done with it unset,
		// and refusing on that would refuse the call this endpoint exists for.
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this recording is still being processed"})
		return
	case len(job.NoteLinks) > 0:
		// It has had its notes. Not "it has a class": a pick that resolved
		// nobody sets no class and creates nothing, and that teacher must be
		// able to pick again — a wrong sibling class is the mistake this whole
		// path exists to undo.
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this recording already has notes"})
		return
	}

	// Pass 2, before any note is written. A provider error or timeout returns
	// here with nothing created and the job untouched, so the card keeps its
	// picker and the teacher retries.
	pass2Ctx, cancel := context.WithTimeout(r.Context(), assemblePass2Timeout)
	defer cancel()
	extracted, err := extractor.ExtractPassages(pass2Ctx, transcript, class)
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	// The pipeline's own fold: several passages about one child join in order,
	// a class-wide passage reaches every child the recording already reached,
	// and an unattributed one reaches nobody but stays on the card.
	notes, passages := assemblePassages(extracted)

	// No second roster check. Pass 2's schema constrains student to this class's
	// roster, and the loop below already skips and logs a name it cannot find —
	// the same as the pipeline's. A second matcher here is the divergence #127
	// deleted.
	studentRepo := serviceDeps.GetStudentRepo()
	noteCreator := serviceDeps.GetNoteCreator()
	noteLinks := []NoteLink{}
	for _, n := range notes {
		studentID, err := studentRepo.FindByNameAndClass(r.Context(), n.Name, req.ClassName, userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				log.Info("assemble notes: student vanished between roster read and lookup",
					"upload_id", uploadID, "user_id", userID, "class_name", req.ClassName)
				continue
			}
			writeInternalError(w, r, err)
			return
		}
		result, err := noteCreator.CreateNote(r.Context(), CreateNoteRequest{
			StudentID:    studentID,
			StudentName:  n.Name,
			QuotedText:   n.Summary,
			Transcript:   transcript,
			Date:         noteDate,
			ModelVersion: extractor.Model(),
			Source:       NoteSourceReviewed,
			TraceID:      row.TraceID,
		})
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		noteLinks = append(noteLinks, NoteLink{
			Name: n.Name, NoteID: result.NoteID,
			StudentID: studentID, ClassName: req.ClassName,
		})

		// One record per recovered note, keyed on the exact string
		// "process voice note: passage recovered" — the Sentry readout filters
		// on it. No name, no text (docs/adr/0003); passage_count says how much
		// of the recording reached this child.
		log.Info("process voice note: passage recovered",
			"route", "class_picker",
			"passage_count", n.Passages,
			"user_id", userID, "upload_id", uploadID, "trace_id", row.TraceID, "student_id", studentID,
			"model", extractor.Model(), "prompt_hash", ExtractionPromptHash)
	}

	// What this run produced, with no reason attached. assembleOutcome decides
	// what the teacher is told; it is given no access to the extraction, which
	// is what stops a reason being derived from a run that cannot support one.
	ran := AssembleNotesResponse{
		ClassName: req.ClassName,
		ClassID:   class.ID,
		NoteLinks: noteLinks,
		Passages:  passages,
	}

	resp := assembleOutcome(job, ran)
	if len(noteLinks) > 0 && job != nil {
		job.ClassName = resp.ClassName
		job.ClassID = resp.ClassID
		job.NoteLinks = resp.NoteLinks
		job.Passages = resp.Passages
		job.NoNotesReason = resp.NoNotesReason
		job.CanPickClass = resp.CanPickClass
		if err := queue.UpdateJob(r.Context(), *job); err != nil {
			// The notes exist; only the card is stale. Say so and return them.
			log.Warn("assemble notes: could not update queued job", "upload_id", uploadID, "error", err)
		}
	}

	// The same per-kind breakdown the pipeline's completion record carries
	// (voice_note_process.go). This is the second place pass 2 runs, and the one
	// where it works against a class a human chose; a breakdown on one path and
	// not the other makes the Sentry readout lie by omission.
	kinds := countKinds(extracted)
	log.Info("assemble notes",
		"user_id", userID, "upload_id", uploadID, "trace_id", row.TraceID,
		"note_count", len(noteLinks),
		"passages_total", len(extracted),
		"passages_child", kinds[PassageChild],
		"passages_unknown", kinds[PassageUnknown],
		"passages_group", kinds[PassageGroup],
		"passages_none", kinds[PassageNone],
		"no_notes_reason", resp.NoNotesReason,
		"queued", job != nil,
		"model", extractor.Model(), "prompt_hash", ExtractionPromptHash)
	writeJSON(w, http.StatusOK, resp)
}

// assembleOutcome turns what a pick produced into what the teacher is told. It
// splits fact from judgement: the class picked and the passages the run
// returned are facts, and every outcome reports them; the reason and whether
// a class could still be picked are judgements, and only the pipeline, which
// had the pinned class, may make them.
//
// It never sees the extraction itself, only its fold, and it never derives a
// reason from that. Pass 2 against the wrong roster returns a name fitting no
// listed child as an unlabelled unknown, indistinguishable here from a
// recording that named nobody, so a reason read off this run would sooner or
// later close a picker that should have stayed open. Making the function blind
// to the extraction is what keeps the rule from being a comment somebody has
// to remember.
//
// Three outcomes, and ClassName, ClassID and Passages come from the run in all
// three:
//
//   - notes created → what the run produced, and the caller writes it. No
//     reason, and no picker: the recording is filed.
//   - no notes, job known → the job's reason and gate, unchanged. The pick
//     settled nothing there, and a card whose reason changes now only to change
//     back on the next poll is worse than one that says nothing new. The caller
//     leaves the job unwritten.
//   - no notes, job forgotten → no cause it cannot know, and the picker stays
//     up, because a class the teacher picks may still rescue the recording and
//     this response outlives the poll on their card.
//
// Reporting the pick on a no-note outcome is what lets a declined recording be
// filed by hand: it holds no passages of its own, so the run's are the only
// rows the teacher can file, and the picked class is the only roster to file
// them to. If the pick was the wrong sibling class, the rows show names as
// unlabelled unknowns against the wrong roster; the teacher reads the summaries
// and picks again, which the picker staying up allows.
func assembleOutcome(job *VoiceNoteJob, ran AssembleNotesResponse) AssembleNotesResponse {
	out := ran
	if out.NoteLinks == nil {
		out.NoteLinks = []NoteLink{}
	}
	switch {
	case len(ran.NoteLinks) > 0:
		out.NoNotesReason = ""
		out.CanPickClass = false
	case job != nil:
		out.NoNotesReason = job.NoNotesReason
		out.CanPickClass = job.CanPickClass
	default:
		out.NoNotesReason = ""
		out.CanPickClass = true
	}
	return out
}

// uploadDay reads the recording day out of a voice_notes.created_at value. The
// column is TEXT written by strftime('%Y-%m-%dT%H:%M:%fZ','now'), so the day is
// the first ten characters; a hand-seeded row could hold something else, and a
// note dated with something else is invisible to every report.
func uploadDay(createdAt string) (string, error) {
	if len(createdAt) < len(time.DateOnly) {
		return "", fmt.Errorf("voice note created_at %q is not a timestamp", createdAt)
	}
	day := createdAt[:len(time.DateOnly)]
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return "", fmt.Errorf("voice note created_at %q does not start with a date: %w", createdAt, err)
	}
	return day, nil
}

// noNotesReason says why a done recording holds no note, or "" when it holds
// one.
//
// The question it answers is not "were there passages" but "did the recording
// speak a name". A passage with no spoken label — an unknown block, a
// class-wide statement — is not a name that missed the roster, and saying so
// would tell the teacher to fix an alias that does not exist. It also gates the
// class picker (JobStatus.tsx): only a spoken name can resolve differently
// against a class the teacher picks, so a recording that spoke none has nothing
// for a pick to do, and offering one would be a button that cannot work.
//
// NoNotesClassUnclear never comes from here. A decline has no passages at all,
// so this would answer nobody_named and suppress the picker on exactly the
// recording that needs it; the pipeline sets that reason directly off pass 1's
// empty class name (voice_note_process.go).
func noNotesReason(noteCount int, passages []JobPassage) string {
	switch {
	case noteCount > 0:
		return ""
	case !anySpokenLabel(passages):
		return NoNotesNobodyNamed
	default:
		return NoNotesNoNameMatched
	}
}

// canPickClass says whether a class the teacher picks could still make notes
// from this recording, given why it made none.
//
// Not derivable on the assemble path, which is the point of it being computed
// once by the pipeline and carried: that handler cannot tell a recording that
// named nobody from one read against the wrong roster, because pass 2 returns
// an off-roster name as an unlabelled unknown either way.
func canPickClass(reason string) bool {
	return reason == NoNotesClassUnclear || reason == NoNotesNoNameMatched
}

// anySpokenLabel reports whether the recording spoke a name for anybody.
//
// hasSpokenName, not a length check: it is the same rule the pronoun guard
// applies (extract.go), so the two cannot drift. The guard only inspects child
// passages, and nothing makes the model obey the prompt's "empty list for
// unknown, group and none" — a group passage that came back labelled "She"
// would otherwise count as a name here and offer a class picker with nothing
// for a pick to resolve.
//
// It is a signal, not a verdict. The converse does not hold: no spoken name
// does NOT mean the recording named nobody, because a name fitting no listed
// child comes back as unknown with no labels. Only the pipeline, which has the
// pinned class, may act on this reason; the picker (voice_note_assemble.go)
// must not treat it as terminal.
func anySpokenLabel(passages []JobPassage) bool {
	for _, p := range passages {
		if hasSpokenName(p.SpokenLabels) {
			return true
		}
	}
	return false
}
