# Phase 1: Infrastructure + DB Layer

**Goal:** SQLite running, schema created, all Go repository types ready. No handler changes yet — Phase 2 wires repos into handlers.

**Parent plan:** [docs/plans/2026-03-27-replace-gdrive-storage.md](./2026-03-27-replace-gdrive-storage.md)

---

## 1. Dependencies

Add to `go.mod`:
```
modernc.org/sqlite
```

Pure Go, no CGO required. Compatible with `scratch`/`distroless` Docker images.

---

## 2. `backend/db.go` — Database Connection

**Purpose:** Open SQLite connection, set pragmas, return `*sql.DB`.

**Function:**
```
func OpenDB(path string) (*sql.DB, error)
```

**Behavior:**
1. `sql.Open("sqlite", path)` (driver registered by modernc.org/sqlite)
2. Run pragmas via `db.Exec`:
   - `PRAGMA journal_mode=WAL;`
   - `PRAGMA busy_timeout=5000;`
   - `PRAGMA foreign_keys=ON;`
3. `db.Ping()` to verify connection
4. Return `*sql.DB`

**Error handling:** Return wrapped errors with context (`fmt.Errorf("open db: %w", err)`).

**Notes:**
- SQLite with WAL mode supports concurrent reads + single writer, sufficient for our single-server deploy.
- `busy_timeout` prevents immediate `SQLITE_BUSY` errors under light contention.

---

## 3. `backend/sql/001_init.sql` — Schema Migration

**Purpose:** Embedded SQL file with full schema from master plan.

**Exact contents:**

```sql
CREATE TABLE IF NOT EXISTS classes (
    id          INTEGER PRIMARY KEY,
    user_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(user_id, name)
);

CREATE TABLE IF NOT EXISTS students (
    id          INTEGER PRIMARY KEY,
    class_id    INTEGER NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(class_id, name)
);

CREATE TABLE IF NOT EXISTS notes (
    id          INTEGER PRIMARY KEY,
    student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    date        TEXT NOT NULL,
    summary     TEXT NOT NULL,
    transcript  TEXT,
    source      TEXT NOT NULL DEFAULT 'auto',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS reports (
    id          INTEGER PRIMARY KEY,
    student_id  INTEGER NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    start_date  TEXT NOT NULL,
    end_date    TEXT NOT NULL,
    html        TEXT NOT NULL,
    instructions TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS report_examples (
    id          INTEGER PRIMARY KEY,
    user_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS uploads (
    id           INTEGER PRIMARY KEY,
    user_id      TEXT NOT NULL,
    file_name    TEXT NOT NULL,
    file_path    TEXT NOT NULL,
    processed_at TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_classes_user ON classes(user_id);
CREATE INDEX IF NOT EXISTS idx_notes_student ON notes(student_id);
CREATE INDEX IF NOT EXISTS idx_notes_date ON notes(student_id, date);
CREATE INDEX IF NOT EXISTS idx_reports_student ON reports(student_id);
CREATE INDEX IF NOT EXISTS idx_report_examples_user ON report_examples(user_id);
CREATE INDEX IF NOT EXISTS idx_uploads_user ON uploads(user_id);
CREATE INDEX IF NOT EXISTS idx_uploads_cleanup ON uploads(processed_at)
    WHERE processed_at IS NOT NULL;
```

All `CREATE` statements use `IF NOT EXISTS` so the migration is idempotent.

---

## 4. `backend/migrate.go` — Migration Runner

**Purpose:** Embed SQL files from `sql/` directory, run them on startup in order.

**Approach:**
```
//go:embed sql/*.sql
var migrations embed.FS
```

**Function:**
```
func RunMigrations(db *sql.DB) error
```

**Behavior:**
1. Create a `_migrations` table: `CREATE TABLE IF NOT EXISTS _migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`
2. Read embedded `sql/` dir, sort filenames lexically
3. For each file not yet in `_migrations`: execute contents in a transaction, insert row into `_migrations`
4. Log each applied migration via `slog.Info`

