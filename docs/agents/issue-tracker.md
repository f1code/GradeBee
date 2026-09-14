# Issue tracker: Kanban

Issues and PRDs for this repo live as tasks on a local **kanban-md** board (one `.md`
file per task under `kanban/tasks/`). Use the `kanban-md` CLI for all operations; see
the `kanban-md` skill for the full command reference.

## Conventions

- **Create a task**: `kanban-md create "TITLE" --body "..."`. For multi-line / Markdown
  bodies, write the body to a temp file and pass `--body "$(cat /tmp/body.md)"`.
- **Read a task**: `kanban-md show <ID>` — includes the full body and all fields.
- **List tasks**: `kanban-md list --compact [--status ...] [--tag ...] [--priority ...]`.
  Use `--unblocked --status todo` for ready work — see the blocking note below before
  reaching for `--blocked` / `--not-blocked`, which mean something else.
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
  edge at creation with `--depends-on <id1>,<id2>`, or afterwards with
  `kanban-md edit <child> --add-dep <blocker-id>`. Both work, and `--add-dep` is idempotent.
  A ticket is unblocked when every dependency is at a terminal status (`done`);
  `kanban-md list --unblocked` reflects this live.
- **Frontier query**: `kanban-md list --compact --parent <map-id> --unblocked --status todo`
  — the map's open children with all deps resolved and no active claim; first in order wins.
- **Claim**: `kanban-md pick --claim <agent> --parent <map-id> --status todo --move in-progress`
  — atomically picks the next unclaimed, unblocked child of the map, claims it, and moves it
  to in-progress in one write. (`pick` orders by priority; use the frontier query above first
  if you need strict map order.) The claim is the session's first write.
- **Resolve**: `kanban-md edit <id> -a "<answer>" -t`, then `kanban-md move <id> done`, then
  append a context pointer (gist + link) to the map's Decisions-so-far with
  `kanban-md edit <map-id> -a "..." -t`.

## Gotchas

Verified against kanban-md 0.38.0.

- **`--unblocked` is the dependency filter. `--blocked` / `--not-blocked` are not.** Those two
  read a separate manual `blocked: true` field in a task's frontmatter and ignore `depends_on`
  entirely, so `--not-blocked` happily lists a task whose blockers are all still open. Use
  `--unblocked` for the frontier, every time.
- **`show` does not print dependencies.** A task with `depends_on` set displays the same as one
  without. To confirm an edge landed, read the task file's frontmatter or query with
  `--unblocked` and check the task is absent.
