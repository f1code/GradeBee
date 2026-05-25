# Eval Harness — Promptfoo Drives the LLM

> **REQUIRED SUB-SKILL:** Use the executing-plans skill to implement this plan task-by-task.

**Goal:** Flip eval-cli from a full exec pipeline (builds prompt + calls LLM) to a pure prompt builder; let promptfoo's native OpenAI provider own the LLM call.

**Architecture:** Add `build-extract-prompt` and `build-report-prompt` subcommands to eval-cli that output a promptfoo messages array (no LLM call). JS prompt functions in `evals/prompts/` shell out to these subcommands. `promptfooconfig.yaml` switches from `exec:` providers to `openai:` provider with `response_format` for extraction. eval-cli loses its OpenAI client entirely.

**Tech Stack:** Go (eval-cli), Node/JS (promptfoo prompt functions), promptfoo YAML, OpenAI native provider.

---

## Why

The current eval-cli calls the LLM itself. That bypasses promptfoo's caching (re-runs always re-hit the model), cost/latency tracking, and multi-model comparison. Prompt building belongs in Go (single source of truth); model invocation belongs in promptfoo.

---

## Current state (what exists today)

- `backend/cmd/eval-cli/main.go` — subcommands `extract` and `generate-report`; each builds prompt + calls OpenAI
- `backend/report_generator.go` — exports `GenerateReportHTML` (used by production `callGPT` + eval-cli)
- `backend/extract.go` — exports `NewGPTExtractorWithModel` (production `newGPTExtractor` delegates to it; eval-cli uses it directly); `buildExtractionPrompt` is unexported
- `backend/evals/promptfooconfig.yaml` — uses `exec:../bin/eval-cli extract` and `exec:../bin/eval-cli generate-report` as providers

---

## Task 1: Export `BuildExtractionPrompt` and `extractResponseSchema`

**Files:**
- Modify: `backend/extract.go`

These are currently unexported. eval-cli's new `build-extract-prompt` subcommand needs to call `BuildExtractionPrompt`, and the schema JSON needs to appear in `promptfooconfig.yaml` (copy-paste, not a Go export — but confirm the schema is stable here first).

**Step 1: Rename and export**

In `backend/extract.go`, rename `buildExtractionPrompt` → `BuildExtractionPrompt` (capital B). Update the single call site inside `Extract`:

```go
systemPrompt := BuildExtractionPrompt(req.Classes)
```

**Step 2: Verify the existing test still references the right name**

```bash
rg "buildExtractionPrompt\|BuildExtractionPrompt" backend/
```

`backend/repo_student_alias_test.go` calls `buildExtractionPrompt` directly (package-internal test). Update that call to `BuildExtractionPrompt`.

**Step 3: Run tests**

```bash
cd backend && go test ./...
```

Expected: all pass.

**Step 4: Commit**

```bash
git add backend/extract.go backend/repo_student_alias_test.go
git commit -m "refactor: export BuildExtractionPrompt"
```

---

## Task 2: Add `build-extract-prompt` and `build-report-prompt` subcommands to eval-cli

**Files:**
- Modify: `backend/cmd/eval-cli/main.go`

These subcommands output a **promptfoo messages array** as JSON — the shape promptfoo expects from a prompt function that returns multiple roles:

```json
[
  {"role": "system", "content": "<built prompt>"},
  {"role": "user",   "content": "<user turn>"}
]
```

For `build-extract-prompt`, the user turn is `vars.transcript`.
For `build-report-prompt`, production sends the built prompt as a **single `user` message** (no system role — see `GenerateReportHTML` in `report_generator.go`). So the messages array is just one element.

promptfoo's exec prompt calling convention passes a **single JSON argument** to the binary: `./eval-cli '{"vars":{...},"config":{...}}'`. The `config` field carries per-prompt configuration (here: `task`). This is different from the exec *provider* convention (positional args). eval-cli needs to handle both:
- If `os.Args[1]` starts with `{` → exec-prompt mode: parse JSON, dispatch on `config.task`
- Otherwise → existing subcommand mode (kept for manual debugging)

**Step 1: Add `runBuildExtractPrompt`**

```go
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
        {"role": "user",   "content": transcript},
    })
}
```

**Step 2: Add `runBuildReportPrompt`**

```go
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
    systemPrompt := handler.BuildReportPrompt(studentName, class, notes, examples, instructions, "")
    // Production sends the built prompt as a single user message (no system role).
    return writeJSON([]map[string]string{
        {"role": "user", "content": systemPrompt},
    })
}
```

**Step 3: Wire into `run()` — add exec-prompt dispatch**

Add a top-level branch in `run()` for the exec-prompt calling convention:

