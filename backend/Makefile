.PHONY: lint test dev generate check-types eval eval-baseline

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
# EVAL_TOKEN defaults to 'dev-eval-token' — override with EVAL_TOKEN=mysecret make eval.
#
# Steps:
#   1. Start the Go server in eval mode (EVAL_TOKEN set, no Clerk auth required).
#   2. Wait for it to be ready.
#   3. Run promptfoo eval.
#   4. Kill the server.
#   5. Print a diff vs the pinned baseline.
eval:
	@EVAL_TOKEN=$${EVAL_TOKEN:-dev-eval-token} \
	  CLERK_SECRET_KEY=eval-placeholder \
	  go run ./cmd/server & \
	  EVAL_PID=$$!; \
	  echo "Starting eval server (PID=$$EVAL_PID)..."; \
	  sleep 3; \
	  EVAL_RC=0; \
	  ( cd evals && \
	    EVAL_TOKEN=$${EVAL_TOKEN:-dev-eval-token} \
	    npx --yes promptfoo@latest eval \
	      --config promptfooconfig.yaml \
	      --output results/$$(date +%Y%m%d-%H%M%S).json ) || EVAL_RC=$$?; \
	  kill $$EVAL_PID 2>/dev/null; \
	  wait $$EVAL_PID 2>/dev/null; \
	  echo ""; \
	  echo "=== Diff vs baseline ==="; \
	  node evals/scripts/diff-baseline.js evals/baseline.json $$(ls -t evals/results/*.json 2>/dev/null | head -1); \
	  exit $$EVAL_RC

# Update the pinned baseline to the most recent eval run.
# Run this after a deliberate prompt or model change and commit the result.
eval-baseline: eval
	@LATEST=$$(ls -t evals/results/*.json 2>/dev/null | head -1); \
	  if [ -z "$$LATEST" ]; then echo "No eval results found. Run 'make eval' first."; exit 1; fi; \
	  cp "$$LATEST" evals/baseline.json; \
	  echo "Baseline updated to $$LATEST. Commit evals/baseline.json with your prompt/model change."

