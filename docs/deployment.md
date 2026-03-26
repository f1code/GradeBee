# Deployment

GradeBee runs on a VPS with Docker Compose: Go backend + Caddy (HTTPS + static files).

## Prerequisites

- VPS with Docker + Docker Compose (tested on Scaleway STARDUST1-S, Paris)
- Domain pointing to VPS IP (e.g. gradebee.f1code.com)
- SSH access to VPS
- Node.js (for frontend build, runs locally)

## VPS Setup (one-time)

Requires [Scaleway CLI](https://www.scaleway.com/en/cli/) configured (`scw init`).

    make provision

This creates a STARDUST1-S instance with Docker pre-installed via cloud-init.
The script outputs the IP and next steps (DNS, .env, first deploy).

To tear down:

    make teardown

## Configuration

Create `.env` on the VPS at `/opt/gradebee/.env`:

| Variable | Required | Description |
|----------|----------|-------------|
| `CLERK_SECRET_KEY` | Yes | Clerk backend API key |
| `OPENAI_API_KEY` | Yes | OpenAI API key (Whisper + GPT) |
| `VITE_CLERK_PUBLISHABLE_KEY` | Yes | Clerk publishable key (baked into frontend at build) |
| `DOMAIN` | Yes | Domain for Caddy HTTPS (e.g. `gradebee.f1code.com`) |
| `ALLOWED_ORIGIN` | No | CORS origin (default `*`, set to `https://yourdomain` in prod) |
| `LOG_LEVEL` | No | DEBUG/INFO/WARN/ERROR (default INFO) |
| `LOG_FORMAT` | No | `json` for JSON logs, else text |

## Deploy

From your local machine:

```bash
make deploy
```

This:
1. Builds the frontend SPA with `VITE_API_URL=/api`
2. Rsyncs the project to the VPS
3. SSHs in and runs `docker compose up -d --build`

Caddy automatically provisions TLS certificates on first request.

## Manual deploy / debugging

```bash
ssh root@<VPS_IP>
cd /opt/gradebee
docker compose up -d --build
docker compose logs -f
```

## Architecture

```
Internet → :443 → Caddy → /api/* → backend:8080 (Go)
                        → /*     → /srv/frontend (static SPA)
```

Single `docker-compose.yml` with two services. Caddy handles TLS + static files + reverse proxy.
