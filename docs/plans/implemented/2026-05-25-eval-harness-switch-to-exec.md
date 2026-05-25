# Eval Harness — Switch from HTTP Provider to Exec Provider

**Goal:** Replace the HTTP-based promptfoo provider (server + `EVAL_TOKEN` + middleware) with an exec-based provider (CLI binary). Eliminate the prompt/GPT-call duplication between eval and production with one shared helper. Make the eval model configurable.

**Non-goals:** changing fixtures, scoring JS, baseline format, judge model, or any frontend/feedback code.

---

## Why

Re-reading `2026-05-20-llm-evaluation-harness.md` Decision #9: we picked promptfoo's HTTP provider without considering exec. The eval handler bypasses Clerk auth, doesn't write to the DB, and just calls the prompt-build + GPT-call path directly. That's a CLI's job.

What HTTP cost us:
- `backend/eval_endpoint.go` (195 lines) + `eval_endpoint_test.go` (76 lines)
- Conditional route registration + prod-env panic in `cmd/server/main.go`
- `EVAL_TOKEN` env var, `.env.example` warning, `ARCHITECTURE.md` section, prod startup assertion (a DoD bullet)
- Makefile lifecycle: background `go run`, `sleep 2` race, PID tracking, `kill`/`wait`

What exec gives:
- One CLI, one `go build`, one promptfoo invocation. No port, no token, no race, no prod-leak surface.
- Single-case repro: `echo '{...}' | ./bin/eval-cli generate-report`.

---

## Code-sharing approach

Reviewed three options; minimal helper extraction wins on ROI.

**Considered and rejected:**
- *In-memory SQLite seeded from fixtures.* Couples eval to schema/migrations for no prompt-fidelity gain.
- *Consumer-side interfaces (`noteReader` / `reportWriter` / `exampleReader`) + stub repos.* Three interfaces × one production impl × one trivial stub each = "interface for one impl" smell. Routes eval through `Generate()` to share ~10 LOC. Drift footgun (`discardReports{}.Create → nil` is a runtime-only guarantee). Verbatim-`Generate` framing was rationalization — `notes`/`examples` aren't part of prompt mechanics, and stubs return them verbatim, so going through `Generate` adds zero fidelity vs. calling `buildReportPrompt` directly.

**Chosen — minimal helper extraction:**

The only thing actually duplicated today is a ~10-line OpenAI call. `buildReportPrompt` is already a free function. So:

1. Extract a single helper `generateReportHTML(ctx, client, model, prompt) (string, error)` from `gptReportGenerator.callGPT`. Production `callGPT` becomes a one-liner that calls it.
2. Add a `model string` field to both `gptReportGenerator` and `gptExtractor`. Default to `ProductionModelName` via existing constructors. New `*WithModel` constructor variants for eval. Replace inline `ProductionModelName` references in OpenAI calls with `g.model`.
3. eval-cli for reports: read fixture JSON from stdin → `handler.BuildReportPrompt(...)` → `handler.GenerateReportHTML(ctx, client, model, prompt)` → emit JSON. Zero interfaces, zero stubs.
4. eval-cli for extraction: construct `gptExtractor` via `NewGPTExtractorWithModel(evalModel)` and call `Extract` directly — already pure, no repos involved.

Drift surface goes from "10 lines of duplicated `callGPTWithClient` + hardcoded model name" to **zero**. Production change: ~20 LOC.

---

## Proposed changes

### 1. New: shared GPT helper

**File:** `backend/report_generator.go` (or new `gpt_call.go` if we want it separate — recommend keeping it in `report_generator.go` next to its main caller).

```go
// generateReportHTML calls the OpenAI chat completion API with the given
// system prompt and returns the generated text. Shared by gptReportGenerator
// (production) and cmd/eval-cli (evaluation harness).
func generateReportHTML(ctx context.Context, client *openai.Client, model, prompt string) (string, error) {
    resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
        Model: model,
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
```

Existing `(*gptReportGenerator).callGPT` becomes:

