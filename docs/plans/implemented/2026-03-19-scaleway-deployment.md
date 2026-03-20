# Scaleway Deployment Plan

## Goal

Complete the deployment setup for GradeBee on Scaleway, modeled after the math-drill project. Fix gaps in existing Terraform config, add frontend deploy automation via AWS CLI.

## Current State

- Terraform in `infra/` already defines: serverless function (go124) + Object Storage bucket for frontend
- Root `Makefile` has `build` (backend zip) and `deploy` (terraform apply)
- Frontend uses `VITE_API_URL` and `VITE_CLERK_PUBLISHABLE_KEY` env vars

## Proposed Changes

### 1. Add missing variables — `infra/variables.tf`

```hcl
variable "openai_api_key" {
  sensitive = true
}

variable "clerk_publishable_key" {
  description = "Clerk publishable key for frontend build"
  default     = ""
}
```

### 2. Pass OPENAI_API_KEY to function — `infra/main.tf`

Add to `scaleway_function_namespace.gradebee.secret_environment_variables`:

```hcl
OPENAI_API_KEY = var.openai_api_key
```

### 3. Add outputs for frontend build — `infra/outputs.tf`

Add:

```hcl
output "frontend_bucket" {
  value = scaleway_object_bucket.frontend.name
}

output "clerk_publishable_key" {
  value = var.clerk_publishable_key
}
```

### 4. Update `infra/terraform.tfvars.example`

Add `clerk_publishable_key` field.

### 5. Rewrite `Makefile` — modeled on math-drill

```makefile
-include .env
export

DIST_DIR := dist/functions
ZIP_FILE := $(DIST_DIR)/backend.zip

.PHONY: build clean build-frontend deploy-frontend deploy terraform dev

build:
	@mkdir -p $(DIST_DIR)
	cd backend && go mod vendor && go build . && \
		zip -r ../$(ZIP_FILE) *.go go.mod go.sum vendor

build-frontend:
	VITE_CLERK_PUBLISHABLE_KEY=$$(terraform -chdir=infra output -raw clerk_publishable_key 2>/dev/null) \
	VITE_API_URL=//$$(terraform -chdir=infra output -raw api_endpoint) \
	npm run --prefix frontend build

S3_ENDPOINT := https://s3.fr-par.scw.cloud
deploy-frontend: build-frontend
	$(eval BUCKET := $(shell terraform -chdir=infra output -raw frontend_bucket))
	aws s3 sync frontend/dist/assets/ s3://$(BUCKET)/assets/ \
		--endpoint-url $(S3_ENDPOINT) \
		--cache-control "public, max-age=31536000, immutable" --delete
	aws s3 sync frontend/dist/ s3://$(BUCKET)/ \
		--endpoint-url $(S3_ENDPOINT) \
		--cache-control "public, max-age=300" \
		--exclude "assets/*" --delete

terraform:
	cd infra && terraform apply

deploy: build terraform deploy-frontend

dev:
	npm run --prefix frontend dev

clean:
	rm -rf dist
```

### 6. Add `docs/deployment.md`

Document:
1. Prerequisites: Scaleway account + API keys, Terraform, AWS CLI (configured for Scaleway S3)
2. Copy `infra/terraform.tfvars.example` → `infra/terraform.tfvars`, fill values
3. `cd infra && terraform init`
4. `make deploy` (builds backend → terraform apply → builds & uploads frontend)
5. AWS CLI S3 config for Scaleway (endpoint, credentials)

## Files Changed

| File | Action |
|------|--------|
| `infra/variables.tf` | Add `openai_api_key`, `clerk_publishable_key` |
| `infra/main.tf` | Add `OPENAI_API_KEY` to secret env vars |
| `infra/outputs.tf` | Add `frontend_bucket`, `clerk_publishable_key` |
| `infra/terraform.tfvars.example` | Add `clerk_publishable_key` |
| `Makefile` | Rewrite with `build-frontend`, `deploy-frontend`, `deploy` |
| `docs/deployment.md` | New — deployment guide |

## Open Questions

1. **Memory/timeout** — Current function is 256MB / 30s. Whisper transcription or GPT calls may need more. Bump to 512MB / 120s?
2. **Custom domain** — Handle later or include now?