```go
func run(args []string) error {
    if len(args) < 2 {
        return fmt.Errorf("usage: eval-cli <subcommand|json>")
    }

    // Exec-prompt mode: promptfoo passes a single JSON arg.
    if strings.HasPrefix(args[1], "{") {
        return runPromptMode(args[1])
    }

    // Subcommand mode (exec-provider / manual debugging).
    // ... existing switch ...
}

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
```

The existing subcommand cases for `build-extract-prompt` and `build-report-prompt` (from Step 3 above) remain, providing a manual debug path:
```bash
./bin/eval-cli build-extract-prompt '' '{}' '{"vars":{...}}'
```

**Step 4: Build and smoke-test**

```bash
cd backend && go build -o bin/eval-cli ./cmd/eval-cli

# Exec-prompt mode (as promptfoo will call it)
./bin/eval-cli '{"vars":{"transcript":"Alice read well today.","classes":[]},"config":{"task":"build-extract-prompt"}}'
```

Expected: JSON array with `system` and `user` messages.

```bash
./bin/eval-cli '{"vars":{"student_name":"Alice","class":"Grade 3A","notes":[{"date":"2026-01-15","summary":"Strong reader."}],"examples":[],"instructions":""},"config":{"task":"build-report-prompt"}}'
```

Expected: JSON array with one `user` message containing the built prompt.

**Step 5: Commit**

```bash
git add backend/cmd/eval-cli/main.go
git commit -m "feat(eval-cli): add build-extract-prompt and build-report-prompt subcommands"
```

---

## Task 3: Update `promptfooconfig.yaml` to use native OpenAI provider + exec prompts

**Files:**
- Modify: `backend/evals/promptfooconfig.yaml`

**Changes:**

1. Replace the two `exec:` providers with native openai providers (one per task type since `response_format` differs).
2. For extraction: add `response_format` with the full JSON schema from `extractResponseSchema()`.
3. Replace per-test `provider:` references; add top-level `prompts:` entries using **exec prompt** syntax pointing directly at the binary with `config.task`. No JS files needed.

**New providers section:**

```yaml
providers:
  - id: openai:chat:gpt-5.4-mini
    label: gradebee-extract
    config:
      response_format:
        type: json_schema
        json_schema:
          # Schema mirrors extractResponseSchema() in backend/extract.go.
          # If ExtractResponse changes, update this block to match.
          name: extract_response
          strict: true
          schema:
            type: object
            properties:
              students:
                type: array
                items:
                  type: object
                  properties:
                    name:        { type: string }
                    class:       { type: string }
                    quoted_text: { type: string }
                    confidence:  { type: number }
                    candidates:
                      type: array
                      items:
                        type: object
                        properties:
                          name:  { type: string }
                          class: { type: string }
                        required: [name, class]
                        additionalProperties: false
                  required: [name, class, quoted_text, confidence, candidates]
                  additionalProperties: false
              date: { type: string }
            required: [students, date]
            additionalProperties: false

  - id: openai:chat:gpt-5.4-mini
    label: gradebee-report
```

> Note: model is hardcoded as `gpt-5.4-mini`. `EVAL_MODEL` is no longer read by eval-cli. To test a different model, change `id:` in the YAML directly.

**New prompts section** (replaces per-test `prompt:` references — defined at top level):

```yaml
prompts:
  - label: extract-prompt
    raw: exec:../bin/eval-cli
    config:
      task: build-extract-prompt
  - label: report-prompt
    raw: exec:../bin/eval-cli
    config:
      task: build-report-prompt
```

**Per-test changes:** each extraction test gets `providers: [gradebee-extract]` and `prompt: extract-prompt`; each report test gets `providers: [gradebee-report]` and `prompt: report-prompt`. Example:

```yaml
  - description: "extraction: preserves teacher voice (no paraphrasing)"
    providers:
      - gradebee-extract
    prompt: extract-prompt
    vars:
      transcript: "file://fixtures/extraction/voice_preservation/transcript.txt"
      classes:    "file://fixtures/extraction/voice_preservation/classes.json"
    assert:
      - type: is-json
      - type: javascript
        value: file://scoring/extraction.js
        config:
          expected: file://fixtures/extraction/voice_preservation/expected.json
          metric: voice_preservation
```

**Step 1: Edit `promptfooconfig.yaml`** — add `prompts:` section, update `providers:`, add `prompt:` + update `provider:` label on every test.

**Step 2: Run the eval (baseline parity check)**

```bash
cd backend && make eval
```

Expected: scores within noise of baseline. Any significant regression indicates the prompt function output doesn't match what the old exec path was sending.

**Step 3: Commit**

```bash
git add backend/evals/promptfooconfig.yaml
git commit -m "feat(evals): switch to native openai provider + JS prompt functions"
```

---

## Task 4: Remove LLM machinery from eval-cli; unexport what's no longer needed

