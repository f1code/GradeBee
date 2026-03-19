// report_generator.go implements the ReportGenerator interface that creates
// report card Google Docs using GPT and student note summaries.
package handler

import (
	"context"
	"fmt"
	"os"
	"strings"

	openai "github.com/sashabaranov/go-openai"
	"google.golang.org/api/docs/v1"
	"google.golang.org/api/drive/v3"
)

// GenerateReportRequest is the input for generating a single student report.
type GenerateReportRequest struct {
	Student          string
	Class            string
	StartDate        string // YYYY-MM-DD
	EndDate          string // YYYY-MM-DD
	NotesRootID      string
	ReportsID        string
	ExamplesFolderID string
	Instructions     string
}

// GenerateReportResponse contains the created report info.
type GenerateReportResponse struct {
	DocID   string `json:"docId"`
	DocURL  string `json:"docUrl"`
	Skipped bool   `json:"skipped"`
}

// ReportGenerator creates report card documents.
type ReportGenerator interface {
	Generate(ctx context.Context, req GenerateReportRequest) (*GenerateReportResponse, error)
	Regenerate(ctx context.Context, req RegenerateReportRequest) (*GenerateReportResponse, error)
}

// RegenerateReportRequest is the input for regenerating an existing report.
type RegenerateReportRequest struct {
	DocID            string
	Student          string
	Class            string
	StartDate        string
	EndDate          string
	NotesRootID      string
	ExamplesFolderID string
	Instructions     string
}

// gptReportGenerator implements ReportGenerator using GPT + Drive/Docs.
type gptReportGenerator struct {
	client   *openai.Client
	metaIdx  MetadataIndex
	examples ExampleStore
	drive    *drive.Service
	docs     *docs.Service
}

func newGPTReportGenerator(driveSvc *drive.Service, docsSvc *docs.Service) (*gptReportGenerator, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	return &gptReportGenerator{
		client:   openai.NewClient(key),
		metaIdx:  newDriveMetadataIndex(driveSvc),
		examples: newDriveExampleStore(driveSvc),
		drive:    driveSvc,
		docs:     docsSvc,
	}, nil
}

func (g *gptReportGenerator) Generate(ctx context.Context, req GenerateReportRequest) (*GenerateReportResponse, error) {
	// 1. Resolve or create YYYY-MM subfolder from endDate.
	monthFolder := req.EndDate[:7] // YYYY-MM
	monthFolderID, err := findOrCreateDriveFolder(ctx, g.drive, req.ReportsID, monthFolder)
	if err != nil {
		return nil, fmt.Errorf("report: create month folder: %w", err)
	}

	// 2. Check for existing report (duplicate detection).
	docName := fmt.Sprintf("%s — %s", req.Student, req.Class)
	existingID, err := g.findExistingReport(ctx, monthFolderID, docName)
	if err != nil {
		return nil, err
	}
	if existingID != "" {
		return &GenerateReportResponse{
			DocID:   existingID,
			DocURL:  fmt.Sprintf("https://docs.google.com/document/d/%s/edit", existingID),
			Skipped: true,
		}, nil
	}

	// 3. Read notes index and filter by date range.
	notes, err := g.getFilteredNotes(ctx, req.NotesRootID, req.Class, req.Student, req.StartDate, req.EndDate)
	if err != nil {
		return nil, err
	}

	// 4. Load example report cards.
	examples, err := g.loadExamples(ctx, req.ExamplesFolderID)
	if err != nil {
		return nil, err
	}

	// 5. Build prompt and call GPT.
	prompt := buildReportPrompt(req.Student, req.Class, req.StartDate, req.EndDate, notes, examples, req.Instructions, "")
	narrative, err := g.callGPT(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// 6. Create Google Doc.
	doc, err := g.drive.Files.Create(&drive.File{
		Name:     docName,
		MimeType: "application/vnd.google-apps.document",
		Parents:  []string{monthFolderID},
	}).Fields("id").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("report: create doc: %w", err)
	}

	// 7. Populate doc.
	if err := g.populateReportDoc(ctx, doc.Id, narrative); err != nil {
		return nil, fmt.Errorf("report: populate doc: %w", err)
	}

	return &GenerateReportResponse{
		DocID:  doc.Id,
		DocURL: fmt.Sprintf("https://docs.google.com/document/d/%s/edit", doc.Id),
	}, nil
}

func (g *gptReportGenerator) Regenerate(ctx context.Context, req RegenerateReportRequest) (*GenerateReportResponse, error) {
	// 1. Read feedback from existing doc.
	feedback, err := g.readFeedback(ctx, req.DocID)
	if err != nil {
		return nil, fmt.Errorf("report: read feedback: %w", err)
	}

	// 2. Read notes.
	notes, err := g.getFilteredNotes(ctx, req.NotesRootID, req.Class, req.Student, req.StartDate, req.EndDate)
	if err != nil {
		return nil, err
	}

	// 3. Load examples.
	examples, err := g.loadExamples(ctx, req.ExamplesFolderID)
	if err != nil {
		return nil, err
	}

	// 4. Build prompt with feedback and call GPT.
	prompt := buildReportPrompt(req.Student, req.Class, req.StartDate, req.EndDate, notes, examples, req.Instructions, feedback)
	narrative, err := g.callGPT(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// 5. Replace doc content.
	if err := g.replaceReportDoc(ctx, req.DocID, narrative); err != nil {
		return nil, fmt.Errorf("report: replace doc: %w", err)
	}

	return &GenerateReportResponse{
		DocID:  req.DocID,
		DocURL: fmt.Sprintf("https://docs.google.com/document/d/%s/edit", req.DocID),
	}, nil
}

func (g *gptReportGenerator) findExistingReport(ctx context.Context, folderID, docName string) (string, error) {
	q := fmt.Sprintf("'%s' in parents and name = '%s' and mimeType = 'application/vnd.google-apps.document' and trashed = false", folderID, docName)
	list, err := g.drive.Files.List().Q(q).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("report: check existing: %w", err)
	}
	if len(list.Files) > 0 {
		return list.Files[0].Id, nil
	}
	return "", nil
}

