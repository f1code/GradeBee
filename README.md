# GradeBee

GradeBee helps teachers record voice notes about students and automatically generate structured notes and report cards. Teachers only need to maintain a simple student list and upload voice recordings -- the system handles transcription, note organization, and report generation.

## How It Works

1. Teacher signs in with Google
2. Teacher adds classes and student names
3. Teacher uploads voice recordings
4. The system transcribes audio, extracts student names, and generates structured notes
5. On demand, the system aggregates notes into report cards per student

On a phone, use the browser's **Add to Home Screen** / **Install** control. GradeBee supplies the name and parchment bee icon; the app then opens full-screen. Chrome on iOS cannot install a standalone web app -- use Safari on iPhone and iPad.

### Organizing Classes

Each class has a **level** (required) and an optional **time slot**.

- **Time slot** lets you run several classes of the same type side by side. Create
  multiple classes that share a level but differ by time slot — e.g. a
  "Maths" class with time slots "Period 1" and "Period 2". The time slot is purely
  organizational free text.
- **Level** also drives report style. Each level carries Report
  Instructions — the sections, tone, and content your reports must have.
  An admin writes them once; every report generated for that level follows
  them.

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

- Node.js 24.13.x (`.nvmrc`; this repo uses [fnm](https://github.com/Schniz/fnm): `eval "$(fnm env)" && fnm use`)
- Go 1.24+
- A [Clerk](https://clerk.com) account configured with Google OAuth and **Organizations enabled**
- pnpm (enabled via Corepack: `corepack enable`)

### User onboarding (invitation-only)

GradeBee is invitation-only. Every user must belong to a **Group** (Clerk Organization) — ungrouped access is not permitted.

- **New teachers** join via an organization invitation from their admin. The admin invites the teacher's email in the Clerk dashboard; the teacher accepts and signs up into the org with a `member` role.
- **Initial setup:** create the first Group and its admin user manually in the Clerk dashboard (see `backend/ARCHITECTURE.md` for the auth pattern). Open self-registration is disabled.

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
- **Sentry (optional diagnostics):** When `VITE_SENTRY_DSN` is set, error reporting, the bug-report button, and short session replays load only after you opt in via the privacy banner. Replays mask all on-screen text. Use **Privacy preferences** in the app footer to change your choice later.
- **What you type into a feedback box:** When a Sentry DSN is set, the text you write in a bug report, a suggestion, or a 👎 comment is forwarded to our diagnostics provider exactly as written — none of it is scrubbed. You asked for a person to read it, so it is not treated as passive diagnostics: the 👎 comment is sent whether or not you opted in above. Please leave student names out of all three; each box says so too. See [ADR 0003](docs/adr/0003-no-child-pii-in-telemetry.md).
- **Server logs (necessary):** When `SENTRY_DSN` is set, the server reports its own errors and operational logs without a consent gate. These carry no student name — not from your roster, where students appear as numeric ids, and not from a recording's file name, which is logged as its extension only, so a file you named after a child does not identify that child. The feedback boxes above are the one exception, and they take a different route out. [ADR 0003](docs/adr/0003-no-child-pii-in-telemetry.md) states the rule and the tests that hold it. Browser diagnostics stay opt-in for a different reason: a replay records how you moved through the app, timings and page addresses even with text masked, and that masking is a default we could change rather than a promise.

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