```go
func (g *gptReportGenerator) callGPT(ctx context.Context, prompt string) (string, error) {
    return generateReportHTML(ctx, g.client, g.model, prompt)
}
```

**Exporting for eval-cli:** since `cmd/eval-cli` lives outside `package handler`, both `GenerateReportHTML` and `BuildReportPrompt` need to be exported. `BuildReportPrompt` is the canonical prompt — it must live in Go, not in a promptfoo YAML template. eval-cli reads raw fixture vars from `context.vars` and calls `BuildReportPrompt` itself. Same for `Note`, `ReportExample` types it consumes — already exported.

### 2. Add `model` field to generators

**Files:** `backend/report_generator.go`, `backend/extract.go`

```go
type gptReportGenerator struct {
    client      *openai.Client
    model       string         // NEW; defaults to ProductionModelName
    noteRepo    *NoteRepo
    reportRepo  *ReportRepo
    exampleRepo *ReportExampleRepo
}

func newDBReportGenerator(nr *NoteRepo, rr *ReportRepo, er *ReportExampleRepo) (*gptReportGenerator, error) {
    return newDBReportGeneratorWithModel(ProductionModelName, nr, rr, er)
}

func newDBReportGeneratorWithModel(model string, nr *NoteRepo, rr *ReportRepo, er *ReportExampleRepo) (*gptReportGenerator, error) {
    key := os.Getenv("OPENAI_API_KEY")
    if key == "" { return nil, fmt.Errorf("OPENAI_API_KEY not set") }
    return &gptReportGenerator{
        client: openai.NewClient(key), model: model,
        noteRepo: nr, reportRepo: rr, exampleRepo: er,
    }, nil
}
```

Same shape for `gptExtractor`: add `model` field, default constructor delegates to `NewGPTExtractorWithModel(ProductionModelName)`. Replace the inline `Model: ProductionModelName` in `Extract` (extract.go:66) with `Model: e.model`.

`ProductionModelName` references shrink to constructor defaults + the prompt-versioning hash. Per-call hardcodes go away.

**Stamping on writes:** `Generate` and `Regenerate` currently stamp `ProductionModelName` on the saved `Report` row (report_generator.go:94, 139). Change to `g.model`. Production behavior unchanged (same default); eval doesn't save anyway.

**Note for extractor consumers:** `extract.go:62` `Extract` is called from production (where model = `ProductionModelName`) and from extract_test.go / integration_test.go. Existing call sites pass through `newGPTExtractor()` which keeps the default. No test changes needed.

### 3. New: `backend/cmd/eval-cli/main.go`

```go
// Subcommands (os.Args[1]):
//   eval-cli extract          <prompt> <options_json> <context_json>
//   eval-cli generate-report  <prompt> <options_json> <context_json>
//
// Promptfoo exec provider appends three positional args after the subcommand:
//   argv[2] = rendered prompt string (already built from promptfoo template)
//   argv[3] = JSON provider options
//   argv[4] = JSON context: { vars: {...}, prompt: {...}, test: {...} }
//
// Both write {output: "..."} JSON to stdout. Errors → stderr + non-zero exit.
//
// Env: OPENAI_API_KEY (required); EVAL_MODEL (optional, defaults to ProductionModelName).
```

Implementation outline:

