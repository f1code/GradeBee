-include ../.env
export

.PHONY: lint test dev generate check-types eval eval-extract eval-report eval-baseline eval-view bin/eval-cli

lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

test:
	go test -v -count=1 ./...

dev:
	cd cmd/server && go run .

generate:
	tygo generate

check-types:
	tygo generate
	git diff --exit-code ../frontend/src/api-types.gen.ts || (echo "Generated types are out of date. Run 'make generate' in backend/" && exit 1)

# Run both extraction and report eval configs, merge results, then diff vs baseline.
# Requires: LLM_PROVIDER + the active provider's API key (OPENAI_API_KEY or MISTRAL_API_KEY).
#
# Steps:
#   1. Build the eval-cli binary.
#   2. Run promptfoo eval for extract config.
#   3. Run promptfoo eval for report config.
#   4. Merge both result JSONs into one combined file.
#   5. Print a diff vs the pinned baseline.
bin/eval-cli:
	@mkdir -p bin
	go build -o bin/eval-cli ./cmd/eval-cli

eval: bin/eval-cli
	@TIMESTAMP=$$(date +%Y%m%d-%H%M%S); \
	cd evals && pnpm exec promptfoo eval \
	  --config promptfooconfig.extract.yaml \
	  --output results/$${TIMESTAMP}-extract.json; \
	pnpm exec promptfoo eval \
	  --config promptfooconfig.report.yaml \
	  --output results/$${TIMESTAMP}-report.json; \
	node scripts/merge-results.js results/$${TIMESTAMP}.json results/$${TIMESTAMP}-extract.json results/$${TIMESTAMP}-report.json
	@echo
	@echo '=== Diff vs baseline ==='
	node evals/scripts/diff-baseline.js evals/baseline.json $$(ls -t evals/results/*.json | grep -v '\-extract\|\-report' | head -1)

# Run extraction eval only.
eval-extract: bin/eval-cli
	-cd evals && pnpm exec promptfoo eval \
	  --config promptfooconfig.extract.yaml \
	  --output results/$$(date +%Y%m%d-%H%M%S)-extract.json

# Run report eval only.
eval-report: bin/eval-cli
	-cd evals && pnpm exec promptfoo eval \
	  --config promptfooconfig.report.yaml \
	  --output results/$$(date +%Y%m%d-%H%M%S)-report.json

# Open the promptfoo browser UI to browse eval results.
eval-view:
	cd evals && pnpm exec promptfoo view --yes

# Update the pinned baseline to the most recent eval run.
# Run this after a deliberate prompt or model change and commit the result.
# Merges the latest extract and report results before copying to baseline.json.
eval-baseline: eval
	@LATEST=$$(ls -t evals/results/*.json 2>/dev/null | grep -v '\-extract\|\-report' | head -1); \
	  if [ -z "$$LATEST" ]; then echo "No eval results found. Run 'make eval' first."; exit 1; fi; \
	  cp "$$LATEST" evals/baseline.json; \
	  echo "Baseline updated to $$LATEST. Commit evals/baseline.json with your prompt/model change."

