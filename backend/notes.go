// notes.go implements the NoteCreator interface that creates Google Docs
// for student observation notes in a class/student subfolder hierarchy.
package handler

import (
	"context"
	"fmt"

	"google.golang.org/api/docs/v1"
	"google.golang.org/api/drive/v3"
)

// NoteCreator creates note documents and manages the folder hierarchy.
type NoteCreator interface {
	CreateNote(ctx context.Context, req CreateNoteRequest) (*CreateNoteResponse, error)
}

// CreateNoteRequest is the input for creating a single student note.
type CreateNoteRequest struct {
	NotesRootID string // root notes folder ID
	StudentName string
	ClassName   string
	Summary     string
	Transcript  string
	Date        string // YYYY-MM-DD
}

// CreateNoteResponse contains the created document info.
type CreateNoteResponse struct {
	DocID  string `json:"docId"`
	DocURL string `json:"docUrl"`
}

// driveNoteCreator creates Google Docs in a class/student subfolder hierarchy.
type driveNoteCreator struct {
	drive    *drive.Service
	docs     *docs.Service
	metaIdx  MetadataIndex
}

func newDriveNoteCreator(driveSvc *drive.Service, docsSvc *docs.Service, metaIdx MetadataIndex) *driveNoteCreator {
	return &driveNoteCreator{drive: driveSvc, docs: docsSvc, metaIdx: metaIdx}
}

func (c *driveNoteCreator) CreateNote(ctx context.Context, req CreateNoteRequest) (*CreateNoteResponse, error) {
	// Resolve or create class subfolder.
	classFolderID, err := findOrCreateDriveFolder(ctx, c.drive, req.NotesRootID, req.ClassName)
	if err != nil {
		return nil, fmt.Errorf("notes: create class folder: %w", err)
	}

	// Resolve or create student subfolder within class.
	studentFolderID, err := findOrCreateDriveFolder(ctx, c.drive, classFolderID, req.StudentName)
	if err != nil {
		return nil, fmt.Errorf("notes: create student folder: %w", err)
	}

	// Create the Google Doc.
	title := fmt.Sprintf("%s — %s", req.StudentName, req.Date)
	doc, err := c.drive.Files.Create(&drive.File{
		Name:     title,
		MimeType: "application/vnd.google-apps.document",
		Parents:  []string{studentFolderID},
	}).Fields("id").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("notes: create doc: %w", err)
	}

	// Populate the document with structured content.
	if err := c.populateDoc(ctx, doc.Id, req); err != nil {
		return nil, fmt.Errorf("notes: populate doc: %w", err)
	}

	// Write to metadata index.
	if c.metaIdx != nil {
		if err := c.metaIdx.AppendEntry(ctx, req.NotesRootID, req.ClassName, req.StudentName, IndexEntry{
			DocID:   doc.Id,
			Date:    req.Date,
			Summary: req.Summary,
		}); err != nil {
			// Log but don't fail note creation for index write errors.
			loggerFromContext(ctx).Warn("notes: failed to write index entry", "student", req.StudentName, "error", err)
		}
	}

	return &CreateNoteResponse{
		DocID:  doc.Id,
		DocURL: fmt.Sprintf("https://docs.google.com/document/d/%s/edit", doc.Id),
	}, nil
}

func (c *driveNoteCreator) populateDoc(ctx context.Context, docID string, req CreateNoteRequest) error {
	// Build insert requests in reverse order (Docs API inserts at index).
	// Final structure: summary, "Transcript" heading, transcript, "Teacher Feedback" heading, empty paragraph.
	requests := []*docs.Request{}

	// Calculate running index.
	idx := int64(1)

	// 1. Summary paragraph
	requests = append(requests,
		&docs.Request{InsertText: &docs.InsertTextRequest{
			Location: &docs.Location{Index: idx},
			Text:     req.Summary + "\n",
		}},
	)
	idx += int64(len(req.Summary) + 1)

	// 2. "Transcript" heading
	transcriptHeading := "Transcript"
	requests = append(requests,
		&docs.Request{InsertText: &docs.InsertTextRequest{
			Location: &docs.Location{Index: idx},
			Text:     transcriptHeading + "\n",
		}},
		&docs.Request{UpdateParagraphStyle: &docs.UpdateParagraphStyleRequest{
			Range:          &docs.Range{StartIndex: idx, EndIndex: idx + int64(len(transcriptHeading)) + 1},
			ParagraphStyle: &docs.ParagraphStyle{NamedStyleType: "HEADING_2"},
			Fields:         "namedStyleType",
		}},
	)
	idx += int64(len(transcriptHeading) + 1)

	// 3. Transcript text
	requests = append(requests,
		&docs.Request{InsertText: &docs.InsertTextRequest{
			Location: &docs.Location{Index: idx},
			Text:     req.Transcript + "\n",
		}},
	)
	idx += int64(len(req.Transcript) + 1)

	// 4. "Teacher Feedback" heading
	feedbackHeading := "Teacher Feedback"
	requests = append(requests,
		&docs.Request{InsertText: &docs.InsertTextRequest{
			Location: &docs.Location{Index: idx},
			Text:     feedbackHeading + "\n",
		}},
		&docs.Request{UpdateParagraphStyle: &docs.UpdateParagraphStyleRequest{
			Range:          &docs.Range{StartIndex: idx, EndIndex: idx + int64(len(feedbackHeading)) + 1},
			ParagraphStyle: &docs.ParagraphStyle{NamedStyleType: "HEADING_2"},
			Fields:         "namedStyleType",
		}},
	)
	idx += int64(len(feedbackHeading) + 1)

	// 5. Empty paragraph for teacher feedback
	requests = append(requests,
		&docs.Request{InsertText: &docs.InsertTextRequest{
			Location: &docs.Location{Index: idx},
			Text:     "\n",
		}},
	)

	_, err := c.docs.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: requests,
	}).Context(ctx).Do()
	return err
}


