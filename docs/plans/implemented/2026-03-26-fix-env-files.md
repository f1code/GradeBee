# Fix: Move VITE_* env vars to frontend/

## Goal
`make deploy` currently exports `.env` (which has dev values) as shell env vars, overriding `.env.production` at build time. Fix by moving frontend env vars into `frontend/.env*` and removing the `envDir` override.

## Problem
Makefile does `-include .env` + `export` → `VITE_API_URL=http://localhost:8080` becomes a shell env var → Vite prioritizes shell env vars over `.env.production` file → production build gets `localhost:8080`.

## Proposed Changes

### 1. Create `frontend/.env` (dev defaults)
```
VITE_CLERK_PUBLISHABLE_KEY=pk_test_bWFnbmV0aWMta29pLTYuY2xlcmsuYWNjb3VudHMuZGV2JA
VITE_API_URL=http://localhost:8080
VITE_GOOGLE_CLIENT_ID=527384016465-li61o00o9639d5bvufj2qbv2cce50j98.apps.googleusercontent.com
```

### 2. Create `frontend/.env.production` (prod values)
```
VITE_CLERK_PUBLISHABLE_KEY=pk_live_Y2xlcmsuZ3JhZGViZWUuZjFjb2RlLmNvbSQ
VITE_API_URL=/api
VITE_GOOGLE_CLIENT_ID=527384016465-li61o00o9639d5bvufj2qbv2cce50j98.apps.googleusercontent.com
```

### 3. Remove `envDir` from `frontend/vite.config.ts`
Delete the `envDir: path.resolve(__dirname, '..')` line and the `path` import.

### 4. Remove `VITE_*` lines from root `.env`
Keep only backend/infra vars.

### 5. Remove `VITE_*` lines from root `.env.production`
Keep only backend vars.

### 6. Update root `.env.example`
Remove `VITE_*` entries, add comment pointing to `frontend/.env`.

### 7. Create `frontend/.env.example`
```
VITE_CLERK_PUBLISHABLE_KEY=
VITE_API_URL=http://localhost:8080
VITE_GOOGLE_CLIENT_ID=
```

## Verification
- `make build-frontend` → grep dist for `/api` (not `localhost`)
- `cd frontend && npm run dev` → still uses `localhost:8080`
