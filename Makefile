-include .env
export

DIST_DIR := dist/functions
ZIP_FILE := $(DIST_DIR)/backend.zip

.PHONY: build clean build-frontend deploy-frontend deploy terraform dev

build:
	@mkdir -p $(DIST_DIR)
	rm -f $(ZIP_FILE)
	cd backend && \
		go mod vendor && \
		go build . && \
		zip -r ../$(ZIP_FILE) *.go go.mod go.sum vendor -x '*_test.go'

build-frontend:
	VITE_CLERK_PUBLISHABLE_KEY=$$(terraform -chdir=infra output -raw clerk_publishable_key 2>/dev/null) \
	VITE_API_URL=//$$(terraform -chdir=infra output -raw api_endpoint) \
	npm run --prefix frontend build

S3_ENDPOINT := https://s3.fr-par.scw.cloud
deploy-frontend: build-frontend
	$(eval BUCKET := $(shell terraform -chdir=infra output -raw frontend_bucket))
	@echo "Uploading assets/ with immutable cache headers..."
	aws s3 sync frontend/dist/assets/ s3://$(BUCKET)/assets/ \
		--endpoint-url $(S3_ENDPOINT) \
		--acl public-read \
		--cache-control "public, max-age=31536000, immutable" \
		--delete
	@echo "Uploading root files with short cache TTL..."
	aws s3 sync frontend/dist/ s3://$(BUCKET)/ \
		--endpoint-url $(S3_ENDPOINT) \
		--acl public-read \
		--cache-control "public, max-age=300" \
		--exclude "assets/*" \
		--delete

terraform:
	cd infra && terraform apply

deploy: build terraform deploy-frontend

dev:
	npm run --prefix frontend dev

clean:
	rm -rf dist
