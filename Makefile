-include .env
export

VPS_HOST ?= root@<VPS_IP>
VPS_DIR  ?= /opt/gradebee

.PHONY: dev build-frontend deploy test clean provision teardown

# --- Local development ---

dev:
	npm run --prefix frontend dev

# --- Build ---

build-frontend:
	VITE_API_URL=/api \
	npm run --prefix frontend build

# --- Deploy to VPS ---

deploy: build-frontend
	rsync -avz --delete \
		--exclude='.git' \
		--exclude='node_modules' \
		--exclude='infra' \
		./ $(VPS_HOST):$(VPS_DIR)/
	ssh $(VPS_HOST) 'cd $(VPS_DIR) && docker compose up -d --build'

# --- Test ---

test:
	cd backend && $(MAKE) test
	npm run --prefix frontend test

clean:
	rm -rf dist frontend/dist

# --- VPS provisioning ---

provision:
	bash scripts/provision-vps.sh

teardown:
	bash scripts/teardown-vps.sh
