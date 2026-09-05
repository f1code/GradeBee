// voice_note_job.go defines the VoiceNoteJob type and status constants
// for async voice note processing (transcribe → extract → create notes).
package handler

import (
	"fmt"
	"time"
)

// Job status constants for voice note processing.
const (
	JobStatusQueued        = "queued"
	JobStatusTranscribing  = "transcribing"
	JobStatusExtracting    = "extracting"
	JobStatusCreatingNotes = "creating_notes"
	JobStatusDone          = "done"
	JobStatusFailed        = "failed"
)

// A job can finish with no note for three different reasons, and the teacher
// can only act on two of them. Without this the done card says "No notes
// created" for all three, which reads as "nothing was in the recording" —
// wrong, and the wrong thing to do next.
//
// The class one is the reason this exists: a recording the model cannot pin to
// a class leaves every name with no roster to resolve against. The single-call
// extractor never sets it — its schema constrains class_name to an enum of the
// teacher's own classes, so a class is always pinned — and the constant is on
// the wire for the two-pass contract (#125) that can decline. The class picker
// does not wait for it; see noNotesReason below.
const (
	// NoNotesNobodyNamed: the recording named no child at all. Nothing to do.
	NoNotesNobodyNamed = "nobody_named"
	// NoNotesClassUnclear: children were named, but no class was pinned, so
	// there was no roster to resolve them against. Saying the class and time
	// at the start of the next recording fixes it.
	NoNotesClassUnclear = "class_unclear"
	// NoNotesNoNameMatched: every spoken name missed the roster. An alias fixes
	// the recurring ones; picking the class fixes a recording read against the
	// wrong roster.
	NoNotesNoNameMatched = "no_name_matched"
)

// NoteLink pairs a student name with the ID of the created note.
type NoteLink struct {
	Name      string `json:"name"`
	NoteID    int64  `json:"noteId"`
	StudentID int64  `json:"studentId"`
	ClassName string `json:"className"`
}

// PassageKind says what a passage is about. The single-call extractor produces
// only child passages; the kind is on the wire because #125's contract adds a
// group passage and the card must not need a new field to render it.
type PassageKind string

// PassageChild is a passage about one named child.
const PassageChild PassageKind = "child"

// JobPassage is one stretch of the recording as the pipeline read it, on the
// wire for the done card. It is a placeholder that #125 will own: it carries
// what the class picker has to hand back and nothing else, so no consumer
// depends on a shape the two-pass contract is going to replace.
type JobPassage struct {
	Kind PassageKind `json:"kind"`
	// SpokenLabels is each name this passage is about, as the extraction model
	// wrote it. The picker hands them straight back to
	// POST /api/voice-notes/{uploadId}/assemble, which re-resolves them against
	// a class the teacher chose — without them the second run has nothing to
	// match. They go to the teacher who spoke them, never to telemetry
	// (docs/adr/0003).
	SpokenLabels []string `json:"spokenLabels,omitempty"`
	// Student is the roster name the passage reached, empty when it reached
	// nobody. That is what makes a recording pickable: every passage empty
	// means no child got a note.
	Student string `json:"student,omitempty"`
	// Summary is the text a note built from this passage holds.
	Summary string `json:"summary"`
}

// VoiceNoteJob represents an async voice note processing job.
type VoiceNoteJob struct {
	UserID     string     `json:"userId"`
	UploadID   int64      `json:"uploadId"`
	FilePath   string     `json:"filePath"`
	FileName   string     `json:"fileName"`
	MimeType   string     `json:"mimeType"`
	Source     string     `json:"source"`
	Transcript string     `json:"transcript,omitempty"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	NoteLinks  []NoteLink `json:"noteLinks,omitempty"`
	// ClassName is the class the notes were filed under; "" when none was. It
	// and Passages are set at completion and absent on jobs done before they
	// existed.
	ClassName string       `json:"className,omitempty"`
	Passages  []JobPassage `json:"passages,omitempty"`
	// NoNotesReason is set only on a done job that created no note, to one of
	// the NoNotes* constants. Empty whenever a note was created.
	NoNotesReason string     `json:"noNotesReason,omitempty"`
	Error         string     `json:"error,omitempty"`
	FailedAt      *time.Time `json:"failedAt,omitempty"`
}

// JobKey implements Keyed.
func (j VoiceNoteJob) JobKey() string { return voiceNoteKey(j.UserID, j.UploadID) }

// OwnerID implements Keyed.
func (j VoiceNoteJob) OwnerID() string { return j.UserID }

// voiceNoteKey builds a job key from user ID and upload ID.
// Used by handlers that receive these values separately.
func voiceNoteKey(userID string, uploadID int64) string {
	return fmt.Sprintf("%s/%d", userID, uploadID)
}
