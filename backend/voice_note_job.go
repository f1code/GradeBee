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

// NoteLink pairs a student name with the ID of the created note.
type NoteLink struct {
	Name      string `json:"name"`
	NoteID    int64  `json:"noteId"`
	StudentID int64  `json:"studentId"`
	ClassName string `json:"className"`
}

// VoiceNoteJob represents an async voice note processing job.
type VoiceNoteJob struct {
	UserID    string     `json:"userId"`
	UploadID  int64      `json:"uploadId"`
	FilePath  string     `json:"filePath"`
	FileName  string     `json:"fileName"`
	MimeType  string     `json:"mimeType"`
	Source     string     `json:"source"`
	Transcript string     `json:"transcript,omitempty"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	NoteLinks []NoteLink `json:"noteLinks,omitempty"`
	Error     string     `json:"error,omitempty"`
	FailedAt  *time.Time `json:"failedAt,omitempty"`
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
