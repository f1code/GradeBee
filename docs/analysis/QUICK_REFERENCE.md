# GradeBee Quick Reference

## Backend Request Flow

```
HTTP Request
    ↓
handler.go::Handle() [logs + route matching]
    ↓
authHandler middleware [verifies Clerk JWT]
    ↓
specific handler (e.g., handleCreateClass)
    ├─ Extract userID from request
    ├─ Decode JSON body
    ├─ Call service through serviceDeps DI
    │   └─ Service calls Repo (e.g., ClassRepo.Create)
    │       └─ Repo executes SQL query
    └─ Return JSON response via writeJSON()
        ↓
Client receives {error: "msg"} or {data}
```

## Frontend Component Flow

```
Component (e.g., AddClassForm)
    ├─ useState for form values + error + submitting
    ├─ useAuth() from Clerk for getToken
    └─ handleSubmit event
        ├─ setSubmitting(true), setError(null)
        ├─ try {
        │   ├─ getToken() → get JWT
        │   ├─ api.createClass(data, getToken)
        │   │   └─ fetch POST /classes with Bearer token
        │   └─ onCreated(result) callback
        │ } catch(err) {
        │   setError(err.message)
        │ } finally {
        │   setSubmitting(false)
        │ }
        ↓
    Render form with error state + disabled while submitting
```

## Database Owner Verification Chain

```
User (Clerk JWT) 
    ↓
classes (WHERE user_id = userID)
    ↓
students (WHERE class_id IN (...))
    ↓
notes/reports (WHERE student_id IN (...))
```

**Every CRUD operation verifies ownership before proceeding.**

## Key Type Mappings

| Go Type | TypeScript | Location |
|---------|-----------|----------|
| `ClassWithCount` struct | `ClassWithCount` interface | api-types.gen.ts |
| Error response | `{error: string, message?: string}` | All handlers |
| List response | `{items: T[]}` | handlers |
| Empty response | `{status: "ok"}` | handlers |

## Common HTTP Status Codes Used

- `200 OK` – Read/update successful
- `201 Created` – Resource created
- `400 Bad Request` – Validation error
- `401 Unauthorized` – No/invalid JWT
- `403 Forbidden` – User not owner
- `404 Not Found` – Resource doesn't exist
- `409 Conflict` – Duplicate constraint violated
- `500 Internal Server Error` – Unexpected error

## File Naming Conventions

| Pattern | Purpose | Example |
|---------|---------|---------|
| `repo_*.go` | Database CRUD | `repo_class.go`, `repo_student.go` |
| `*_handler.go` | HTTP handlers | `students.go`, `notes.go` |
| `*_test.go` | Tests | `handler_test.go` |
| `*.tsx` | React components | `AddClassForm.tsx` |
| `sql/00X_*.sql` | Migrations | `sql/001_init.sql` |

## Environment Variables

**Backend**:
- `CLERK_SECRET_KEY` – Required for JWT verification
- `OPENAI_API_KEY` – Required for Whisper/Claude
- `DB_PATH` – SQLite file path (default: `/data/gradebee.db`)
- `UPLOADS_DIR` – Audio storage (default: `/data/uploads`)
- `UPLOAD_RETENTION_HOURS` – Keep processed audio (default: 168)
- `ALLOWED_ORIGIN` – CORS origin (default: `*`)
- `PORT` – Server port (default: `8080`)

**Frontend**:
- `VITE_CLERK_PUBLISHABLE_KEY` – Clerk public key
- `VITE_API_URL` – Backend URL (default: `http://localhost:8080`)

## Common Commands

```bash
# Backend
cd backend && make lint           # Run Go linter
cd backend && make test           # Run tests
cd backend && make generate       # Generate TypeScript types
cd backend && make check-types    # Verify types are up-to-date

# Frontend
cd frontend && npm run dev        # Dev server
cd frontend && npm run build      # Production build
npm run test --prefix frontend    # Run tests

# Root
npm run dev                       # Run both backend + frontend
npm run test:e2e                  # Playwright E2E tests
make build-backend build-frontend # Production build
make deploy                       # Deploy to VPS
```

## Repo Pattern (Template)

```go
type FooRepo struct{ db *sql.DB }
type Foo struct {
    ID        int64  `json:"id"`
    UserID    string `json:"userId"`
    Name      string `json:"name"`
    CreatedAt string `json:"createdAt"`
}

func (r *FooRepo) List(ctx context.Context, userID string) ([]Foo, error) { ... }
func (r *FooRepo) Get(ctx context.Context, id int64, userID string) (Foo, error) { ... }
func (r *FooRepo) Create(ctx context.Context, userID, name string) (Foo, error) { ... }
func (r *FooRepo) Update(ctx context.Context, userID, id int64, name string) error { ... }
func (r *FooRepo) Delete(ctx context.Context, id int64, userID string) error { ... }
```

## Handler Pattern (Template)

```go
func handleListFoo(w http.ResponseWriter, r *http.Request) {
    userID, err := userIDFromRequest(r)
    if err != nil {
        writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
        return
    }
    
    items, err := serviceDeps.GetFooRepo().List(r.Context(), userID)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
        return
    }
    
    if items == nil {
        items = []Foo{}
    }
    
    writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}
```

## Component Pattern (Template)

```typescript
interface FooFormProps {
    onCreated?: (foo: Foo) => void
    onCancel?: () => void
}

export default function FooForm({ onCreated, onCancel }: FooFormProps) {
    const { getToken } = useAuth()
    const [name, setName] = useState('')
    const [error, setError] = useState<string | null>(null)
    const [submitting, setSubmitting] = useState(false)

    async function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setSubmitting(true)
        setError(null)
        try {
            const foo = await createFoo(name, getToken)
            onCreated?.(foo)
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed')
        } finally {
            setSubmitting(false)
        }
    }

    return (
        <motion.div initial={{opacity: 0}} animate={{opacity: 1}}>
            <form onSubmit={handleSubmit}>
                <input value={name} onChange={e => setName(e.target.value)} />
                {error && <p className="error">{error}</p>}
                <button type="submit" disabled={submitting}>{submitting ? 'Loading…' : 'Submit'}</button>
            </form>
        </motion.div>
    )
}
```

## API Call Pattern (Template)

```typescript
export async function createFoo(
    name: string,
    getToken: () => Promise<string | null>
): Promise<Foo> {
    const token = await getToken()
    const resp = await fetch(`${apiUrl}/foo`, {
        method: 'POST',
        headers: {
            Authorization: `Bearer ${token}`,
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ name }),
    })
    const body = await resp.json()
    if (!resp.ok) throw new Error(body.error || 'Failed')
    return body
}
```

## Design System Quick Lookup

| CSS Variable | Color | Use |
|---|---|---|
| `--honey` | #E8A317 | Primary buttons, links |
| `--honey-dark` | #C4880F | Hover state |
| `--honey-light` | #FFF3D4 | Highlight background |
| `--comb` | #F5E6C8 | Card background, border |
| `--ink` | #2C1810 | Primary text |
| `--ink-muted` | #7A6B5D | Secondary text |
| `--parchment` | #FBF7F0 | Page background |
| `--chalk` | #FFFFFF | Card surface |
| `--error-red` | #C53030 | Errors |
| `--success-green` | #38A169 | Success |

### Button Classes
- Default `<button>` → Primary style (honey background)
- `.btn-secondary` → White with comb border
- `.btn-danger` → Red background, white text
- `.btn-sm` → Smaller padding/font

### Fonts
- Display headings: `var(--font-display)` → Fraunces (serif)
- Body text: `var(--font-body)` → Source Sans 3 (sans-serif)

