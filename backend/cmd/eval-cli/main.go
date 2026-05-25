// eval-cli is a command-line tool for the GradeBee LLM evaluation harness.
//
// # Usage (exec-prompt mode — invoked by promptfoo as a prompt function)
//
//	eval-cli '{"vars":{...},"config":{"task":"build-extract-prompt"}}'
//	eval-cli '{"vars":{...},"config":{"task":"build-report-prompt"}}'
//
// Output is a JSON messages array: [{"role":"system","content":"..."},...]
//
// # Usage (legacy exec-provider mode — manual debugging)
//
//	eval-cli extract         <prompt> <options_json> <context_json>
//	eval-cli generate-report <prompt> <options_json> <context_json>
//
// # Environment
//
//	OPENAI_API_KEY  required (legacy exec-provider mode only)
//	EVAL_MODEL      optional; defaults to handler.ProductionModelName (legacy only)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	handler "github.com/nicogaller/gradebee/backend"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "eval-cli: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: eval-cli <subcommand|json>")
	}

	// Exec-prompt mode: promptfoo passes a single JSON argument.
	if strings.HasPrefix(args[1], "{") {
		return runPromptMode(args[1])
	}

	// Legacy exec-provider / manual debug mode.
	if len(args) < 5 {
		return fmt.Errorf("usage: eval-cli <subcommand> <prompt> <options> <context>")
	}

	subcommand := args[1]
	// args[2] = rendered prompt (ignored — we call BuildReportPrompt ourselves)
	// args[3] = provider options JSON (unused)
	contextJSON := args[4]

	var ctx evalContext
	if err := json.Unmarshal([]byte(contextJSON), &ctx); err != nil {
		return fmt.Errorf("parse context: %w", err)
	}

	model := os.Getenv("EVAL_MODEL")
	if model == "" {
		model = handler.ProductionModelName
	}

	var output string
	var runErr error

	switch subcommand {
	case "extract":
		output, runErr = runExtract(context.Background(), model, ctx)
	case "generate-report":
		output, runErr = runGenerateReport(context.Background(), model, ctx)
	default:
		return fmt.Errorf("unknown subcommand %q; expected extract or generate-report", subcommand)
	}

	if runErr != nil {
		// Write error JSON to stdout (promptfoo reads stdout) and also stderr.
		fmt.Fprintf(os.Stderr, "eval-cli %s: %v\n", subcommand, runErr)
		return writeJSON(map[string]string{"error": runErr.Error()})
	}

	return writeJSON(map[string]string{"output": output})
}

// promptRequest is the shape promptfoo passes to exec-prompt functions.
type promptRequest struct {
	Vars   map[string]json.RawMessage `json:"vars"`
	Config struct {
		Task string `json:"task"`
	} `json:"config"`
}

func runPromptMode(jsonArg string) error {
	var req promptRequest
	if err := json.Unmarshal([]byte(jsonArg), &req); err != nil {
		return fmt.Errorf("parse prompt request: %w", err)
	}
	ec := evalContext{Vars: req.Vars}
	switch req.Config.Task {
	case "build-extract-prompt":
		return runBuildExtractPrompt(ec)
	case "build-report-prompt":
		return runBuildReportPrompt(ec)
	default:
		return fmt.Errorf("unknown config.task %q", req.Config.Task)
	}
}

// runBuildExtractPrompt outputs a promptfoo messages array for extraction.
func runBuildExtractPrompt(ec evalContext) error {
	var classes []handler.ClassGroup
	if err := ec.unmarshalVar("classes", &classes); err != nil {
		return err
	}
	var transcript string
	if err := ec.unmarshalVar("transcript", &transcript); err != nil {
		return err
	}
	if transcript == "" {
		return fmt.Errorf("vars.transcript is required")
	}
	systemPrompt := handler.BuildExtractionPrompt(classes)
	return writeJSON([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": transcript},
	})
}

// runBuildReportPrompt outputs a promptfoo messages array for report generation.
func runBuildReportPrompt(ec evalContext) error {
	var studentName, class, instructions string
	if err := ec.unmarshalVar("student_name", &studentName); err != nil {
		return err
	}
	if err := ec.unmarshalVar("class", &class); err != nil {
		return err
	}
	if err := ec.unmarshalVar("instructions", &instructions); err != nil {
		return err
	}
	var notes []handler.Note
	if err := ec.unmarshalVar("notes", &notes); err != nil {
		return err
	}
	var examples []handler.ReportExample
	if err := ec.unmarshalVar("examples", &examples); err != nil {
		return err
	}
	// Production sends the built prompt as a single user message (no system role).
	prompt := handler.BuildReportPrompt(studentName, class, notes, examples, instructions, "")
	return writeJSON([]map[string]string{
		{"role": "user", "content": prompt},
	})
}

// runExtract runs the extraction pipeline and returns JSON-encoded output.
func runExtract(ctx context.Context, model string, ec evalContext) (string, error) {
	var transcript string
	if err := ec.unmarshalVar("transcript", &transcript); err != nil {
		return "", err
	}
	if transcript == "" {
		return "", fmt.Errorf("vars.transcript is required")
	}

	var classes []handler.ClassGroup
	if err := ec.unmarshalVar("classes", &classes); err != nil {
		return "", err
	}

	ext, err := handler.NewGPTExtractorWithModel(model)
	if err != nil {
		return "", err
	}

	result, err := ext.Extract(ctx, handler.ExtractRequest{
		Transcript: transcript,
		Classes:    classes,
	})
	if err != nil {
		return "", err
	}

	out, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal extract result: %w", err)
	}
	return string(out), nil
}

// runGenerateReport runs the report-generation pipeline and returns the HTML.
func runGenerateReport(ctx context.Context, model string, ec evalContext) (string, error) {
	var studentName string
	if err := ec.unmarshalVar("student_name", &studentName); err != nil {
		return "", err
	}
	if studentName == "" {
		return "", fmt.Errorf("vars.student_name is required")
	}

	var class, instructions string
	if err := ec.unmarshalVar("class", &class); err != nil {
		return "", err
	}
	if err := ec.unmarshalVar("instructions", &instructions); err != nil {
		return "", err
	}

	var notes []handler.Note
	if err := ec.unmarshalVar("notes", &notes); err != nil {
		return "", err
	}

	var examples []handler.ReportExample
	if err := ec.unmarshalVar("examples", &examples); err != nil {
		return "", err
	}

	prompt := handler.BuildReportPrompt(studentName, class, notes, examples, instructions, "")

	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set")
	}
	client := openai.NewClient(key)

	return handler.GenerateReportHTML(ctx, client, model, prompt)
}

// evalContext mirrors the promptfoo exec context shape.
type evalContext struct {
	Vars map[string]json.RawMessage `json:"vars"`
}

// unmarshalVar decodes a named var into v. Missing vars are silently ignored
// (zero value remains), since optional vars like instructions may be absent.
func (ec *evalContext) unmarshalVar(name string, v interface{}) error {
	raw, ok := ec.Vars[name]
	if !ok {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("parse vars.%s: %w", name, err)
	}
	return nil
}

// writeJSON encodes v as JSON to stdout.
func writeJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
