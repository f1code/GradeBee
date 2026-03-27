# Drop NATS, In-Process Worker, VPS Deployment

## Goal

Replace NATS JetStream + Scaleway serverless with an in-memory job queue and goroutine worker running inside a single Go binary. Deploy to a VPS with Caddy (auto-HTTPS, static files, reverse proxy).

## Current State

- **Frontend drives pipeline synchronously**: upload → transcribe → extract → confirm → save notes (no NATS involvement)
- **NATS async pipeline exists** but is a parallel code path: `POST /jobs/process`, `GET /jobs`, `POST /jobs/retry`, `cmd/worker/`
- **Scaleway serverless** deployment via Terraform (zip upload, NATS trigger)
- **`UploadQueue` interface** already abstracts NATS — clean seam for replacement

## Proposed Changes

### 1. New `memQueue` — in-memory UploadQueue implementation

**New file: `backend/mem_queue.go`**

- Implements `UploadQueue` interface using `sync.RWMutex` + `map[string]UploadJob`
- `Publish()` stores job in map, sends `{userID, fileID}` to a buffered channel
- Background goroutine pool (e.g. 4 workers) reads from channel, calls `processUploadJob`
- No external dependencies
- Jobs lost on restart — acceptable per requirements

### 2. Wire `memQueue` into deps

**Edit: `backend/deps.go`**

- `prodDeps.GetUploadQueue()` returns a singleton `memQueue` instead of `natsUploadQueue`
- Remove `NATS_URL` / `NATS_CREDS` env var reads
- `memQueue` needs a reference to `deps` to call `processUploadJob` — pass at init time

### 3. Start worker goroutines from server entrypoint

**Edit: `backend/cmd/server/main.go`**

- After creating the `memQueue`, start its worker goroutines
- Handle graceful shutdown (context cancellation)
- This is now the **only** entrypoint (local + production)

### 4. Remove NATS code

**Delete:**
- `backend/nats.go` — NATS `UploadQueue` implementation
- `backend/nats_test.go` — NATS integration tests
- `backend/cmd/worker/main.go` — standalone NATS consumer
- `docker-compose.yml` — only had NATS

**Edit: `backend/upload_process.go`**
- Remove `handleJobProcess` and the `PROCESS_SECRET` auth logic
- Keep `processUploadJob` (used by memQueue workers)
- Remove `ProcessUploadJob` exported wrapper (was for cmd/worker)

**Edit: `backend/handler.go`**
- Remove `/jobs/process` route

**Edit: `backend/go.mod`**
- Drop `nats-io/nats.go`, `nats-io/nats-server` dependencies

### 5. Wire upload endpoints to dispatch jobs

**Edit: `backend/upload.go`**

- After successful Drive upload, call `queue.Publish()` to dispatch async processing
- Return `fileId` + `jobId` immediately (frontend polls `/jobs`)

**Edit: `backend/drive_import.go`**

- Same: after successful copy, dispatch job via `queue.Publish()`

### 6. Update server entrypoint for production use

**Rewrite: `backend/cmd/server/main.go`**

- Remove "local dev only" comments — this is now the production entrypoint
- Load `.env` only if present (graceful for both local and Docker)
- Init `memQueue` + start workers
- Graceful shutdown on SIGINT/SIGTERM

### 7. Add Dockerfile for backend

**New file: `Dockerfile`**

- Multi-stage: Go build → minimal runtime image
- Single binary, no external deps
- Expose port 8080

### 8. Add Caddyfile

**New file: `Caddyfile`**

- Automatic HTTPS via Let's Encrypt
- Serve frontend static files from `/srv/frontend`
- Reverse proxy `/api/*` to Go backend on `localhost:8080`
- Strip `/api` prefix before forwarding (backend routes don't have it)

```
{$DOMAIN:localhost} {
    handle /api/* {
        uri strip_prefix /api
        reverse_proxy backend:8080
    }
    root * /srv/frontend
    file_server
    try_files {path} /index.html  # SPA fallback
}
```

### 9. Replace docker-compose.yml

**Rewrite: `docker-compose.yml`**

```yaml
services:
  backend:
    build: .
    env_file: .env
    restart: unless-stopped

  caddy:
    image: caddy:latest
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - ./frontend/dist:/srv/frontend:ro
      - caddy-data:/data      # persists certs
      - caddy-config:/config
    environment:
      - DOMAIN=${DOMAIN:-localhost}
    restart: unless-stopped

volumes:
  caddy-data:
  caddy-config:
```

For production: set `DOMAIN=gradebee.yourdomain.com` in `.env`. Caddy auto-provisions TLS.
For local dev: defaults to `localhost` (Caddy serves HTTP, or self-signed).

### 10. Update deploy targets

**Edit: `Makefile`**

- `make build-frontend` — build frontend SPA (no Terraform dependency)
- `make docker` — build backend Docker image
- `make deploy` — SSH to VPS, pull repo, `docker compose up -d --build`
- Remove Scaleway/Terraform references
- Keep `make dev` for local Go dev (`go run cmd/server`)

### 11. Remove Scaleway infra

**Delete or archive: `infra/`** — no longer deploying to Scaleway serverless

**Update: `docs/deployment.md`** — document VPS + Docker Compose + Caddy deployment

### 12. Frontend API base URL

**No code changes** — `VITE_API_URL` env var changes from absolute URL to `/api` (relative, same-origin via Caddy). Set in `make build-frontend`.

### 13. Update ARCHITECTURE.md

**Edit: `backend/ARCHITECTURE.md`**

- Remove NATS references, Scaleway serverless, NATS trigger
- Document in-memory queue + goroutine workers
- Update env vars (remove NATS_URL, NATS_CREDS, PROCESS_SECRET)
- Update entrypoint description

## Migration Summary

| Remove | Add |
|--------|-----|
| `backend/nats.go` | `backend/mem_queue.go` |
| `backend/nats_test.go` | `Dockerfile` |
| `backend/cmd/worker/main.go` | `Caddyfile` |
| `infra/*.tf` | Updated `docker-compose.yml` |
| NATS deps in `go.mod` | Updated `docker-compose.yml` |
| `handleJobProcess` + `/jobs/process` route | |

## What Stays the Same

- `UploadQueue` interface (unchanged)
- `UploadJob` struct + status constants
- `processUploadJob` pipeline (transcribe → extract → notes)
- `GET /jobs`, `POST /jobs/retry` handlers (work against any `UploadQueue`)
- All other endpoints unchanged
- Frontend unchanged

## Open Questions

1. ~~**VPS provider?**~~ **Decided: Scaleway STARDUST1-S** (1 vCPU, 1GB RAM, Paris). Already have account from current setup.
2. **Domain name?** Needed for Caddy auto-HTTPS. Set via `DOMAIN` env var.
-> Use gradebee.f1code.com
3. **Frontend API base URL** — currently absolute (`VITE_API_URL`). Needs to change to `/api` (relative, same origin via Caddy). Simplifies CORS too — same-origin requests need no CORS headers.
-> yes
4. **`backend/Makefile` `dev` target** — currently starts NATS via docker-compose. Should just be `go run cmd/server` after migration.
-> yes
