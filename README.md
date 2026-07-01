# GradeBee

GradeBee helps teachers record voice notes about students and automatically generate structured notes and report cards. Teachers only need to maintain a simple student list and upload voice recordings -- the system handles transcription, note organization, and report generation.

## How It Works

1. Teacher signs in with Google
2. Teacher adds classes and student names
3. Teacher uploads voice recordings and example report cards
4. The system transcribes audio, extracts student names, and generates structured notes
5. On demand, the system aggregates notes into report cards per student

### Organizing Classes

Each class has a **level** (required) and an optional **schedule**.

- **Schedule** lets you run several classes of the same type side by side. Create
  multiple classes that share a level but differ by schedule — e.g. a
  "Maths" class with schedules "Period 1" and "Period 2". The schedule is purely
  organizational.
- **Level** also drives report style. When you upload example report
  cards, you tag each example with one or more levels. At report
  generation, GradeBee selects the examples tagged with the level being
  generated and uses them to match the writing style.

## Technology Stack

| Layer          | Technology                                             |
| -------------- | ------------------------------------------------------ |
| Frontend       | React 19, TypeScript, Vite                             |
| Routing        | react-router-dom v7                                    |
| Authentication | Clerk (Google OAuth)                                   |
| Backend        | Go 1.24, plain `net/http`                              |
| Storage        | SQLite database, local disk (audio)                    |
| AI             | Mistral (extraction, vision), Voxtral (transcription) |
| Infrastructure | VPS + Dokku (single container)                         |
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

- Node.js 24.13.x
- Go 1.24+
- A [Clerk](https://clerk.com) account configured with Google OAuth
- pnpm (enabled via Corepack: `corepack enable`)

### Setup

1. Copy `.env.example` to `.env` at the project root and fill in the backend/deployment values.

   Copy `frontend/.env.example` to `frontend/.env` and fill in the browser (Vite) values:

   ```
   VITE_CLERK_PUBLISHABLE_KEY=pk_test_xxx
   VITE_API_URL=http://localhost:8080

   # Sentry diagnostics (optional — leave blank to disable; requires in-app consent)
   VITE_SENTRY_DSN=https://xxx@oXXX.ingest.sentry.io/YYY
   ```

   > **Why two files?** Vite only reads `.env` from its own project directory (`frontend/`), and
   > `VITE_*` vars are inlined into the browser bundle at build time. Backend secrets
   > (`CLERK_SECRET_KEY`, `MISTRAL_API_KEY`, …) must never appear in the bundle, so they live
   > in the root `.env` only.

### Privacy and diagnostics

- **Clerk (necessary):** Google sign-in and session cookies are required to use GradeBee.
- **Sentry (optional diagnostics):** When `VITE_SENTRY_DSN` is set, error reporting, in-app feedback, and short session replays load only after you opt in via the privacy banner. Use **Privacy preferences** in the app footer to change your choice later.

2. Install dependencies:

   ```sh
   pnpm install
   ```

3. Install git hooks (runs TypeScript check, ESLint, Prettier, and Go lint on commit):

   ```sh
   pnpm run prepare
   ```

4. Run the development servers:

   ```sh
   pnpm run dev
   ```

   This starts the frontend on `http://localhost:5173` and the backend on `http://localhost:8080`.

## Testing

End-to-end tests use [Playwright](https://playwright.dev) with [Clerk testing tokens](https://clerk.com/docs/testing/playwright) for authenticated flows.

```sh
# Run all e2e tests (starts the frontend dev server automatically)
pnpm run test:e2e

# Run with Playwright's interactive UI
pnpm run test:e2e:ui
```

The `VITE_CLERK_PUBLISHABLE_KEY` (in `frontend/.env`) and `CLERK_SECRET_KEY` (in `.env`) must be set for the Clerk testing token integration to work.

## License

[GPL v3](LICENSE)