```go
func main() {
    model := os.Getenv("EVAL_MODEL")
    if model == "" { model = handler.ProductionModelName }

    // argv: [eval-cli, subcommand, prompt, options, context]
    prompt   := os.Args[2]          // rendered prompt from promptfoo template
    var ctx  evalContext
    json.Unmarshal([]byte(os.Args[4]), &ctx) // context.vars holds fixture data

    switch os.Args[1] {
    case "extract":
        // vars: {transcript, classes}
        ext, _ := handler.NewGPTExtractorWithModel(model)
        req := handler.ExtractRequest{
            Transcript: ctx.Vars["transcript"],
            Classes:    ctx.Vars["classes"],
        }
        out, err := ext.Extract(context.Background(), req)
        // emit {output: json.Marshal(out)}
    case "generate-report":
        // vars: {student_name, class, start_date, end_date, notes (JSON), examples (JSON), instructions}
        // The promptfoo-rendered prompt (argv[2]) is ignored — BuildReportPrompt is the
        // canonical source of truth for the prompt. promptfoo supplies raw data; we build.
        var notes    []handler.Note
        var examples []handler.ReportExample
        json.Unmarshal([]byte(ctx.Vars["notes"]), &notes)
        json.Unmarshal([]byte(ctx.Vars["examples"]), &examples)
        builtPrompt := handler.BuildReportPrompt(
            ctx.Vars["student_name"], ctx.Vars["class"],
            ctx.Vars["start_date"],   ctx.Vars["end_date"],
            notes, examples, ctx.Vars["instructions"], "",
        )
        client := openai.NewClient(os.Getenv("OPENAI_API_KEY"))
        html, err := handler.GenerateReportHTML(context.Background(), client, model, builtPrompt)
        // emit {output: html}
    }
}

type evalContext struct {
    Vars map[string]string `json:"vars"`
}
```

**Key implication:** `BuildReportPrompt` **must** be exported. The promptfoo YAML template supplies raw fixture vars only — no prompt rendering. eval-cli owns prompt construction, ensuring it stays in sync with production.

~70 LOC including JSON decoding, stdout encoding, error handling.

### 4. New: `backend/cmd/eval-cli/main_test.go`

Port the existing `eval_endpoint_test.go` shape — feed JSON in via a `run(args, stdin, stdout) error` function, parse JSON out. No coverage loss.

### 5. Modify: `backend/evals/promptfooconfig.yaml`

Replace HTTP providers with exec:

```yaml
providers:
  - id: exec:../bin/eval-cli extract
    label: gradebee-extract
  - id: exec:../bin/eval-cli generate-report
    label: gradebee-report
```

