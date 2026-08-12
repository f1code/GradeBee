# Issue tracker: Kanban

Issues and PRDs for this repo live as tasks on a local **kanban-md** board (one `.md`
file per task under `kanban/tasks/`). Use the `kanban-md` CLI for all operations; see
the `kanban-md` skill for the full command reference.

## Conventions

- **Create a task**: `kanban-md create "TITLE" --body "..."`. For multi-line / Markdown
  bodies, write the body to a temp file and pass `--body "$(cat /tmp/body.md)"`.
- **Read a task**: `kanban-md show <ID>` — includes the full body and all fields.
- **List tasks**: `kanban-md list --compact [--status ...] [--tag ...] [--priority ...]`.
  Use `--not-blocked --status todo` for ready work, `--blocked` for stuck items.
- **Comment on a task**: `kanban-md edit <ID> -a "..." -t` — appends a timestamped note.
- **Apply / remove labels**: tags — `kanban-md edit <ID> --add-tag "..."` / `--remove-tag "..."`.
- **Close**: `kanban-md move <ID> done`.

Statuses and priorities are board-specific — check `kanban-md board --compact` before using values.

## When a skill says "publish to the issue tracker"

Create a Kanban task.

## When a skill says "fetch the relevant ticket"

Run `kanban-md show <ID>`.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single task with **child** tasks as tickets, linked
via kanban-md's parent field.

- **Map**: a single task tagged `wayfinder:map`, holding the Notes / Decisions-so-far / Fog
  body. `kanban-md create "<title>" --tags wayfinder:map`.
- **Child ticket**: a task linked to the map via `--parent <map-id>`, with the question in
  the body. Tags: `wayfinder:<type>` (`research`/`prototype`/`grilling`/`task`).
  `kanban-md create "<question>" --parent <map-id> --tags wayfinder:task`. Once claimed, the
  ticket carries the driving dev's claim.
- **Blocking**: kanban-md's **native dependencies** — the canonical representation. Add an
  edge with `kanban-md edit <child> --add-dep <blocker-id>` (or at creation,
  `--depends-on <id1>,<id2>`). A ticket is unblocked when every dependency is at a terminal
  status (`done`); `kanban-md list --not-blocked` / `--unblocked` reflect this live.
- **Frontier query**: `kanban-md list --compact --parent <map-id> --not-blocked --status todo`
  — the map's open children with all deps resolved and no active claim; first in order wins.
- **Claim**: `kanban-md pick --claim <agent> --parent <map-id> --status todo --move in-progress`
  — atomically picks the next unclaimed, unblocked child of the map, claims it, and moves it
  to in-progress in one write. (`pick` orders by priority; use the frontier query above first
  if you need strict map order.) The claim is the session's first write.
- **Resolve**: `kanban-md edit <id> -a "<answer>" -t`, then `kanban-md move <id> done`, then
  append a context pointer (gist + link) to the map's Decisions-so-far with
  `kanban-md edit <map-id> -a "..." -t`.

