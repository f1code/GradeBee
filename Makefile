-include .env
export

VPS_HOST ?= root@<VPS_IP>
VPS_DIR  ?= /opt/gradebee

.PHONY: dev build-frontend deploy test clean provision teardown setup-infra backup backup-list backup-restore

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
		--include='scripts/***' \
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
	bash scripts/provision-vps.sh

teardown:
	bash scripts/teardown-vps.sh

# --- Infrastructure setup (backups + logging) ---

setup-infra:
	ssh $(VPS_HOST) 'apt-get update && apt-get install -y sqlite3 awscli'
	scp scripts/backup-db.sh $(VPS_HOST):$(VPS_DIR)/scripts/backup-db.sh
	ssh $(VPS_HOST) 'chmod +x $(VPS_DIR)/scripts/backup-db.sh'
	ssh $(VPS_HOST) 'cat > /etc/cron.d/gradebee-backup <<EOF\n0 */6 * * *  root  $(VPS_DIR)/scripts/backup-db.sh >> /var/log/gradebee-backup.log 2>&1\nEOF'
	ssh $(VPS_HOST) 'aws configure set default.s3.endpoint_url https://s3.fr-par.scw.cloud'
	ssh $(VPS_HOST) 'aws configure set default.region fr-par'
	bash scripts/install-alloy.sh

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
