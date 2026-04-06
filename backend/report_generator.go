// report_generator.go implements the ReportGenerator interface that creates
// HTML report cards using GPT and student notes from the database.
package handler

import (
	"context"
	"fmt"
	"os"

	openai "github.com/sashabaranov/go-openai"
)

// GenerateReportRequest is the input for generating a single student report.
type GenerateReportRequest struct {
	StudentID    int64
	Student      string
	Class        string
	StartDate    string // YYYY-MM-DD
	EndDate      string // YYYY-MM-DD
	UserID       string
	Instructions string
}

// GenerateReportResponse contains the created report info.
type GenerateReportResponse struct {
	ReportID  int64  `json:"reportId"`
	HTML      string `json:"html"`
	CreatedAt string `json:"createdAt"`
}

// ReportGenerator creates report card documents.
type ReportGenerator interface {
	Generate(ctx context.Context, req GenerateReportRequest) (*GenerateReportResponse, error)
	Regenerate(ctx context.Context, req RegenerateReportRequest) (*GenerateReportResponse, error)
}

// RegenerateReportRequest is the input for regenerating an existing report.
type RegenerateReportRequest struct {
	ReportID     int64
	Feedback     string
	StudentID    int64
	Student      string
	Class        string
	StartDate    string
	EndDate      string
	UserID       string
	Instructions string
}

// gptReportGenerator implements ReportGenerator using GPT + DB.
type gptReportGenerator struct {
	client      *openai.Client
	noteRepo    *NoteRepo
	reportRepo  *ReportRepo
	exampleRepo *ReportExampleRepo
}

func newDBReportGenerator(nr *NoteRepo, rr *ReportRepo, er *ReportExampleRepo) (*gptReportGenerator, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	return &gptReportGenerator{
		client:      openai.NewClient(key),
		noteRepo:    nr,
		reportRepo:  rr,
		exampleRepo: er,
	}, nil
}

func (g *gptReportGenerator) Generate(ctx context.Context, req GenerateReportRequest) (*GenerateReportResponse, error) {
	// 1. Query notes for the student in date range.
	notes, err := g.noteRepo.ListForStudents(ctx, []int64{req.StudentID}, req.StartDate, req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("report: read notes: %w", err)
	}

	// 2. Load examples.
	examples, err := g.loadExamples(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 3. Build prompt and call GPT.
	prompt := buildReportPrompt(req.Student, req.Class, req.StartDate, req.EndDate, notes, examples, req.Instructions, "")
	html, err := g.callGPT(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// 4. Save report to DB.
	rpt := &Report{
		StudentID: req.StudentID,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		HTML:      html,
	}
	if req.Instructions != "" {
		rpt.Instructions = &req.Instructions
	}
	if err := g.reportRepo.Create(ctx, rpt); err != nil {
		return nil, fmt.Errorf("report: save: %w", err)
	}

	return &GenerateReportResponse{
		ReportID:  rpt.ID,
		HTML:      html,
		CreatedAt: rpt.CreatedAt,
	}, nil
}

func (g *gptReportGenerator) Regenerate(ctx context.Context, req RegenerateReportRequest) (*GenerateReportResponse, error) {
	// 1. Query notes.
	notes, err := g.noteRepo.ListForStudents(ctx, []int64{req.StudentID}, req.StartDate, req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("report: read notes: %w", err)
	}

	// 2. Load examples.
	examples, err := g.loadExamples(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	// 3. Build prompt with feedback and call GPT.
	prompt := buildReportPrompt(req.Student, req.Class, req.StartDate, req.EndDate, notes, examples, req.Instructions, req.Feedback)
	html, err := g.callGPT(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// 4. Save as a new report (new row, preserves history).
	rpt := &Report{
		StudentID: req.StudentID,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		HTML:      html,
	}
	if req.Instructions != "" {
		rpt.Instructions = &req.Instructions
	}
	if err := g.reportRepo.Create(ctx, rpt); err != nil {
		return nil, fmt.Errorf("report: save: %w", err)
	}

	return &GenerateReportResponse{
		ReportID:  rpt.ID,
		HTML:      html,
		CreatedAt: rpt.CreatedAt,
	}, nil
}

func (g *gptReportGenerator) loadExamples(ctx context.Context, userID string) ([]ReportExample, error) {
	if userID == "" {
		return nil, nil
	}
	dbExamples, err := g.exampleRepo.ListReady(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("report: list examples: %w", err)
	}
	examples := make([]ReportExample, len(dbExamples))
	for i, e := range dbExamples {
		examples[i] = ReportExample{ID: e.ID, Name: e.Name, Content: e.Content, Status: e.Status}
	}
	return examples, nil
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
