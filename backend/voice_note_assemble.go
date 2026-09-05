// voice_note_assemble.go implements POST /api/voice-notes/{uploadId}/assemble:
// the teacher picks the class the recording should have been read against, and
// the notes the pipeline would have made then exist (#115).
//
// No model call. The passages come back from the done card exactly as the
// extraction returned them; only the class is new, so the work is re-resolving
// each spoken label against that class's roster — the same MatchStudent the
// rest of the pipeline resolves with.
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// AssembleNotesRequest is the body of the assemble call: the class the teacher
// picked, and the card's own passages handed straight back.
//
// The passages are JobPassage, not a parallel request type. Both sides of this
// API are camelCase, so the type the card received is the type it can post —
// a second type would exist only to be converted to the first.
type AssembleNotesRequest struct {
	ClassName string       `json:"className"`
	Passages  []JobPassage `json:"passages"`
}

// AssembleNotesResponse is the done card, repainted. It carries what the job
// JSON carries at completion, so the card renders as a normal done card whether
// or not the job is still in the queue.
type AssembleNotesResponse struct {
	ClassName string       `json:"className"`
	NoteLinks []NoteLink   `json:"noteLinks"`
	Passages  []JobPassage `json:"passages"`
	// NoNotesReason is empty once a note exists; a pick whose names all miss
	// the chosen roster comes back no_name_matched.
	NoNotesReason string `json:"noNotesReason,omitempty"`
}

// handleAssembleNotes re-runs assembly for one recording against a class the
// teacher picked.
//
// The transcript is read from the voice_notes row, never from the body: it is
// what every created note stores, and a body-supplied one would let a caller
// file arbitrary text under another teacher's wording.
//
// The passages are not held to that, and the asymmetry is worth naming. Each
// passage's summary comes from the body and becomes the note's visible text,
// written with source `reviewed` and stamped with the extraction model's id —
// so a caller can put words the model never wrote behind the model's name, and
// editing them away then feeds the implicit thumbs-down (notes.go,
// isModelWritten). The blast radius is the caller's own notes, which is why
// this ships: there is nowhere to store passages to check against in v1. #127
// closes it by re-running extraction's second pass against the picked class
// (Extractor.ExtractPassages) instead of trusting the body.
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

	var req AssembleNotesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClassName == "" || len(req.Passages) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "className and passages are required"})
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
	// roster, and it yields the students the labels resolve against — so the
	// gate and the matching cannot disagree about which class this is.
	classes, err := serviceDeps.GetRoster(r.Context(), userID).Students(r.Context())
	if err != nil {
		writeInternalError(w, r, err)
		return
	}
	students, ok := classStudents(classes, req.ClassName)
	if !ok {
		log.Warn("assemble notes: class not owned", "upload_id", uploadID, "user_id", userID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "class not found"})
		return
	}

	// Three refusals, all only while the job is in memory: with no passage
	// storage there is nothing left to check once it is gone.
	//
	// Every read is advisory. Nothing holds a lock across the note loop and the
	// UpdateJob at the end, so two posts racing each other both see a clean job
	// and both create a full set of notes, and a failure part-way through the
	// loop leaves notes created with the job never updated, so the next pick
	// doubles them. At single-digit picks a month that is the accepted trade;
	// the fix is a per-upload lock, or a passages table.
	queue, queueErr := serviceDeps.GetVoiceNoteQueue()
	var job *VoiceNoteJob
	if queueErr == nil {
		if j, err := queue.GetJob(r.Context(), voiceNoteKey(userID, uploadID)); err == nil {
			job = j
		}
	}
	switch {
	case job == nil:
		// Not in the queue: a restart, or a dismissed card still open in a tab.
	case job.Status == JobStatusFailed:
		// A failed job's route is retry, not a class: the pipeline gave up
		// somewhere, and re-running it is what tells the teacher where.
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

	resolved := resolvePassages(req.Passages, students)

	extractor, err := serviceDeps.GetExtractor()
	if err != nil {
		writeInternalError(w, r, err)
		return
	}

	// The note loop mirrors voice_note_process.go: MatchStudent returns
	// canonical roster names, so FindByNameAndClass is an ID read, not a second
	// matcher. A name that does not resolve is dropped, exactly as there — the
	// roster read and this lookup can disagree if a student was just deleted.
	studentRepo := serviceDeps.GetStudentRepo()
	noteCreator := serviceDeps.GetNoteCreator()
	noteLinks := []NoteLink{}
	for _, rp := range resolved {
		studentID, err := studentRepo.FindByNameAndClass(r.Context(), rp.name, req.ClassName, userID)
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
			StudentName:  rp.name,
			QuotedText:   rp.summary,
			Transcript:   transcript,
			Date:         noteDate,
			ModelVersion: extractor.Model(),
			Source:       NoteSourceReviewed,
		})
		if err != nil {
			writeInternalError(w, r, err)
			return
		}
		noteLinks = append(noteLinks, NoteLink{
			Name: rp.name, NoteID: result.NoteID,
			StudentID: studentID, ClassName: req.ClassName,
		})

		// One record per recovered note, keyed on the exact string
		// "process voice note: passage recovered" — the Sentry readout filters
		// on it. No name, no text (docs/adr/0003); passage_count says how much
		// of the recording reached this child.
		log.Info("process voice note: passage recovered",
			"route", "class_picker",
			"passage_count", rp.passages,
			"user_id", userID, "upload_id", uploadID, "student_id", studentID,
			"model", extractor.Model(), "prompt_hash", ExtractionPromptHash)
	}

	painted := repaintPassages(req.Passages, students)
	resp := AssembleNotesResponse{
		ClassName:     req.ClassName,
		NoteLinks:     noteLinks,
		Passages:      painted,
		NoNotesReason: noNotesReason(len(noteLinks), painted),
	}

	// If the job is still queued, the card's next poll must agree with what was
	// just created — otherwise a refresh shows the picker again over notes that
	// already exist.
	//
	// Only when it created something. A pick that resolved nobody leaves the
	// job exactly as it was, which is the truth: no class is filed against this
	// recording and nothing came of the attempt. Writing the class on anyway
	// would leave a reloaded card with no picker and no way back — and the
	// wrong sibling class is the mistake this endpoint exists to undo.
	if job != nil && len(noteLinks) > 0 {
		job.ClassName = resp.ClassName
		job.NoteLinks = resp.NoteLinks
		job.Passages = resp.Passages
		job.NoNotesReason = resp.NoNotesReason
		if err := queue.UpdateJob(r.Context(), *job); err != nil {
			// The notes exist; only the card is stale. Say so and return them.
			log.Warn("assemble notes: could not update queued job", "upload_id", uploadID, "error", err)
		}
	}

	log.Info("assemble notes",
		"user_id", userID, "upload_id", uploadID,
		"note_count", len(noteLinks),
		"passage_count", len(req.Passages),
		"queued", job != nil,
		"model", extractor.Model(), "prompt_hash", ExtractionPromptHash)
	writeJSON(w, http.StatusOK, resp)
}

