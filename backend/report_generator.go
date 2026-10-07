// report_generator.go implements the ReportGenerator interface that creates
// HTML report cards using an LLMProvider. Callers resolve the notes.
package handler

import (
	"context"
	"database/sql"
	"fmt"
)

// GenerateReportRequest is the input for generating a single student report.
type GenerateReportRequest struct {
	ReportInputs
	UserID string
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
	ReportInputs
	ReportID int64
	Feedback string
	UserID   string
}

// llmReportGenerator implements ReportGenerator using an LLMProvider + ReportRepo.
type llmReportGenerator struct {
	provider   LLMProvider
	model      string
	reportRepo *ReportRepo
}

// NewReportGenerator builds the production generator outside the server, for
// cmd/eval-cli. It reads LLM_PROVIDER and the API key like the server.
func NewReportGenerator(db *sql.DB) (ReportGenerator, error) {
	provider, err := LoadProvider(db)
	if err != nil {
		return nil, err
	}
	return newDBReportGenerator(provider, &ReportRepo{db: db})
}

func newDBReportGenerator(provider LLMProvider, rr *ReportRepo) (*llmReportGenerator, error) {
	return &llmReportGenerator{
		provider:   provider,
		model:      provider.Model(LLMTaskReport),
		reportRepo: rr,
	}, nil
}

func (g *llmReportGenerator) Generate(ctx context.Context, req GenerateReportRequest) (*GenerateReportResponse, error) {
	// 1. Build prompt and call LLM.
	prompt := BuildReportPrompt(req.StudentName, req.ClassName, req.Notes, req.ReportInstructions, req.Instructions, "")
	html, err := g.callLLM(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// 2. Save report to DB.
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
	// 1. Build prompt with feedback and call LLM.
	prompt := BuildReportPrompt(req.StudentName, req.ClassName, req.Notes, req.ReportInstructions, req.Instructions, req.Feedback)
	html, err := g.callLLM(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// 2. Save as a new report (new row, preserves history).
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

func (g *llmReportGenerator) callLLM(ctx context.Context, prompt string) (string, error) {
	resp, err := g.provider.ChatText(ctx, ChatTextRequest{UserPrompt: prompt})
	if err != nil {
		return "", fmt.Errorf("report: LLM call failed: %w", err)
	}
	return resp.Text, nil
}
