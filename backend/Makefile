.PHONY: lint test dev

lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

test:
	go test -v -count=1 ./...

dev:
	cd cmd/server && go run .
