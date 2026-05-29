// report_generator.go implements the ReportGenerator interface that creates
// HTML report cards using an LLMProvider and student notes from the database.
package handler

import (
	"context"
	"fmt"
)

// GenerateReportRequest is the input for generating a single student report.
type GenerateReportRequest struct {
	StudentID    int64
	Student      string
	Class        string
	ClassName    string
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
	ClassName    string
	StartDate    string
	EndDate      string
	UserID       string
	Instructions string
}

// llmReportGenerator implements ReportGenerator using an LLMProvider + DB.
type llmReportGenerator struct {
	provider    LLMProvider
	model       string
	noteRepo    *NoteRepo
	reportRepo  *ReportRepo
	exampleRepo *ReportExampleRepo
}

func newDBReportGenerator(provider LLMProvider, nr *NoteRepo, rr *ReportRepo, er *ReportExampleRepo) (*llmReportGenerator, error) {
	return &llmReportGenerator{
		provider:    provider,
		model:       provider.Model(LLMTaskReport),
		noteRepo:    nr,
		reportRepo:  rr,
		exampleRepo: er,
	}, nil
}

func (g *llmReportGenerator) Generate(ctx context.Context, req GenerateReportRequest) (*GenerateReportResponse, error) {
	// 1. Query notes for the student in date range.
	notes, err := g.noteRepo.ListForStudents(ctx, []int64{req.StudentID}, req.StartDate, req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("report: read notes: %w", err)
	}

	// 2. Load examples.
	examples, err := g.loadExamples(ctx, req.UserID, req.ClassName)
	if err != nil {
		return nil, err
	}

	// 3. Build prompt and call LLM.
	prompt := BuildReportPrompt(req.Student, req.Class, notes, examples, req.Instructions, "")
	html, err := g.callLLM(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// 4. Save report to DB.
	modelVersion := g.model
	promptHash := ReportPromptHash
	rpt := &Report{
		StudentID:    req.StudentID,
		StartDate:    req.StartDate,
		EndDate:      req.EndDate,
		HTML:         html,
		ModelVersion: &modelVersion,
		PromptHash:   &promptHash,
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

func (g *llmReportGenerator) Regenerate(ctx context.Context, req RegenerateReportRequest) (*GenerateReportResponse, error) {
	// 1. Query notes.
	notes, err := g.noteRepo.ListForStudents(ctx, []int64{req.StudentID}, req.StartDate, req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("report: read notes: %w", err)
	}

	// 2. Load examples.
	examples, err := g.loadExamples(ctx, req.UserID, req.ClassName)
	if err != nil {
		return nil, err
	}

	// 3. Build prompt with feedback and call LLM.
	prompt := BuildReportPrompt(req.Student, req.Class, notes, examples, req.Instructions, req.Feedback)
	html, err := g.callLLM(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// 4. Save as a new report (new row, preserves history).
	modelVersion := g.model
	promptHash := ReportPromptHash
	rpt := &Report{
		StudentID:    req.StudentID,
		StartDate:    req.StartDate,
		EndDate:      req.EndDate,
		HTML:         html,
		ModelVersion: &modelVersion,
		PromptHash:   &promptHash,
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

func (g *llmReportGenerator) loadExamples(ctx context.Context, userID, className string) ([]ReportExample, error) {
	if userID == "" {
		return nil, nil
	}
	dbExamples, err := g.exampleRepo.ListReadyByClassName(ctx, userID, className)
	if err != nil {
		return nil, fmt.Errorf("report: list examples: %w", err)
	}
	examples := make([]ReportExample, len(dbExamples))
	for i, e := range dbExamples {
		examples[i] = ReportExample{ID: e.ID, Name: e.Name, Content: e.Content, Status: e.Status}
	}
	return examples, nil
}

func (g *llmReportGenerator) callLLM(ctx context.Context, prompt string) (string, error) {
	text, err := g.provider.ChatText(ctx, ChatTextRequest{UserPrompt: prompt})
	if err != nil {
		return "", fmt.Errorf("report: LLM call failed: %w", err)
	}
	return text, nil
}