func (g *gptReportGenerator) getFilteredNotes(ctx context.Context, notesRootID, class, student, startDate, endDate string) ([]IndexEntry, error) {
	idx, err := g.metaIdx.ReadIndex(ctx, notesRootID, class, student)
	if err != nil {
		return nil, fmt.Errorf("report: read notes index: %w", err)
	}
	var filtered []IndexEntry
	for _, e := range idx.Entries {
		if e.Date >= startDate && e.Date <= endDate {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

func (g *gptReportGenerator) loadExamples(ctx context.Context, examplesFolderID string) ([]ReportExample, error) {
	if examplesFolderID == "" {
		return nil, nil
	}
	list, err := g.examples.ListExamples(ctx, examplesFolderID)
	if err != nil {
		return nil, fmt.Errorf("report: list examples: %w", err)
	}
	var full []ReportExample
	for _, ex := range list {
		detail, err := g.examples.ReadExample(ctx, ex.ID)
		if err != nil {
			return nil, fmt.Errorf("report: read example %s: %w", ex.Name, err)
		}
		full = append(full, *detail)
	}
	return full, nil
}

func (g *gptReportGenerator) callGPT(ctx context.Context, prompt string) (string, error) {
	resp, err := g.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: "gpt-5.4-mini",
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: prompt},
			{Role: openai.ChatMessageRoleUser, Content: "Generate the report card now."},
		},
	})
	if err != nil {
		return "", fmt.Errorf("report: GPT call failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("report: GPT returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

func (g *gptReportGenerator) populateReportDoc(ctx context.Context, docID, narrative string) error {
	idx := int64(1)
	requests := []*docs.Request{}

	// 1. Report narrative
	requests = append(requests, &docs.Request{InsertText: &docs.InsertTextRequest{
		Location: &docs.Location{Index: idx},
		Text:     narrative + "\n",
	}})
	idx += int64(len(narrative) + 1)

	// 2. "Teacher Feedback" heading
	heading := "Teacher Feedback"
	requests = append(requests,
		&docs.Request{InsertText: &docs.InsertTextRequest{
			Location: &docs.Location{Index: idx},
			Text:     heading + "\n",
		}},
		&docs.Request{UpdateParagraphStyle: &docs.UpdateParagraphStyleRequest{
			Range:          &docs.Range{StartIndex: idx, EndIndex: idx + int64(len(heading)) + 1},
			ParagraphStyle: &docs.ParagraphStyle{NamedStyleType: "HEADING_2"},
			Fields:         "namedStyleType",
		}},
	)
	idx += int64(len(heading) + 1)

	// 3. Empty paragraph for feedback
	requests = append(requests, &docs.Request{InsertText: &docs.InsertTextRequest{
		Location: &docs.Location{Index: idx},
		Text:     "\n",
	}})

	_, err := g.docs.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: requests,
	}).Context(ctx).Do()
	return err
}

func (g *gptReportGenerator) replaceReportDoc(ctx context.Context, docID, narrative string) error {
	// Get current doc to find content length.
	doc, err := g.docs.Documents.Get(docID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("report: get doc for replace: %w", err)
	}

	// Delete all content (index 1 to end).
	endIdx := doc.Body.Content[len(doc.Body.Content)-1].EndIndex - 1
	requests := []*docs.Request{}
	if endIdx > 1 {
		requests = append(requests, &docs.Request{DeleteContentRange: &docs.DeleteContentRangeRequest{
			Range: &docs.Range{StartIndex: 1, EndIndex: endIdx},
		}})
	}

	_, err = g.docs.Documents.BatchUpdate(docID, &docs.BatchUpdateDocumentRequest{
		Requests: requests,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("report: clear doc: %w", err)
	}

	// Repopulate.
	return g.populateReportDoc(ctx, docID, narrative)
}

func (g *gptReportGenerator) readFeedback(ctx context.Context, docID string) (string, error) {
	doc, err := g.docs.Documents.Get(docID).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("report: get doc for feedback: %w", err)
	}

	// Find "Teacher Feedback" heading and extract text after it.
	foundHeading := false
	var feedback strings.Builder
	for _, elem := range doc.Body.Content {
		if elem.Paragraph == nil {
			continue
		}
		// Check if this paragraph is the "Teacher Feedback" heading.
		if !foundHeading {
			for _, el := range elem.Paragraph.Elements {
				if el.TextRun != nil && strings.TrimSpace(el.TextRun.Content) == "Teacher Feedback" {
					foundHeading = true
					break
				}
			}
			continue
		}
		// Collect text after the heading.
		for _, el := range elem.Paragraph.Elements {
			if el.TextRun != nil {
				feedback.WriteString(el.TextRun.Content)
			}
		}
	}
	return strings.TrimSpace(feedback.String()), nil
}
