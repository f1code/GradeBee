# Replace Google Drive with DB-Backed CRUD

## Decisions

- **Option A: Full in-app CRUD** (chosen)
- **SQLite** (not Postgres) — single-file DB, no extra container, sufficient for single-server deploy
- **Auto-increment integer PKs** — simpler, faster (SQLite rowid alias), no UUID dep
- Teachers need to **edit and add** notes (not read-only)
- No migration from existing Drive data — clean slate
- Reports output as **HTML** (copy/paste friendly), no more Google Docs
- **Keep** Google OAuth + Drive Picker for audio import from Drive (download to local disk instead of copy-to-Drive)
- Clerk still uses Google OAuth; scopes narrowed to `drive.readonly` (just need to read/download picked files)

## DB Schema

IDs are `INTEGER PRIMARY KEY` (auto-increment, SQLite rowid alias). Timestamps stored as ISO8601 text.

```sql
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;

-- classes and students (the roster)
CREATE TABLE classes (
    id          INTEGER PRIMARY KEY,
    user_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(user_id, name)
);

CREATE TABLE students (
    id          INTEGER PRIMARY KEY,
    class_id    INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(class_id, name)
);

-- observation notes (created by pipeline or manually)
CREATE TABLE notes (
    id          INTEGER PRIMARY KEY,
    student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    date        TEXT NOT NULL,          -- YYYY-MM-DD
    summary     TEXT NOT NULL,          -- markdown
    transcript  TEXT,                   -- original transcript (null for manual notes)
    source      TEXT NOT NULL DEFAULT 'auto',  -- 'auto' | 'manual'
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- generated reports
CREATE TABLE reports (
    id          INTEGER PRIMARY KEY,
    student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    start_date  TEXT NOT NULL,
    end_date    TEXT NOT NULL,
    html        TEXT NOT NULL,
    instructions TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- example report cards (style references)
CREATE TABLE report_examples (
    id          INTEGER PRIMARY KEY,
    user_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- audio uploads (file stored on disk)
CREATE TABLE uploads (
    id           INTEGER PRIMARY KEY,
    user_id      TEXT NOT NULL,
    file_name    TEXT NOT NULL,
    file_path    TEXT NOT NULL,
    processed_at TEXT,                  -- set when job completes; NULL = not yet processed
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX idx_classes_user ON classes(user_id);
CREATE INDEX idx_notes_student ON notes(student_id);
CREATE INDEX idx_notes_date ON notes(student_id, date);
CREATE INDEX idx_reports_student ON reports(student_id);
CREATE INDEX idx_report_examples_user ON report_examples(user_id);
CREATE INDEX idx_uploads_user ON uploads(user_id);
CREATE INDEX idx_uploads_cleanup ON uploads(processed_at)
    WHERE processed_at IS NOT NULL;
```

## API Changes

### Removed Endpoints
| Endpoint | Reason |
|----------|--------|
| `POST /setup` | No Drive workspace to provision |

### Kept (modified)
| Endpoint | Change |
|----------|--------|
| `GET /google-token` | Stays — needed for Drive Picker on frontend |
| `POST /drive-import` | Now downloads file from Drive to local disk + `uploads` table (instead of copy-to-Drive) |

### New Endpoints
| Method | Path | Description |
|--------|------|-------------|
| **Classes** | | |
| GET | `/classes` | List user's classes with student counts |
| POST | `/classes` | Create a class |
| PUT | `/classes/:id` | Rename a class |
| DELETE | `/classes/:id` | Delete class + cascade students/notes |
| **Students** | | |
| GET | `/classes/:classId/students` | List students in a class |
| POST | `/classes/:classId/students` | Add a student |
| PUT | `/students/:id` | Rename / move student to different class |
| DELETE | `/students/:id` | Delete student + cascade notes |
| **Notes** | | |
| GET | `/students/:studentId/notes` | List notes for a student (date desc) |
| GET | `/notes/:id` | Get single note |
| POST | `/students/:studentId/notes` | Create a manual note |
| PUT | `/notes/:id` | Edit note summary |
| DELETE | `/notes/:id` | Delete a note |
| **Reports** | | |
| POST | `/reports` | Generate reports (same inputs, returns HTML) |
| POST | `/reports/:id/regenerate` | Regenerate with feedback |
| GET | `/students/:studentId/reports` | List reports for a student |
| GET | `/reports/:id` | Get single report HTML |
| DELETE | `/reports/:id` | Delete a report |

### Modified Endpoints
| Endpoint | Change |
|----------|--------|
| `GET /students` | Now reads from DB, returns `{classes: [{id, name, students: [{id, name}]}]}` |
| `POST /upload` | Saves file to disk, stores row in `uploads`, dispatches job |
| `GET /jobs` | Unchanged (in-memory queue) |

## Implementation Phases