**Files:**
- Modify: `backend/cmd/eval-cli/main.go`
- Modify: `backend/report_generator.go`
- Modify: `backend/extract.go`

Now that promptfoo drives the LLM call, eval-cli no longer needs `GenerateReportHTML`, `NewGPTExtractorWithModel`, or an OpenAI client.

**Step 1: Delete `runExtract` and `runGenerateReport` from `main.go`**

Remove the `extract` and `generate-report` cases from the switch, the two functions, and the OpenAI import.

**Step 2a: Remove `EVAL_MODEL` dead code from `run()`**

After deleting `runExtract`/`runGenerateReport`, the `model` variable has no callers. Remove:

```go
model := os.Getenv("EVAL_MODEL")
if model == "" {
    model = handler.ProductionModelName
}
```

Model selection now lives entirely in the promptfoo provider config.

**Step 2b: Unexport `GenerateReportHTML`**

In `backend/report_generator.go`, rename `GenerateReportHTML` → `generateReportHTML`. Update the one production call site (`callGPT`).

**Step 3: Unexport `NewGPTExtractorWithModel`**

In `backend/extract.go`, rename `NewGPTExtractorWithModel` → `newGPTExtractorWithModel`. Update `newGPTExtractor()` (the only caller).

**Step 4: Build + test**

```bash
cd backend && go build ./... && go test ./...
```

Expected: no references to the old exported names; all tests pass.

**Step 5: Update `cmd/eval-cli/main_test.go`**

Remove test cases for `extract` and `generate-report` subcommands; add test cases for `build-extract-prompt` and `build-report-prompt`.

The new subcommands write via `writeJSON` to `os.Stdout` directly (return only `error`), so tests need stdout capture:

```go
// captureOutput redirects os.Stdout for the duration of f() and returns what was written.
// Not goroutine-safe — use only in sequential tests.
func captureOutput(f func() error) (string, error) {
    r, w, _ := os.Pipe()
    old := os.Stdout
    os.Stdout = w
    err := f()
    w.Close()
    os.Stdout = old
    var buf bytes.Buffer
    io.Copy(&buf, r)
    return buf.String(), err
}

func TestRunBuildExtractPrompt(t *testing.T) {
    ec := evalContext{Vars: mustRawMap(map[string]interface{}{
        "transcript": "Alice read well today.",
        "classes":    []interface{}{},
    })}
    out, err := captureOutput(func() error { return runBuildExtractPrompt(ec) })
    require.NoError(t, err)
    var msgs []map[string]string
    require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &msgs))
    require.Len(t, msgs, 2)
    assert.Equal(t, "system", msgs[0]["role"])
    assert.Equal(t, "Alice read well today.", msgs[1]["content"])
}
```

Same pattern for `TestRunBuildReportPrompt`: assert one message, `user` role, non-empty content.

**Step 6: Run tests again**

```bash
cd backend && go test ./cmd/eval-cli/...
```

**Step 7: Commit**

```bash
git add backend/cmd/eval-cli/ backend/report_generator.go backend/extract.go
git commit -m "refactor(eval-cli): remove LLM machinery; unexport GenerateReportHTML + NewGPTExtractorWithModel"
```

---

## Task 5: Update docs

**Files:**
- Modify: `backend/evals/README.md`
- Modify: `backend/ARCHITECTURE.md`

**README.md changes:**
- "How it works": update step 2 to describe JS prompt functions + native provider instead of exec pipeline
- "Debugging a single case": replace `eval-cli extract/generate-report` examples with `eval-cli build-extract-prompt/build-report-prompt`
- Add a one-line note explaining why promptfoo drives the LLM (caching, cost tracking, multi-model comparison)

**ARCHITECTURE.md changes:**
- Update `GenerateReportHTML` and `NewGPTExtractorWithModel` references to reflect they are now unexported
- Update the eval harness section to reflect the new flow
- Remove `EVAL_MODEL` from the eval-harness env-var table (no longer read by eval-cli; model selection now lives in promptfoo provider config)

**Step 1: Edit both files.**

**Step 2: Verify lint**

```bash
cd backend && make lint
```

**Step 3: Commit**

```bash
git add backend/evals/README.md backend/ARCHITECTURE.md
git commit -m "docs: update eval harness docs for promptfoo-driven LLM approach"
```

---

## Rollout order

1. Task 1 — export `BuildExtractionPrompt` (pure rename, no behaviour change)
2. Task 2 — add new subcommands + exec-prompt dispatch to eval-cli (additive, old subcommands still present)
3. Task 3 — switch `promptfooconfig.yaml`; **parity check** against baseline
4. Task 4 — remove old subcommands + unexport helpers
5. Task 5 — docs

Tasks 1–2 are safe at any point. Task 3 is the parity gate. Task 4 only after Task 3 is green.

---

## Open questions

None — all resolved during planning.
