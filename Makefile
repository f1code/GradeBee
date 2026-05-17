-include .env
export

.PHONY: dev build build-frontend build-backend test clean

# --- Local development ---

dev:
	npm run --prefix frontend dev

# --- Build ---
#
# Production builds happen inside the Docker image (see Dockerfile). These
# targets exist for ad-hoc local builds only.

build-backend:
	cd backend && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/gradebee ./cmd/server

build-frontend:
	npm run --prefix frontend build

# Build the production Docker image locally.
# Pass VITE_CLERK_PUBLISHABLE_KEY=... at minimum.
build:
	docker build \
		--build-arg VITE_CLERK_PUBLISHABLE_KEY=$(VITE_CLERK_PUBLISHABLE_KEY) \
		--build-arg VITE_API_URL=$(VITE_API_URL) \
		--build-arg VITE_SENTRY_DSN=$(VITE_SENTRY_DSN) \
		--build-arg VITE_APP_VERSION=$(VITE_APP_VERSION) \
		-t gradebee:local .

# --- Deploy ---
#
# Production deployment is handled by Dokku via GitHub Actions on push to main.
# See docs/deployment.md. There is no Make target for deploy any more.

# --- Test ---

test:
	cd backend && $(MAKE) test
	cd backend && $(MAKE) check-types
	npm run --prefix frontend test

clean:
	rm -rf dist frontend/dist backend/dist
