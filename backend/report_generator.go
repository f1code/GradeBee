// report_generator.go implements the ReportGenerator interface that creates
// HTML report cards using an LLMProvider. Callers resolve the notes.
package handler

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
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
	ReportRevision
	ReportID int64
	UserID   string
}

// llmReportGenerator implements ReportGenerator using an LLMProvider + ReportRepo.
type llmReportGenerator struct {
	provider   LLMProvider
	model      string
	reportRepo *ReportRepo
}

// ReportDrafter writes a first-generation report through the prompt Generate
// uses, without saving it: cmd/eval-cli keeps eval references in files, out of
// the DB a prod copy replaces.
type ReportDrafter interface {
	Draft(ctx context.Context, in ReportInputs) (string, error)
	Model() string
}

// NewReportDrafter builds the production drafter outside the server, for
// cmd/eval-cli. It reads LLM_PROVIDER and the API key like the server.
func NewReportDrafter(db *sql.DB) (ReportDrafter, error) {
	provider, err := LoadProvider(db)
	if err != nil {
		return nil, err
	}
	return newDBReportGenerator(provider, nil)
}

func newDBReportGenerator(provider LLMProvider, rr *ReportRepo) (*llmReportGenerator, error) {
	return &llmReportGenerator{
		provider:   provider,
		model:      provider.Model(LLMTaskReport),
		reportRepo: rr,
	}, nil
}

func (g *llmReportGenerator) Draft(ctx context.Context, in ReportInputs) (string, error) {
	return g.callLLM(ctx, BuildReportPrompt(in.StudentName, in.ClassName, in.Notes, in.ReportInstructions, in.Instructions, nil))
}

func (g *llmReportGenerator) Model() string { return g.model }

func (g *llmReportGenerator) Generate(ctx context.Context, req GenerateReportRequest) (*GenerateReportResponse, error) {
	// 1. Build prompt and call LLM.
	html, err := g.Draft(ctx, req.ReportInputs)
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
	// 1. Build prompt with previous draft and feedback, call LLM.
	prompt := BuildReportPrompt(req.StudentName, req.ClassName, req.Notes, req.ReportInstructions, req.Instructions, &req.ReportRevision)
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
	// Implicit signal: regenerating records a thumbs-down on the source report.
	// Saved with the report: the chain of later rounds reads this row.
	fb := ArtifactFeedback{
		ArtifactType: "report",
		ArtifactID:   req.ReportID,
		Rating:       "down",
		Signal:       "regenerated",
		Comment:      &req.Feedback,
		UserID:       req.UserID,
	}
	if err := g.reportRepo.CreateRegenerated(ctx, rpt, fb); err != nil {
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
	return stripCodeFence(resp.Text), nil
}

// Some models wrap the HTML in a ```html fence; the prompt does not forbid it.
var codeFence = regexp.MustCompile("(?s)^(```|~~~)[a-zA-Z]*\\s*(.*?)\\s*(```|~~~)?$")

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if m := codeFence.FindStringSubmatch(s); m != nil {
		return m[2]
	}
	return s
}