(Verify exact promptfoo exec-provider syntax + how it pipes test `vars`. If stdin-as-JSON isn't direct, eval-cli adapts to whatever shape promptfoo passes — same JSON content, possibly via argv or env.)

Test bodies stay identical — `body` var continues to reference the same fixture files.

### 6. Modify: `backend/Makefile`

```makefile
.PHONY: eval eval-baseline bin/eval-cli

bin/eval-cli:
	@mkdir -p bin
	go build -o bin/eval-cli ./cmd/eval-cli

eval: bin/eval-cli
	@cd evals && npx --yes promptfoo@latest eval \
	  --config promptfooconfig.yaml \
	  --output results/$$(date +%Y%m%d-%H%M%S).json
	@echo
	@echo '=== Diff vs baseline ==='
	@node evals/scripts/diff-baseline.js evals/baseline.json $$(ls -t evals/results/*.json | head -1)

eval-baseline: eval
	@LATEST=$$(ls -t evals/results/*.json | head -1); \
	  cp "$$LATEST" evals/baseline.json; \
	  echo "Baseline updated to $$LATEST."
```

No background process, no `EVAL_TOKEN`, no `CLERK_SECRET_KEY=eval-placeholder` workaround, no `sleep 2`, no PID tracking.

### 7. Delete

- `backend/eval_endpoint.go`
- `backend/eval_endpoint_test.go`
- The `if evalToken := os.Getenv("EVAL_TOKEN"); …` block in `backend/cmd/server/main.go` (~lines 102-113), including the prod-env panic.
- `EVAL_TOKEN` lines in `.env.example` (~lines 32-33).

Add `bin/` to `.gitignore` if not already there.

### 8. Modify: `backend/ARCHITECTURE.md`

Replace "Eval HTTP endpoints (gated by EVAL_TOKEN)" section (~lines 392-401) and the `EVAL_TOKEN` env-var row (~line 343) with a short "Eval CLI" section pointing at `cmd/eval-cli` and `make eval`. Add `EVAL_MODEL` to the env-var table. Drop the prod-leak warning paragraph.

### 9. Modify: `backend/evals/README.md`

- "How it works": `make eval` builds `bin/eval-cli`; promptfoo invokes it per case via stdin.
- Remove server-lifecycle paragraphs.
- Add debug recipe: `echo '{...}' | ./bin/eval-cli generate-report | jq`.
- Add a one-line note explaining **why exec, not HTTP** so the next person doesn't re-derive this.
- Document `EVAL_MODEL` env var for trying alternate models.

### 10. Update: `docs/plans/2026-05-20-llm-evaluation-harness.md`

Strike the now-obsolete DoD bullets:
- "Production startup assertion: refuse to boot if `EVAL_TOKEN` is set in prod env"
- The `.env.example EVAL_TOKEN` bullet

Add a one-line note that Decision #9 was revisited and superseded by the exec approach (see this plan).

---

## Open questions

1. **Promptfoo exec vars contract.** ✅ **Resolved.** Promptfoo passes three positional CLI arguments:
   - `argv[1]` = rendered prompt string
   - `argv[2]` = JSON-encoded provider options
   - `argv[3]` = JSON-encoded context `{ vars, prompt, test }`

   `context.vars` contains the fixture variables (e.g. `transcript`, `classes`, `student_name`, `notes`, etc.). The exec process writes its response to **stdout** as either a plain string or `{output: "..."}` JSON.

   **Impact on eval-cli design:** The current plan had eval-cli reading JSON from stdin — that needs updating:
   - For `generate-report`: fixture vars (`student_name`, `notes`, `examples`, etc.) come from `context.vars` (argv[4]). eval-cli calls `BuildReportPrompt(...)` — the canonical Go implementation — then `GenerateReportHTML(...)`. The rendered `prompt` arg (argv[2]) from promptfoo is **ignored**; promptfoo YAML templates supply raw data only, never pre-built prompts. `BuildReportPrompt` **must** be exported.
   - For `extract`: transcript/classes come from `context.vars` (argv[4]) rather than stdin.
   - Subcommand dispatch can remain as the first argument (argv[1] in the promptfoo sense is the rendered prompt, so the binary is invoked as `eval-cli <prompt> <options> <context>` — no explicit subcommand in the argv sense; instead, use the `label` in options or a separate binary per provider, OR keep the subcommand and have promptfoo configured with two separate exec entries with distinct paths).

   **Recommended approach:** Two separate entrypoints using the same binary, differentiated by a subcommand prefix in the exec path:
   ```yaml
   providers:
     - id: "exec:bin/eval-cli extract"
     - id: "exec:bin/eval-cli generate-report"
   ```
   Promptfoo appends its three args after `extract`/`generate-report`, so `os.Args` will be `[eval-cli, extract, <prompt>, <options>, <context>]`. This keeps the dispatch clean.
2. **Helper location.** Keep `generateReportHTML` in `report_generator.go` (next to its main caller) or move to a new `gpt_call.go`? Recommend in `report_generator.go` — single tiny helper, no need for a new file.
3. **`bin/eval-cli` rebuild semantics.** Marking it `.PHONY` rebuilds every `make eval` invocation. `go build` is fast and idempotent, so this is fine — preferred over fragile dep tracking.
4. **Baseline parity.** Run `make eval` once before deleting the HTTP path to capture current scores, and again after the swap to confirm parity. Same prompts + same default model = scores within noise. Drift would signal an equivalence bug in the helper extraction.

---

## Rollout order

1. Add `generateReportHTML` helper. Refactor `gptReportGenerator.callGPT` to delegate. Add `model` field + `*WithModel` constructors to both generators. Run `make test` + `make lint`. *Production parity check — no behavioral change expected.*
2. Add `cmd/eval-cli/` + tests. Hand-test: `echo ... | ./bin/eval-cli generate-report`.
3. Add `bin/eval-cli` Make target.
4. Switch `promptfooconfig.yaml` to exec. Run `make eval` against current baseline — scores should be within noise.
5. Delete `eval_endpoint.go`, test, `cmd/server/main.go` block, `.env.example` lines.
6. Update `ARCHITECTURE.md`, `evals/README.md`, original plan DoD.
7. `cd backend && make lint && make test`.

Single PR. Step 1 is a pure-refactor commit; step 4 is the parity gate.
