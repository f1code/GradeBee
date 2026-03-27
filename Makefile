-include .env
export

VPS_HOST ?= root@<VPS_IP>
VPS_DIR  ?= /opt/gradebee

.PHONY: dev build-frontend deploy test clean provision teardown backup backup-list backup-restore

# --- Local development ---

dev:
	npm run --prefix frontend dev

# --- Build ---

build-frontend:
	npm run --prefix frontend build

# --- Deploy to VPS ---

deploy: build-frontend
	rsync -avz --delete \
		--include='docker-compose.yml' \
		--include='Caddyfile' \
		--include='Dockerfile' \
		--include='backend/***' \
		--include='frontend/' \
		--include='frontend/dist/***' \
		--exclude='*' \
		./ $(VPS_HOST):$(VPS_DIR)/
	ssh $(VPS_HOST) 'mkdir -p $(VPS_DIR)/data/uploads'
	ssh $(VPS_HOST) 'cd $(VPS_DIR) && docker compose up -d --build'

# --- Test ---

test:
	cd backend && $(MAKE) test
	npm run --prefix frontend test

clean:
	rm -rf dist frontend/dist

# --- VPS provisioning ---

provision:
	cd terraform && terraform apply
	@echo "\nVPS IP: $$(cd terraform && terraform output -raw vps_ip)"

teardown:
	cd terraform && terraform destroy

# --- Backups ---

# Run backup manually on VPS
backup:
	ssh $(VPS_HOST) '$(VPS_DIR)/scripts/backup-db.sh'

# List existing backups
backup-list:
	ssh $(VPS_HOST) 'aws s3 ls s3://gradebee-backups/db/'

# Restore from latest backup
backup-restore:
	ssh $(VPS_HOST) 'LATEST=$$(aws s3 ls s3://gradebee-backups/db/ | sort | tail -1 | awk "{print \$$4}") && \
		aws s3 cp s3://gradebee-backups/db/$$LATEST /tmp/restore.db && \
		docker compose -f $(VPS_DIR)/docker-compose.yml stop backend && \
		cp /tmp/restore.db $(VPS_DIR)/data/gradebee.db && \
		docker compose -f $(VPS_DIR)/docker-compose.yml start backend'