### Phase 1: Infrastructure + DB Layer (2-3d)
**Goal:** SQLite running, schema created, Go repository layer ready.

| File | Change |
|------|--------|
| `docker-compose.yml` | Add `/data` volume mount for DB file + uploads. Remove need for postgres service. |
| `backend/db.go` | NEW — open SQLite (`modernc.org/sqlite`), set PRAGMAs (WAL, busy_timeout, foreign_keys), return `*sql.DB` |
| `backend/migrate.go` | NEW — embed + run migration SQL on startup |
| `backend/sql/001_init.sql` | NEW — schema above |
| `backend/cmd/server/main.go` | Init DB, pass to deps, run migrations |
| `backend/repo_class.go` | NEW — `ClassRepo` (list, create, rename, delete) |
| `backend/repo_student.go` | NEW — `StudentRepo` (list, create, rename, delete, move) |
| `backend/repo_note.go` | NEW — `NoteRepo` (list, get, create, update, delete) |
| `backend/repo_report.go` | NEW — `ReportRepo` (list, get, create, delete) |
| `backend/repo_example.go` | NEW — `ReportExampleRepo` (list, create, delete) |
| `backend/repo_upload.go` | NEW — `UploadRepo` (create, get, markProcessed, listStale) |
| `go.mod` | Add `modernc.org/sqlite` |

### Phase 2: Backend — Swap Implementations (3-4d)
**Goal:** All handlers use DB instead of Google APIs.

| File | Change |
|------|--------|
| `backend/deps.go` | Replace Google-backed getters with DB-backed ones. Remove `GoogleServices`, `GoogleServicesForUser`. Add `GetDB()`, repo getters. |
| `backend/handler.go` | New routes for classes/students/notes/reports CRUD. Remove `/setup`, `/google-token`, `/drive-import`. |
| `backend/roster.go` | New `dbRoster` impl reading from `ClassRepo`/`StudentRepo` |
| `backend/notes.go` | New `dbNoteCreator` impl writing to `NoteRepo` |
| `backend/metadata_index.go` | New `dbMetadataIndex` reading from `NoteRepo` |
| `backend/report_generator.go` | Return HTML string instead of creating Google Doc. Drop Drive/Docs deps. |
| `backend/report_examples.go` | New `dbExampleStore` impl |
| `backend/upload.go` | Save to disk + `UploadRepo` instead of Drive |
| `backend/upload_process.go` | Use DB-backed deps; note creation writes to `notes` table. Mark upload `processed_at` on success. |
| `backend/upload_cleanup.go` | NEW — goroutine that deletes processed audio files older than retention period (default 7d, `UPLOAD_RETENTION_HOURS`). |
| `backend/drive_import.go` | Rewrite: download file from Drive → save to local disk + `uploads` table → dispatch job. No more copy-to-Drive. |
| `backend/students.go` | Rewrite to read from DB |
| `backend/google.go` | Slim down: keep only Google Drive read client (for drive-import download). Remove Sheets/Docs. |
| `backend/auth.go` | Keep — still needed for Google OAuth token retrieval |
| **DELETE** | `clerk_metadata.go`, `setup.go`, `drive_store.go` |

### Phase 3: Frontend — Roster CRUD (2d)
**Goal:** Teachers can manage classes and students in-app.

| File | Change |
|------|--------|
| `frontend/src/api.ts` | Add class/student CRUD calls. Keep `getGoogleToken` + `importFromDrive`. |
| `frontend/src/components/StudentList.tsx` | Rewrite: inline add/edit/delete for students. Add class management. |
| `frontend/src/components/AddClassForm.tsx` | NEW — simple form to add a class |
| `frontend/src/components/AddStudentForm.tsx` | NEW — inline form to add student to a class |
| `frontend/src/components/DriveSetup.tsx` | DELETE |
| `frontend/src/App.tsx` | Remove setup flow. Keep Drive picker import in AudioUpload. |

### Phase 4: Frontend — Notes UI (2-3d)
**Goal:** Teachers can browse, edit, and manually add notes.

| File | Change |
|------|--------|
| `frontend/src/components/NotesList.tsx` | NEW — timeline of notes per student, date-grouped |
| `frontend/src/components/NoteEditor.tsx` | NEW — textarea/markdown editor for creating/editing a note |
| `frontend/src/components/StudentDetail.tsx` | NEW — student page showing notes list + add note button |
| `frontend/src/api.ts` | Add notes CRUD calls |

### Phase 5: Frontend — Reports UI (1-2d)
**Goal:** Generate reports, view HTML, copy to clipboard.

| File | Change |
|------|--------|
| `frontend/src/components/ReportGeneration.tsx` | Rewrite: show generated HTML inline instead of Drive links |
| `frontend/src/components/ReportViewer.tsx` | NEW — render HTML report with "Copy to clipboard" button |
| `frontend/src/components/ReportHistory.tsx` | NEW — list past reports per student |
| `frontend/src/api.ts` | Update report calls to handle HTML responses |

