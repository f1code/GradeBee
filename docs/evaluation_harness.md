# Eval Harness Implementation Summary

**Status:** Implemented (2026-05-25)

## Overview

Evolved the evaluation harness from an HTTP-based endpoint (dual LLM calls, code duplication, auth overhead) to a streamlined exec-based CLI pipeline where promptfoo owns all LLM calls and eval-cli focuses purely on prompt construction.

## Architecture

```
Promptfoo → exec:../bin/eval-cli <subcommand> <prompt> <options> <context>
                        ↓
                  [Parse context.vars]
                  [Build prompt in Go]
                  ↓
                  [eval-cli outputs messages JSON]
                  ↓
Promptfoo (native OpenAI provider) → OpenAI API → JSON response
```

## How to Use

### Run Full Eval Suite
```bash
cd backend
make eval
```

### Run Baseline Capture
```bash
cd backend
make eval-baseline
```

### Debug Single Extraction Case
```bash
cd backend
./bin/eval-cli '{"vars":{"transcript":"Alice read well.","classes":[]},"config":{"task":"build-extract-prompt"}}'
```

### Debug Single Report Case
```bash
cd backend
./bin/eval-cli '{"vars":{"student_name":"Alice","class":"Grade 3A","notes":[],"examples":[],"instructions":""},"config":{"task":"build-report-prompt"}}'
```

## Environment Variables

| Var | Used By | Purpose |
|-----|---------|---------|
| `OPENAI_API_KEY` | eval-cli, production | OpenAI authentication |
| `EVAL_MODEL` | eval-cli only | Override model for evaluation (default: `ProductionModelName`) |

## Verification

- All existing unit tests pass (`cd backend && go test ./...`)
- All existing integration tests pass
- Eval baseline parity confirmed (same prompts + same default model)
- Production behavior unchanged (same model, same prompt construction)
