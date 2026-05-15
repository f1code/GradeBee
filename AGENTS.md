# Agent Instructions

## Where to Find Information

Before starting work, consult the relevant doc:

| Topic | Source of Truth |
| --- | --- |
| Project overview, setup, tech stack | `README.md` |
| Backend architecture, patterns, conventions | `backend/ARCHITECTURE.md` |
| Frontend design system (colors, typography, components) | `frontend/DESIGN.md` |
| Implementation plans, RFCs, design docs | `docs/` |
| Deep codebase analysis, quick references, diagrams | `docs/analysis/` |
| End-to-end test examples | `e2e/` |
| Environment variables | `.env.example` |

When an authoritative doc exists for a topic, **read it first** rather than re-deriving knowledge from the code.

## Go Backend

Refer to `backend/ARCHITECTURE.md` for backend architecture, patterns, and implementation guidelines.
Update this document when the backend is updated.

After editing Go code in `backend/`, run lint to catch issues:

```bash
cd backend && make lint
```

Run this before considering Go changes complete.

## Frontend Design

Follow the design system documented in `frontend/DESIGN.md` for all UI work. Use the established color tokens, typography, and component patterns.

## Documentation Maintenance

Keeping docs in sync with code is part of "done". Before considering a task complete, check whether any of these triggers apply:

| When you... | Update... |
| --- | --- |
| Add/change an API endpoint, handler, repo, DI wiring, or job queue logic | `backend/ARCHITECTURE.md` |
| Add a SQL migration or change the schema | `backend/ARCHITECTURE.md` (schema section) |
| Add a new design token, component pattern, color, or typography rule | `frontend/DESIGN.md` |
| Add or rename an environment variable | `.env.example` (always) + `README.md` (if user-facing) |
| Change the tech stack or complete a phase | `README.md` |
| Complete or supersede an implementation plan | Mark status in the relevant `docs/plans/*.md` |
| Make a non-trivial architectural decision | Add a doc under `docs/` (consider an ADR-style filename) |

Definition of done for code changes:

1. Lint passes (`cd backend && make lint` for Go changes)
2. Tests pass (relevant unit/e2e suites)
3. **Docs updated** per the table above

If unsure whether a doc update is needed, prefer updating the authoritative doc over leaving it stale.

## Generated Analysis Files

Files under `docs/analysis/` (e.g. `CODEBASE_ANALYSIS.md`, `ARCHITECTURE_DIAGRAM.md`, `QUICK_REFERENCE.md`) are **generated snapshots** of the codebase at a point in time. Treat them as read-only reference material:

- **Do not** hand-patch them during feature work -- they will drift and that's expected.
- **Do** regenerate them on demand if the user asks for fresh analysis.
- **Do** put any new generated analysis or reference guides under `docs/analysis/` (not the project root).
- If the information belongs in an authoritative doc (`backend/ARCHITECTURE.md`, `frontend/DESIGN.md`, etc.), update that doc instead of producing a parallel analysis file.

## LLM

Gpt 5.4-mini is used for extraction + report generation. It is a real model.
