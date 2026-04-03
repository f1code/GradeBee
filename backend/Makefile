.PHONY: lint test dev generate check-types

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