// resolvedPassage is one student the picked class's roster answered for, with
// everything the recording said about them.
type resolvedPassage struct {
	name     string
	summary  string
	passages int
}

// resolvePassages resolves every spoken label against one class's roster and
// groups what the recording said by the child it reached.
//
// It is the picker's own resolution path, and it is not the pipeline's: a
// group passage carries no spoken label, so the loop below skips it and the
// class-wide observation the pipeline fans out to every child is silently lost
// on a re-pick. #127 removes the asymmetry by re-running pass 2 against the
// picked class and calling assemblePassages, the way the pipeline does.
//
// Grouping is what stops a child getting two notes from one recording. Order
// follows first mention, so the notes come out in the order the teacher spoke.
func resolvePassages(passages []JobPassage, students []ClassStudent) []resolvedPassage {
	var out []resolvedPassage
	at := map[string]int{}
	for _, p := range passages {
		for _, label := range p.SpokenLabels {
			name, ok := MatchStudent(label, students)
			if !ok {
				continue
			}
			i, seen := at[name]
			if !seen {
				at[name] = len(out)
				out = append(out, resolvedPassage{name: name, summary: p.Summary, passages: 1})
				continue
			}
			// Blank line between passages: they are separate stretches of
			// speech, and running them together invents a sentence the teacher
			// never said.
			out[i].summary += "\n\n" + p.Summary
			out[i].passages++
		}
	}
	return out
}

// repaintPassages stamps each passage with the child it reached under the
// picked class, so the card the teacher gets back says who each stretch of the
// recording went to. A passage that reached nobody keeps an empty Student.
func repaintPassages(passages []JobPassage, students []ClassStudent) []JobPassage {
	out := make([]JobPassage, len(passages))
	for i, p := range passages {
		p.Student = ""
		for _, label := range p.SpokenLabels {
			if name, ok := MatchStudent(label, students); ok {
				p.Student = name
				break
			}
		}
		out[i] = p
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

// classStudents returns one class's roster by exact name, which is how every
// class_name on this API is compared.
func classStudents(classes []ClassGroup, name string) ([]ClassStudent, bool) {
	for _, c := range classes {
		if c.Name == name {
			return c.Students, true
		}
	}
	return nil, false
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
// NoNotesClassUnclear is not reachable from here or from the pipeline: pass 1's
// schema constrains the class to an enum of the teacher's own classes with no
// "", so a class is always pinned and the model cannot decline. #127 adds that
// one enum value and this becomes reachable; the constant and the card's
// message for it are already on the wire so that lands as a value, not as a
// feature.
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

// anySpokenLabel reports whether the recording spoke a name for anybody.
//
// hasSpokenName, not a length check: it is the same rule the pronoun guard
// applies (extract.go), so the two cannot drift. The guard only inspects child
// passages, and nothing makes the model obey the prompt's "empty list for
// unknown, group and none" — a group passage that came back labelled "She"
// would otherwise count as a name here, offer the class picker, and be
// stop-listed by MatchStudent the moment the teacher picked. Which is the
// futile picker this predicate exists to prevent.
func anySpokenLabel(passages []JobPassage) bool {
	for _, p := range passages {
		if hasSpokenName(p.SpokenLabels) {
			return true
		}
	}
	return false
}
