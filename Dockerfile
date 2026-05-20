# Stage 1: build frontend
FROM node:24.13.0-alpine AS frontend
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY frontend/package.json ./frontend/
RUN pnpm install --frozen-lockfile --filter ./frontend...
COPY frontend/ ./frontend/
WORKDIR /app/frontend

# Build-time args for the frontend bundle. Required:
#   VITE_CLERK_PUBLISHABLE_KEY — frontend/src/main.tsx asserts this.
# Optional:
#   VITE_API_URL (default /api — same-origin), VITE_SENTRY_DSN, VITE_APP_VERSION.
ARG VITE_CLERK_PUBLISHABLE_KEY
ARG VITE_API_URL=/api
ARG VITE_SENTRY_DSN=
ARG VITE_APP_VERSION=dev
ENV VITE_CLERK_PUBLISHABLE_KEY=$VITE_CLERK_PUBLISHABLE_KEY \
    VITE_API_URL=$VITE_API_URL \
    VITE_SENTRY_DSN=$VITE_SENTRY_DSN \
    VITE_APP_VERSION=$VITE_APP_VERSION
RUN pnpm build

# Stage 2: build Go binary with embedded frontend
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY backend/ ./backend/
# Replace the placeholder backend/static/ with the real frontend dist so embed.FS picks it up.
RUN rm -rf ./backend/static
COPY --from=frontend /app/frontend/dist ./backend/static/
WORKDIR /app/backend
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gradebee ./cmd/server

# Stage 3: minimal runtime
FROM alpine:latest
RUN apk add --no-cache bash ca-certificates poppler-utils
COPY app.json .
COPY --from=builder /gradebee /gradebee

# Promote build-time Sentry args into runtime env vars so the Go binary can read them.
ARG VITE_SENTRY_DSN=
ARG VITE_APP_VERSION=dev
ENV SENTRY_DSN=$VITE_SENTRY_DSN \
    SENTRY_RELEASE=$VITE_APP_VERSION

EXPOSE 8080
CMD ["/gradebee"]
