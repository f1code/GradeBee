// eval_endpoint.go exposes lightweight HTTP endpoints that the promptfoo eval
// harness can call to exercise the real Go prompt-building paths.
//
// # Security
//
// The endpoints are registered in cmd/server/main.go ONLY when the EVAL_TOKEN
// environment variable is non-empty. Every request must include a matching
// X-Eval-Token header; the middleware refuses with 404 (not 401) to avoid
// advertising the endpoint's existence to an attacker.
//
// EVAL_TOKEN must NEVER be set in production. The startup code in
// cmd/server/main.go logs a warning when the var is set so operators notice if
// it leaks. A panic is raised when APP_ENV=prod and EVAL_TOKEN is non-empty.
//
// The handlers do NOT write to the database — evaluation is read-only.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	openai "github.com/sashabaranov/go-openai"
)

// RequireEvalToken returns middleware that checks the X-Eval-Token header.
// If the token does not match, the handler returns 404 (not 401).
func RequireEvalToken(token string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Eval-Token") != token {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	})
}

// evalExtractRequest is the JSON body for POST /eval/extract.
type evalExtractRequest struct {
	Transcript string       `json:"transcript"`
	Classes    []ClassGroup `json:"classes"`
}

// evalGenerateReportRequest is the JSON body for POST /eval/generate-report.
type evalGenerateReportRequest struct {
	StudentName  string        `json:"student_name"`
	Class        string        `json:"class"`
	StartDate    string        `json:"start_date"`
	EndDate      string        `json:"end_date"`
	Notes        []evalNote    `json:"notes"`
	Examples     []evalExample `json:"examples"`
	Instructions string        `json:"instructions"`
}

type evalNote struct {
	Date    string `json:"date"`
	Summary string `json:"summary"`
}

type evalExample struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// evalGenerateReportResponse is the JSON response from POST /eval/generate-report.
type evalGenerateReportResponse struct {
	HTML         string `json:"html"`
	ModelVersion string `json:"model_version"`
	PromptHash   string `json:"prompt_hash"`
}

// HandleEvalExtract calls the real extraction logic and returns the result
// without persisting anything to the database.
// Route: POST /eval/extract (registered only when EVAL_TOKEN is set).
func HandleEvalExtract(w http.ResponseWriter, r *http.Request) {
	var req evalExtractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEvalError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Transcript == "" {
		writeEvalError(w, http.StatusBadRequest, "transcript is required")
		return
	}

	extractor, err := newGPTExtractor()
	if err != nil {
		writeEvalError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result, err := extractor.Extract(r.Context(), ExtractRequest(req))
	if err != nil {
		writeEvalError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		getLogger().Error("eval extract encode error", "error", err)
	}
}

// HandleEvalGenerateReport calls the real report-prompt builder + GPT and
// returns the generated HTML without persisting to the database.
// Route: POST /eval/generate-report (registered only when EVAL_TOKEN is set).
func HandleEvalGenerateReport(w http.ResponseWriter, r *http.Request) {
	var req evalGenerateReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEvalError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.StudentName == "" {
		writeEvalError(w, http.StatusBadRequest, "student_name is required")
		return
	}

	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		writeEvalError(w, http.StatusInternalServerError, "OPENAI_API_KEY not set")
		return
	}

	// Convert eval notes to Note slice (no DB IDs needed — builder only uses Date + Summary).
	notes := make([]Note, len(req.Notes))
	for i, n := range req.Notes {
		notes[i] = Note{Date: n.Date, Summary: n.Summary}
	}

	// Convert eval examples.
	examples := make([]ReportExample, len(req.Examples))
	for i, e := range req.Examples {
		examples[i] = ReportExample{Name: e.Name, Content: e.Content}
	}

	startDate := req.StartDate
	if startDate == "" {
		startDate = "2000-01-01"
	}
	endDate := req.EndDate
	if endDate == "" {
		endDate = "2099-12-31"
	}

	prompt := buildReportPrompt(req.StudentName, req.Class, startDate, endDate, notes, examples, req.Instructions, "")

	client := openai.NewClient(key)
	html, err := callGPTWithClient(r.Context(), client, prompt)
	if err != nil {
		writeEvalError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp := evalGenerateReportResponse{
		HTML:         html,
		ModelVersion: ProductionModelName,
		PromptHash:   ReportPromptHash,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		getLogger().Error("eval generate-report encode error", "error", err)
	}
}

// writeEvalError writes a JSON error response for eval endpoints.
func writeEvalError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	if err := enc.Encode(map[string]string{"error": msg}); err != nil {
		getLogger().Error("eval error encode failed", "error", err)
	}
}

// callGPTWithClient calls the OpenAI chat completion API with the given prompt
// and returns the generated text. Extracted from gptReportGenerator.callGPT
// so eval endpoints can use it without the full generator struct.
func callGPTWithClient(ctx context.Context, client *openai.Client, prompt string) (string, error) {
	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: ProductionModelName,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: prompt},
			{Role: openai.ChatMessageRoleUser, Content: "Generate the report card now."},
		},
	})
	if err != nil {
		return "", fmt.Errorf("eval: GPT call failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("eval: GPT returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}
