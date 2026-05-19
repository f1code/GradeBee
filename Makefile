-include .env
export

.PHONY: dev build build-frontend build-backend test clean \
        infra-up infra-server infra-app infra-provision infra

# --- Local development ---

dev:
	pnpm -F frontend dev

# --- Build ---
#
# Production builds happen inside the Docker image (see Dockerfile). These
# targets exist for ad-hoc local builds only.

build-backend:
	cd backend && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/gradebee ./cmd/server

build-frontend:
	pnpm -F frontend build

# Build the production Docker image locally.
# Pass VITE_CLERK_PUBLISHABLE_KEY=... at minimum.
build:
	docker build \
		--build-arg VITE_CLERK_PUBLISHABLE_KEY=$(VITE_CLERK_PUBLISHABLE_KEY) \
		--build-arg VITE_API_URL=$(VITE_API_URL) \
		--build-arg VITE_SENTRY_DSN=$(VITE_SENTRY_DSN) \
		--build-arg VITE_APP_VERSION=$(VITE_APP_VERSION) \
		-t gradebee:local .

# --- Infrastructure ---
#
# One-time setup. See docs/deployment.md for full instructions.
#
# Prerequisites:
#   - SCW_ACCESS_KEY, SCW_SECRET_KEY, SCW_DEFAULT_PROJECT_ID set in environment
#   - .env.infra populated (copy from .env.infra.example; file is gitignored)
#   - For a staging environment: ENV_FILE=.env.staging make infra-app

# Provision cloud resources (S3 bucket, IAM, Cockpit token) via Terraform.
infra-up:
	cd terraform && terraform init && terraform apply

# Provision the VPS server level (apt, Dokku, Alloy, GHCR login, AWS CLI for backups).
# Safe to re-run at any time — does not touch any app.
infra-server:
	./scripts/provision-server.sh

# Provision a single app environment (create app, config vars, deploy image, TLS, backup cron).
# For a staging environment: ENV_FILE=.env.staging make infra-app
infra-app:
	./scripts/provision-app.sh

# Provision everything (server + app) in one pass — convenience wrapper for first-time setup.
# Prerequisite: push the Docker image to GHCR first (see docs/deployment.md).
infra-provision: infra-server infra-app

# Run both steps in order (full first-time setup).
# Prerequisite: push the Docker image to GHCR first (see docs/deployment.md).
infra: infra-up infra-provision

# --- Test ---

test:
	cd backend && $(MAKE) test
	cd backend && $(MAKE) check-types
	pnpm -F frontend test

clean:
	rm -rf dist frontend/dist backend/dist
