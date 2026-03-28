# Local Cross-Compilation for VPS Deploy

## Goal

Stop building Go on the VPS (Stardust 1GB RAM can't handle it). Cross-compile locally on macOS ARM and ship the binary.

## Proposed Changes

### 1. Replace multi-stage Dockerfile with binary-only image — `Dockerfile`

```dockerfile
FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY gradebee /gradebee
EXPOSE 8080
CMD ["/gradebee"]
```

No more `golang` build stage.

### 2. Add build target — `Makefile`

New `build-backend` target that cross-compiles for linux/amd64:

```makefile
build-backend:
	cd backend && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../gradebee ./cmd/server
```

Update `deploy` to depend on `build-backend`, and include the `gradebee` binary in the rsync:

```makefile
deploy: build-backend build-frontend
	rsync -avz --delete \
		--include='docker-compose.yml' \
		--include='Caddyfile' \
		--include='Dockerfile' \
		--include='gradebee' \
		--include='frontend/' \
		--include='frontend/dist/***' \
		--exclude='*' \
		./ $(VPS_HOST):$(VPS_DIR)/
	ssh $(VPS_HOST) 'mkdir -p $(VPS_DIR)/data/uploads'
	ssh $(VPS_HOST) 'cd $(VPS_DIR) && docker compose up -d --build'
```

No longer rsyncing `backend/***`.

### 3. Add `gradebee` to `.gitignore`

### 4. Update `clean` target

```makefile
clean:
	rm -rf dist frontend/dist gradebee
```

### 5. Update `docs/deployment.md`

Note that Go is required locally for building. Remove any mention of Go building on the VPS.

## Files Changed

| File | Action |
|------|--------|
| `Dockerfile` | Remove build stage, just copy binary |
| `Makefile` | Add `build-backend`, update `deploy` and `clean` |
| `.gitignore` | Add `gradebee` |
| `docs/deployment.md` | Update |

## Open Questions

1. **Stardust architecture** — is it x86_64? Assuming yes (amd64). Need to confirm, otherwise target `GOARCH=arm64`.
-> confirmed
