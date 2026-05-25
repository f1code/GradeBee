-include ../.env
export

.PHONY: lint test dev generate check-types eval eval-baseline bin/eval-cli

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

# Run the promptfoo eval harness against the fixture set.
# Requires OPENAI_API_KEY in the environment.
# Optional: EVAL_MODEL overrides the model used for generation (defaults to ProductionModelName).
#
# Steps:
#   1. Build the eval-cli binary.
#   2. Run promptfoo eval (it invokes eval-cli per test case via exec provider).
#   3. Print a diff vs the pinned baseline.
bin/eval-cli:
	@mkdir -p bin
	go build -o bin/eval-cli ./cmd/eval-cli

eval: bin/eval-cli
	-cd evals && pnpm exec promptfoo eval \
	  --config promptfooconfig.yaml \
	  --output results/$$(date +%Y%m%d-%H%M%S).json
	@echo
	@echo '=== Diff vs baseline ==='
	node evals/scripts/diff-baseline.js evals/baseline.json $$(ls -t evals/results/*.json | head -1)

# Update the pinned baseline to the most recent eval run.
# Run this after a deliberate prompt or model change and commit the result.
eval-baseline: eval
	@LATEST=$$(ls -t evals/results/*.json 2>/dev/null | head -1); \
	  if [ -z "$$LATEST" ]; then echo "No eval results found. Run 'make eval' first."; exit 1; fi; \
	  cp "$$LATEST" evals/baseline.json; \
	  echo "Baseline updated to $$LATEST. Commit evals/baseline.json with your prompt/model change."

