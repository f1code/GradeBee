# App Packaging (Dockerfile, embed.FS, migrate-only)

**Status:** implemented  
**Kanban task:** #14  
**Branch:** `task/14-app-packaging`

## Goal

Produce a single self-contained Docker image that:
- Builds the React frontend (with `VITE_APP_VERSION` injected)
- Embeds the built `dist/` into the Go binary via `embed.FS`
- Serves `/api/*` via existing handlers and everything else via SPA fallback
- Supports `--migrate-only` for Dokku predeploy hook
- Creates `app.json` for Dokku
- Removes Caddy from the deployment topology

## Proposed Changes

### 1. `Dockerfile` (rewrite)

Multi-stage build:

```dockerfile
# Stage 1: build frontend
FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
ARG VITE_APP_VERSION=dev
ENV VITE_APP_VERSION=$VITE_APP_VERSION
RUN npm run build

# Stage 2: build Go binary with embedded frontend
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY backend/ ./backend/
# Copy built frontend into backend/static/ so embed.FS can pick it up
COPY --from=frontend /app/frontend/dist ./backend/static/
COPY go.work* ./
RUN cd backend && go build -o /gradebee ./cmd/server

# Stage 3: runtime
FROM alpine:latest
RUN apk add --no-cache ca-certificates poppler-utils
COPY --from=builder /gradebee /gradebee
EXPOSE 8080
CMD ["/gradebee"]
```

Notes:
- Go module is `backend/go.mod` only (no top-level `go.work` currently); the `COPY go.work*` is a no-op if absent.
- Frontend dist is copied to `backend/static/` before the Go build.

### 2. `backend/static.go` (new file)

```go
package handler

import (
    "embed"
    "io/fs"
    "net/http"
    "strings"
)

//go:embed static
var staticFS embed.FS

// spaFS returns a sub-FS rooted at "static" for use as an http.FileSystem.
func spaFS() http.FileSystem {
    sub, _ := fs.Sub(staticFS, "static")
    return http.FS(sub)
}
```

### 3. `backend/handler.go` — SPA fallback in `Handle`

In the `default:` branch of the route switch, instead of returning 404, serve the embedded SPA:

```go
default:
    matched = false
    serveSPA(rec, r)
```

New `serveSPA` function (in `static.go` or `handler.go`):
- Hashed assets under `assets/` → `Cache-Control: public, max-age=31536000, immutable`
- `index.html` → `Cache-Control: no-cache`
- Use `http.FileServer(spaFS())` with fallback: if file not found, serve `index.html`

The SPA fallback must handle the case where the path doesn't match any file in the FS — serve `index.html` instead (mirroring Caddy's `try_files {path} /index.html`).

The existing CORS and `Content-Type: application/json` headers set at the top of `Handle` need to be scoped to API routes only (not static file serving).

### 4. `backend/cmd/server/main.go` — `--migrate-only` flag

Add at the top of `main()`, after loading env and opening the DB:

```go
if len(os.Args) > 1 && os.Args[1] == "--migrate-only" {
    if err := handler.RunMigrations(db); err != nil {
        slog.Error("migrations failed", "error", err)
        os.Exit(1)
    }
    slog.Info("migrations complete")
    os.Exit(0)
}
```

### 5. `app.json` (new, repo root)

```json
{
  "scripts": {
    "dokku": {
      "predeploy": "/gradebee --migrate-only"
    }
  }
}
```

### 6. `Caddyfile` — delete

### 7. `docker-compose.yml` — local-dev only

Replace with a minimal single-service file clearly labelled for local dev. Remove Caddy service entirely.

```yaml
# Local development only — not used in production (Dokku handles deployment)
services:
  backend:
    build: .
    env_file: .env
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
```

### 8. `.dockerignore` — update

The current `.dockerignore` excludes `frontend/`, but we now need it in the build. Update:
- Remove `frontend/` exclusion
- Keep `dist/`, `.env`, `.git/`, `docs/`, `infra/`, `*.md`

### 9. `backend/ARCHITECTURE.md` — update

- Update overview (no longer Docker Compose + Caddy)
- Add static file serving section
- Update entrypoint section to mention `--migrate-only`

## Open Questions

1. **`go.work`**: Is there a workspace file at the repo root? If not, the Dockerfile needs `go build` run from within `backend/`. Current Dockerfile copies `backend/dist/gradebee` — the build happens outside Docker. Confirm there's no `go.work`.
2. **`static/` directory in git**: The `backend/static/` dir will be populated only at Docker build time (copied from the node stage). It should be added to `.gitignore` (backend-level). Confirm this is acceptable.
3. **CORS headers for SPA**: Currently `Handle` sets `Content-Type: application/json` and CORS headers unconditionally. For static file responses these should be suppressed. Plan is to move those headers to only apply on API routes — confirm this won't break the frontend's assumptions.
4. **Vite `base` config**: Vite defaults to `/` as the base path. The SPA is served at `/` so this should be fine. Confirm there's no `base` config set that would change asset paths.
