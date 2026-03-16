-include .env
export

DIST_DIR := dist/functions
ZIP_FILE := $(DIST_DIR)/backend.zip
BACKEND_SRC := backend/handler.go backend/auth.go backend/setup.go backend/go.mod backend/go.sum

.PHONY: build clean

build: $(ZIP_FILE)

$(ZIP_FILE): $(BACKEND_SRC)
	@mkdir -p $(DIST_DIR)
	cd backend && \
		go mod vendor && \
		go build . && \
		zip -r ../$(ZIP_FILE) *.go go.mod go.sum vendor

clean:
	rm -rf dist

deploy:
	terraform -chdir=infra apply -auto-approve

dev:
	npm run dev
