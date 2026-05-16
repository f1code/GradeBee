# GradeBee

GradeBee helps teachers record voice notes about students and automatically generate structured notes and report cards. Teachers only need to maintain a simple student list and upload voice recordings -- the system handles transcription, note organization, and report generation.

## How It Works

1. Teacher signs in with Google
2. Teacher adds classes and student names
3. Teacher uploads voice recordings
4. The system transcribes audio, extracts student names, and generates structured notes
5. On demand, the system aggregates notes into report cards per student

## Technology Stack

| Layer          | Technology                                             |
| -------------- | ------------------------------------------------------ |
| Frontend       | React 19, TypeScript, Vite                             |
| Routing        | react-router-dom v7                                    |
| Authentication | Clerk (Google OAuth)                                   |
| Backend        | Go 1.24, plain `net/http`                              |
| Storage        | Scaleway Object Storage                                |
| AI             | OpenAI Whisper (transcription), Claude (summarization) |
| Infrastructure | Scaleway (Object Storage + Serverless Functions)       |
| IaC            | Terraform                                              |

## Project Structure

```
GradeBee/
├── frontend/              # React SPA (Vite + TypeScript)
│   └── .env.example       # Browser env vars (VITE_*)
├── backend/               # Go API (plain net/http, vendored deps)
│   └── cmd/server/        # Local dev server entrypoint
├── e2e/                   # Playwright end-to-end tests
├── docs/                  # Design docs and implementation plans
├── Makefile               # build, clean, deploy, dev
├── package.json           # Root: runs frontend + backend concurrently
└── .env.example           # Backend + deployment env vars
```

## Documentation

- `backend/ARCHITECTURE.md` -- backend architecture and patterns
- `frontend/DESIGN.md` -- frontend design system
- `docs/` -- implementation plans and design docs
- `docs/analysis/` -- generated codebase analysis, diagrams, and quick references
- `AGENTS.md` -- guidance for AI/automation agents working on this repo

## Getting Started

### Prerequisites

- Node.js
- Go 1.24+
- A [Clerk](https://clerk.com) account configured with Google OAuth

### Setup

1. Copy `.env.example` to `.env` at the project root and fill in the backend/deployment values.

   Copy `frontend/.env.example` to `frontend/.env` and fill in the browser (Vite) values:

   ```
   VITE_CLERK_PUBLISHABLE_KEY=pk_test_xxx
   VITE_API_URL=http://localhost:8080

   # Sentry User Feedback (optional — leave blank to disable)
   VITE_SENTRY_DSN=https://xxx@oXXX.ingest.sentry.io/YYY
   ```

   > **Why two files?** Vite only reads `.env` from its own project directory (`frontend/`), and
   > `VITE_*` vars are inlined into the browser bundle at build time. Backend secrets
   > (`CLERK_SECRET_KEY`, `OPENAI_API_KEY`, …) must never appear in the bundle, so they live
   > in the root `.env` only.

2. Install dependencies:

   ```sh
   npm install
   cd frontend && npm install
   ```

3. Install git hooks (runs TypeScript check, ESLint, Prettier, and Go lint on commit):

   ```sh
   npm run prepare
   ```

4. Run the development servers:

   ```sh
   npm run dev
   ```

   This starts the frontend on `http://localhost:5173` and the backend on `http://localhost:8080`.

## Testing

End-to-end tests use [Playwright](https://playwright.dev) with [Clerk testing tokens](https://clerk.com/docs/testing/playwright) for authenticated flows.

```sh
# Run all e2e tests (starts the frontend dev server automatically)
npm run test:e2e

# Run with Playwright's interactive UI
npm run test:e2e:ui
```

The `CLERK_PUBLISHABLE_KEY` and `CLERK_SECRET_KEY` environment variables must be set (from `.env`) for the Clerk testing token integration to work.

## Implementation Status

The project follows a [phased implementation plan](docs/plans/2026-03-13-phased-implementation.md):

- **Phase 1** -- Auth (done)
- **Phase 2** -- Student List (done)
- **Phase 3** -- Voice Upload & Transcription (done)
- **Phase 4** -- Note Generation (done)
- **Phase 5** -- Report Card Generation (done)
- **Phase 6** -- Polish & deployment

## License

[GPL v3](LICENSE)
