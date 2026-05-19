-include .env
export

.PHONY: dev build build-frontend build-backend test clean \
        infra-up infra-provision infra

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
#   - ansible/secrets.yml populated (see docs/deployment.md); file is gitignored,
#     plain text is fine
#   - dokku_domain is set in ansible/vars.yml; override with DOKKU_DOMAIN=other.app if needed

# Provision cloud resources (S3 bucket, IAM, Cockpit token) via Terraform.
infra-up:
	cd terraform && terraform init && terraform apply

# Provision the VPS (Dokku, Alloy, backup cron, app config) via Ansible.
# ansible/secrets.yml is plain text (gitignored). If you encrypt it with
# ansible-vault, add --vault-password-file ~/.ansible/vault-pass to this command.
# dokku_domain defaults to the value in ansible/vars.yml; override with DOKKU_DOMAIN=other.app.
infra-provision:
	ansible-playbook -i ansible/inventory ansible/provision.yml \
		$(if $(DOKKU_DOMAIN),-e "dokku_domain=$(DOKKU_DOMAIN)") \
		-e @ansible/secrets.yml

# Run both steps in order (full first-time setup).
infra: infra-up infra-provision

# --- Test ---

test:
	cd backend && $(MAKE) test
	cd backend && $(MAKE) check-types
	pnpm -F frontend test

clean:
	rm -rf dist frontend/dist backend/dist