**Error handling:** Wrap with migration filename context. If any migration fails, the transaction rolls back and the error is returned (server won't start with a broken schema).

**Why not a migration library?** The schema is simple and we only need forward migrations. A 30-line runner is preferable to a dependency.

---

## 5. Repository Layer

All repos live in package `handler` (matching existing convention). Each repo is a struct holding `*sql.DB`. All methods take `ctx context.Context` as first arg. All queries filter by `user_id` where applicable (multi-tenant isolation). Errors are returned as-is (wrapped with `fmt.Errorf`); callers in Phase 2 will map `sql.ErrNoRows` → 404 etc.

### 5.1 `backend/repo_class.go` — ClassRepo

**Struct:**
```go
type ClassRepo struct{ db *sql.DB }

type Class struct {
    ID        int64  
    UserID    string 
    Name      string 
    Position  int    
    CreatedAt string 
}

type ClassWithCount struct {
    Class
    StudentCount int
}
```

**Functions:**

| Signature | Query / Behavior |
|-----------|-----------------|
| `List(ctx, userID) ([]ClassWithCount, error)` | `SELECT c.*, COUNT(s.id) FROM classes c LEFT JOIN students s ON s.class_id=c.id WHERE c.user_id=? GROUP BY c.id ORDER BY c.position, c.name` |
| `Create(ctx, userID, name string) (Class, error)` | `INSERT INTO classes (user_id, name, position) VALUES (?,?,?)` — position = `SELECT COALESCE(MAX(position),0)+1 FROM classes WHERE user_id=?`. Return created row via `RETURNING`. |
| `Rename(ctx, userID string, id int64, name string) error` | `UPDATE classes SET name=? WHERE id=? AND user_id=?`. Check `RowsAffected==0` → not found. |
| `Delete(ctx, userID string, id int64) error` | `DELETE FROM classes WHERE id=? AND user_id=?`. Cascade deletes students+notes via FK. Check `RowsAffected`. |

**Error handling:** On UNIQUE constraint violation (duplicate class name), return a typed `ErrDuplicate` error so handlers can return 409.

### 5.2 `backend/repo_student.go` — StudentRepo

**Struct:**
```go
type StudentRepo struct{ db *sql.DB }

type Student struct {
    ID        int64
    ClassID   int64
    Name      string
    CreatedAt string
}
```

**Functions:**

| Signature | Query / Behavior |
|-----------|-----------------|
| `List(ctx, classID int64) ([]Student, error)` | `SELECT * FROM students WHERE class_id=? ORDER BY name` |
| `Create(ctx, classID int64, name string) (Student, error)` | `INSERT ... RETURNING` |
| `Rename(ctx, id int64, name string) error` | `UPDATE students SET name=? WHERE id=?` |
| `Move(ctx, id int64, newClassID int64) error` | `UPDATE students SET class_id=? WHERE id=?` |
| `Delete(ctx, id int64) error` | `DELETE FROM students WHERE id=?` — cascades notes |
| `GetByID(ctx, id int64) (Student, error)` | Needed for ownership checks in handlers |

**Ownership:** Handlers must verify student belongs to user's class before mutating. Add helper:
```
func (r *StudentRepo) BelongsToUser(ctx, studentID int64, userID string) (bool, error)
```
Query: `SELECT 1 FROM students s JOIN classes c ON s.class_id=c.id WHERE s.id=? AND c.user_id=?`

### 5.3 `backend/repo_note.go` — NoteRepo

**Struct:**
```go
type NoteRepo struct{ db *sql.DB }

type Note struct {
    ID         int64
    StudentID  int64
    Date       string // YYYY-MM-DD
    Summary    string
    Transcript *string // nil for manual notes
    Source     string  // "auto" | "manual"
    CreatedAt  string
    UpdatedAt  string
}
```

**Functions:**

| Signature | Query / Behavior |
|-----------|-----------------|
| `List(ctx, studentID int64) ([]Note, error)` | `SELECT * FROM notes WHERE student_id=? ORDER BY date DESC, created_at DESC` |
| `GetByID(ctx, id int64) (Note, error)` | Single note |
| `Create(ctx, note *Note) error` | `INSERT ... RETURNING id, created_at, updated_at` — fills in generated fields on the passed pointer |
| `Update(ctx, id int64, summary string) error` | `UPDATE notes SET summary=?, updated_at=strftime(...) WHERE id=?` |
| `Delete(ctx, id int64) error` | `DELETE FROM notes WHERE id=?` |
| `ListForStudents(ctx, studentIDs []int64, startDate, endDate string) ([]Note, error)` | Batch query for report generation — `WHERE student_id IN (?) AND date BETWEEN ? AND ?` |

**Note on `ListForStudents`:** Since `database/sql` doesn't support `IN (?)` with slices natively, build the placeholder string dynamically (`?,?,?`) and spread args. Keep it in the repo — no raw SQL in handlers.

### 5.4 `backend/repo_report.go` — ReportRepo

**Struct:**
```go
type ReportRepo struct{ db *sql.DB }

type Report struct {
    ID           int64
    StudentID    int64
    StartDate    string
    EndDate      string
    HTML         string
    Instructions *string
    CreatedAt    string
}
```

**Functions:**

| Signature | Query / Behavior |
|-----------|-----------------|
| `List(ctx, studentID int64) ([]Report, error)` | `SELECT id, student_id, start_date, end_date, instructions, created_at FROM reports WHERE student_id=? ORDER BY created_at DESC` — omit `html` for list (large field) |
| `GetByID(ctx, id int64) (Report, error)` | Full row including `html` |
| `Create(ctx, report *Report) error` | `INSERT ... RETURNING id, created_at` |
| `Delete(ctx, id int64) error` | `DELETE FROM reports WHERE id=?` |

### 5.5 `backend/repo_example.go` — ReportExampleRepo

**Struct:**
```go
type ReportExampleRepo struct{ db *sql.DB }

type ReportExample struct {
    ID        int64
    UserID    string
    Name      string
    Content   string
    CreatedAt string
}
```

**Functions:**

| Signature | Query / Behavior |
|-----------|-----------------|
| `List(ctx, userID string) ([]ReportExample, error)` | `SELECT * FROM report_examples WHERE user_id=? ORDER BY created_at DESC` |
| `Create(ctx, userID, name, content string) (ReportExample, error)` | `INSERT ... RETURNING *` |
| `Delete(ctx, userID string, id int64) error` | `DELETE FROM report_examples WHERE id=? AND user_id=?` |

### 5.6 `backend/repo_upload.go` — UploadRepo

**Struct:**
```go
type UploadRepo struct{ db *sql.DB }

type Upload struct {
    ID          int64
    UserID      string
    FileName    string
    FilePath    string
    ProcessedAt *string
    CreatedAt   string
}
```

**Functions:**

| Signature | Query / Behavior |
|-----------|-----------------|
| `Create(ctx, userID, fileName, filePath string) (Upload, error)` | `INSERT ... RETURNING *` |
| `GetByID(ctx, id int64) (Upload, error)` | Single row |
| `MarkProcessed(ctx, id int64) error` | `UPDATE uploads SET processed_at=strftime(...) WHERE id=?` |
| `ListStale(ctx, olderThan string) ([]Upload, error)` | `SELECT * FROM uploads WHERE processed_at IS NOT NULL AND processed_at < ?` — for cleanup goroutine |
| `Delete(ctx, id int64) error` | `DELETE FROM uploads WHERE id=?` |

---

## 6. Shared Error Types

**File:** `backend/repo_errors.go`

```go
var (
    ErrNotFound  = errors.New("not found")
    ErrDuplicate = errors.New("duplicate")
)
```

Repos return `ErrNotFound` when `RowsAffected==0` on update/delete, or `sql.ErrNoRows` on get. Repos detect SQLite unique constraint errors and wrap as `ErrDuplicate`. Phase 2 handlers map these to HTTP 404/409.

---

## 7. `backend/cmd/server/main.go` Changes

Add between Clerk init and queue init:

1. Read `DB_PATH` env var (default `/data/gradebee.db`)
2. `db, err := handler.OpenDB(dbPath)` — panic on error
3. `handler.RunMigrations(db)` — panic on error
4. `defer db.Close()`

Pass `db` to deps (see next section).

---

## 8. `backend/deps.go` Changes

Add to `deps` interface (additive only — existing methods untouched in Phase 1):
```go
GetDB() *sql.DB
GetClassRepo() *ClassRepo
GetStudentRepo() *StudentRepo
GetNoteRepo() *NoteRepo
GetReportRepo() *ReportRepo
GetExampleRepo() *ReportExampleRepo
GetUploadRepo() *UploadRepo
```

Add to `prodDeps`:
```go
type prodDeps struct {
    db *sql.DB  // new field
}
```

Each `Get*Repo()` returns `&XxxRepo{db: p.db}` (repos are stateless wrappers, cheap to create).

Replace `ServiceDeps()` with `NewProdDeps(db *sql.DB) deps`. The `main.go` call becomes `NewProdDeps(db)`. The existing `serviceDeps` variable pattern stays — tests can still swap the whole `deps` interface.

---

## 9. `docker-compose.yml` Changes

Add volume mount to `backend` service:
```yaml
backend:
  volumes:
    - gradebee-data:/data
```

Add to `volumes:`:
```yaml
gradebee-data:
```

The DB file lives at `/data/gradebee.db`, uploads at `/data/uploads/`. Single volume for both.

---

## 10. `.env.example` Changes

Add:
```
DB_PATH=/data/gradebee.db
```

---

## 11. Testing Strategy

**Unit tests for repos:** Use in-memory SQLite (`:memory:`) — call `OpenDB(":memory:")` + `RunMigrations`. Each test gets a fresh DB. No test containers needed.

**Test file:** `backend/repo_test.go` (or per-repo `repo_class_test.go` etc.)

**What to test per repo:**
- CRUD happy paths
- Unique constraint violations → `ErrDuplicate`
- Cascade deletes (delete class → students + notes gone)
- `BelongsToUser` ownership check
- `ListStale` with time filtering
- `ListForStudents` with date range

**Integration with existing test pattern:** Existing tests override `serviceDeps`. Phase 1 tests don't need to — they test repos directly against a real (in-memory) SQLite. Phase 2 will add repo getters to the `mockDepsAll` helper.

---

## 12. Verification Checklist

Before Phase 1 is considered done:

- [ ] `make lint` passes
- [ ] `make test` passes (existing tests still green — no handler changes)
- [ ] New repo tests pass with in-memory SQLite
- [ ] `OpenDB` + `RunMigrations` works with a file path (manual test)
- [ ] Schema matches master plan exactly
- [ ] All 6 repos have full CRUD + ownership helpers

---

## Resolved Questions

1. **`ServiceDeps()` signature change** — Replace `func ServiceDeps() deps` with `func NewProdDeps(db *sql.DB) deps`. Compile-time safety beats a smaller diff; `main.go` is the only call site. Phase 2 rewrites deps heavily anyway.
2. **`RETURNING` clause** — modernc.org/sqlite bundles SQLite ≥3.35; `RETURNING` works. Use it everywhere.
