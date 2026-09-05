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
// a class leaves every name with no roster to resolve against. Pass 1 declines
// by returning "" from an enum that carries it, and the pipeline sets this
// reason off that empty class name — not through noNotesReason, which sees no
// passages on a decline and would answer nobody_named.
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

// PassageKind says what a passage is about. Extraction returns all four, and
// what the pipeline does with each is in voice_note_process.go.
type PassageKind string

const (
	// PassageChild: the teacher is talking about one child and says who.
	PassageChild PassageKind = "child"
	// PassageUnknown: the teacher is talking about one child, but no name was
	// spoken for them — only a pronoun, or a name matching nobody on the
	// class's roster. Its summary reaches the unattributed list, never a note.
	PassageUnknown PassageKind = "unknown"
	// PassageGroup: a statement about the class as a whole. It joins the note
	// of every child this recording already reached.
	PassageGroup PassageKind = "group"
	// PassageNone: not an observation about children — the spoken header, a
	// greeting, thinking aloud. Dropped at assembly and never put on a job, so
	// a recording holding nothing but a header still reads as nobody named
	// rather than offering the class picker over one empty passage.
	PassageNone PassageKind = "none"
)

// JobPassage is one stretch of the recording as the pipeline read it, on the
// wire for the done card. It is ExtractedPassage in camelCase, minus nothing:
// the two types are separate because one is the model's contract and the other
// is this API's, and a single struct cannot carry both spellings.
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
	// the NoNotes* constants. Empty whenever a note was created. It says WHY,
	// and nothing else: it is not the card's instruction about what to offer.
	NoNotesReason string `json:"noNotesReason,omitempty"`
	// CanPickClass says whether picking a class could still rescue this
	// recording, and it is the card's gate for the class picker.
	//
	// Separate from NoNotesReason on purpose. The two answer different
	// questions — a cause and an affordance — and folding them left the card
	// deciding an affordance by listing the causes it knew about, so a new
	// cause silently removed the picker. It also forced the assemble handler to
	// name a cause it could not know in order to keep the picker up. The server
	// decides this; the card obeys it.
	CanPickClass bool       `json:"canPickClass,omitempty"`
	Error        string     `json:"error,omitempty"`
	FailedAt     *time.Time `json:"failedAt,omitempty"`
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