### Phase 6: Cleanup + Audio Storage (1d)
| File | Change |
|------|--------|
| `backend/` | Remove all `google.golang.org/api/*` vendor deps |
| `go.mod` | Remove Google Sheets/Docs deps. Keep `google.golang.org/api/drive/v3` (read-only, for imports). |
| `.env.example` | Remove Google-specific vars, add `DB_PATH` (default `/data/gradebee.db`) |
| `frontend/src/components/AudioUpload.tsx` | Keep Drive picker. No other changes needed — backend handles the storage change. |
| `Makefile` | Update deploy to backup `/data` volume (DB file + uploads) |

## File Storage for Audio

Local volume mounted in Docker (`/data/uploads/`). Files named `{upload_uuid}{ext}`.

### Cleanup Strategy

Audio files are only needed during pipeline processing (transcribe step). Once a job completes successfully, the file is expendable — the transcript and extracted notes are in the DB.

| Component | Detail |
|-----------|--------|
| `uploads` table | Add `processed_at TIMESTAMPTZ` — set when job reaches `done` |
| `backend/upload_cleanup.go` | NEW — `cleanProcessedUploads(ctx, db, maxAge)`: delete files from disk + rows from `uploads` where `processed_at < now() - maxAge` |
| `cmd/server/main.go` | Start a goroutine that runs cleanup every hour. Default retention: **7 days** after processing (configurable via `UPLOAD_RETENTION_HOURS` env var). Gives a window for retries/debugging. |
| Failed jobs | Files for failed jobs are **not** cleaned up (`processed_at` stays NULL). They're kept until the job is retried+completed or manually dismissed. |
| Dismissed jobs | `POST /jobs/dismiss` sets `processed_at = now()` so the file enters the cleanup window. |

`processed_at` and cleanup index are included in the main schema above (no separate migration needed for clean-slate).

This keeps the volume bounded — worst case is 7 days of audio × number of active users.

## Open Questions

1. **Markdown editor choice** — plain `<textarea>` for MVP, or lightweight editor (Tiptap) from the start? Recommend textarea + markdown rendering for v1.
2. **Multi-user isolation** — all queries filtered by `user_id` from Clerk JWT. No shared data between teachers.
3. **Backup strategy** — see below.

## Backup

SQLite `.backup` to Scaleway Object Storage via cron on the VPS. Runs outside Docker (host cron) to avoid coupling backup lifecycle to app container.

### Script: `scripts/backup-db.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

DB_PATH="${DB_PATH:-/opt/gradebee/data/gradebee.db}"
BUCKET="${S3_BUCKET:-s3://gradebee-backups}"
RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
TIMESTAMP=$(date -u +%Y%m%dT%H%M%SZ)
BACKUP_FILE="/tmp/gradebee-${TIMESTAMP}.db"

# Safe online backup (doesn't lock writers)
sqlite3 "$DB_PATH" ".backup '$BACKUP_FILE'"

# Upload to object storage
aws s3 cp "$BACKUP_FILE" "${BUCKET}/db/${TIMESTAMP}.db" --quiet

# Cleanup local temp
rm -f "$BACKUP_FILE"

# Prune old backups
aws s3 ls "${BUCKET}/db/" \
  | awk '{print $4}' \
  | sort \
  | head -n -${RETENTION_DAYS} \
  | xargs -I{} aws s3 rm "${BUCKET}/db/{}" --quiet 2>/dev/null || true
```

### Cron (VPS host)

```
# /etc/cron.d/gradebee-backup
0 */6 * * *  root  /opt/gradebee/scripts/backup-db.sh >> /var/log/gradebee-backup.log 2>&1
```

Every 6 hours, keep 30 backups (~7.5 days of history).

### Setup

- Scaleway IAM role attached to the instance with `ObjectStorageObjectAccess` scoped to the backup bucket — no static credentials on disk
- `aws` CLI configured with Scaleway S3 endpoint (`s3.fr-par.scw.cloud`), credentials sourced from instance metadata
- Bucket created via Terraform (already in stack for other Scaleway resources)
- `sqlite3` binary on host (standard on Debian/Ubuntu)

### Deployment

| File | Change |
|------|--------|
| `scripts/backup-db.sh` | NEW — backup script above |
| `terraform/iam.tf` | NEW — IAM application + policy (`ObjectStorageObjectAccess` scoped to backup bucket) + role attached to instance |
| `terraform/storage.tf` | Add `scaleway_object_bucket.gradebee_backups` resource |
| `Makefile` | `provision` target: installs cron job, configures `aws` CLI for Scaleway S3 endpoint, verifies `sqlite3` present |
