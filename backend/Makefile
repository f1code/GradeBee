-include ../.env
export

.PHONY: lint test dev generate check-types eval eval-fixtures eval-extract eval-report eval-baseline eval-view bin/eval-cli

lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

test:
	go test -v -count=1 ./...
	node --test evals/scoring/extraction.test.js

# Live-LLM tests (llm_live_test.go) run as part of `test` when LLM_PROVIDER
# and the matching API key (OPENAI_API_KEY or MISTRAL_API_KEY) are set, and
# skip themselves otherwise.
dev:
	cd cmd/server && go run .

generate:
	tygo generate

check-types:
	tygo generate
	git diff --exit-code ../frontend/src/api-types.gen.ts || (echo "Generated types are out of date. Run 'make generate' in backend/" && exit 1)

# Run both extraction and report eval configs, then diff each against its own
# baseline. Requires: LLM_PROVIDER + the active provider's API key
# (OPENAI_API_KEY or MISTRAL_API_KEY).
#
# Extraction baseline is committed. Report baseline holds real student names,
# so it lives under data/ (gitignored) next to the DB.
EXTRACT_BASELINE := evals/baseline-extract.json
REPORT_BASELINE  := ../data/eval-baseline-report.json

# Fixed once per make invocation, so eval-baseline pins the files its own eval
# prerequisite wrote, never an older run's.
EVAL_STAMP     := $(shell date +%Y%m%d-%H%M%S)
EXTRACT_RESULT := evals/results/$(EVAL_STAMP)-extract.json
REPORT_RESULT  := evals/results/$(EVAL_STAMP)-report.json

bin/eval-cli:
	@mkdir -p bin
	go build -o bin/eval-cli ./cmd/eval-cli

# Report cases come from the local DB (see evals/README.md, "Report cases").
# Not DB_PATH: .env sets that for the server's working directory (cmd/server).
EVAL_DB ?= ../data/gradebee.db
EVAL_MANIFEST := evals/fixtures.manifest.json
EVAL_REPORT_TESTS := evals/tests.report.generated.yaml
GEN_REPORT_CASES = ./bin/eval-cli gen-report-cases -db $(EVAL_DB) -manifest $(EVAL_MANIFEST) \
	-cases-dir evals/fixtures/reports -tests $(EVAL_REPORT_TESTS)

# Writes on the DB land in the WAL first, so its mtime counts too.
$(EVAL_REPORT_TESTS): $(EVAL_MANIFEST) $(wildcard $(EVAL_DB) $(EVAL_DB)-wal) | bin/eval-cli
	$(GEN_REPORT_CASES)

# Regenerate report cases unconditionally.
eval-fixtures: bin/eval-cli
	$(GEN_REPORT_CASES)

# Regenerates report cases first when the manifest or DB changed.
# promptfoo exits non-zero when any assertion fails, so its exit code can't
# tell a failed run from a low score. A run that failed writes no output file.
eval: bin/eval-cli $(EVAL_REPORT_TESTS)
	-cd evals && pnpm exec promptfoo eval \
	  --config promptfooconfig.extract.yaml \
	  --output ../$(EXTRACT_RESULT)
	-cd evals && pnpm exec promptfoo eval \
	  --config promptfooconfig.report.yaml \
	  --output ../$(REPORT_RESULT)
	@echo; echo '=== Extraction diff ==='
	@node evals/scripts/diff-baseline.js $(EXTRACT_BASELINE) $(EXTRACT_RESULT)
	@echo; echo '=== Report diff ==='
	@node evals/scripts/diff-baseline.js $(REPORT_BASELINE) $(REPORT_RESULT)
	@test -f $(EXTRACT_RESULT) && test -f $(REPORT_RESULT) || { echo "A promptfoo run wrote no output: it failed, see above."; exit 1; }

# Run extraction eval only.
eval-extract: bin/eval-cli
	-cd evals && pnpm exec promptfoo eval \
	  --config promptfooconfig.extract.yaml \
	  --output results/$$(date +%Y%m%d-%H%M%S)-extract.json

# Run report eval only.
eval-report: bin/eval-cli $(EVAL_REPORT_TESTS)
	-cd evals && pnpm exec promptfoo eval \
	  --config promptfooconfig.report.yaml \
	  --output results/$$(date +%Y%m%d-%H%M%S)-report.json

# Open the promptfoo browser UI to browse eval results.
eval-view:
	cd evals && pnpm exec promptfoo view --yes

# Pin both baselines to this invocation's eval run. Commit the extraction
# baseline with the prompt or model change; the report baseline stays local.
eval-baseline: eval
	@node evals/scripts/pin-baseline.js \
	  $(EXTRACT_RESULT) $(EXTRACT_BASELINE) \
	  $(REPORT_RESULT) $(REPORT_BASELINE)
	@echo "Commit $(EXTRACT_BASELINE) with your prompt/model change."

