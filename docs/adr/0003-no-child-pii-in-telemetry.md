# Child PII does not leave the system into third-party telemetry

**Status:** accepted

## Context & Decision

GradeBee handles names of school children. Six backend sites shipped those names to Sentry —
four in `voice_note_process.go` (two silent-drop logs, two failure paths where the name was
interpolated into the step string) and two `log.Error` calls in `reports_handler.go`. Because
`logger.go` attaches the `sentryslog` handler whenever `SENTRY_DSN` is set, and because a failed
job logs its error twice, first names and their classes were queryable in Sentry in production.

We **remove student names at the emitting site**, and treat that as the standing rule: no student
name reaches a log, an error string, or any other payload bound for a third-party service.
Operational context stays — each record keeps whatever identifies the work rather than the child:
the job key (`user_id/upload_id`) on the voice-note paths, `student_id` on the report paths, plus
`class_name`, `confidence`, and the failure reason. `student_id` replaces the name wherever an
identifier is genuinely needed. Server-side Sentry identity was already pseudonymous
(`sentry.User{ID: <subject>}`), so with names gone the backend payload is pseudonymous
operational data.

The rule is stated for names the system holds — roster entries and what extraction matches
against them. It is **not yet fully true of free text a teacher types**; see Consequences.

Names still flow where they are the product: the LLM request that writes a report, the API
response rendered to the teacher who is entitled to see them, and the database.

## Considered Options

- **Scrub on the way out** — extend the existing `redactNames` / `scrubBeforeSend` filter in
  `sentry.go` to cover log attributes. Rejected: three separate defences of exactly this shape
  were already deployed against exactly this data and all three failed. `redactNames` requires
  two capitalised words, so a single first name passes, and its ASCII `[a-z]` class cannot match
  `Théo`; it also only ever inspected exception values, never log attributes. Sentry's
  `dataScrubber` defaults match *field names* (`password`, `token`, …), never `student`. And
  `sensitiveFields` was empty at both org and project level. Pattern-matching a name is a
  guess; over a codebase small enough to fix at source, the guess is strictly worse than the fix.
- **Gate backend logging behind the diagnostics consent flag**, matching the frontend. Rejected:
  it would make basic operational logging opt-in, and it treats the symptom. The reason the
  backend may log without consent is precisely that it carries no child PII — a property this
  ADR establishes rather than assumes.

## Consequences

- The backend/frontend consent asymmetry is **intentional and now defensible**. Frontend
  diagnostics stay behind `isDiagnosticsConsented()` because session replay captures screen
  content, which is child data by construction. Backend logs are necessary operational telemetry
  permitted without a gate because they carry none.
- Debugging a specific child's failed note now goes through `student_id`, resolved against the
  database by someone with access to it. This is deliberate friction, and the reason to keep
  `student_id` on the record rather than dropping identifiers entirely.
- **The teacher's copy and the telemetry copy are separate strings.** Stripping names from a
  message that a teacher reads would be a regression, not a privacy win: they are entitled to the
  name and cannot act on a row id. `failWith(step, detail, err)` in `voice_note_process.go` takes
  both — `step` is logged and re-logged by the queue worker, `detail` is what `JobStatus` renders
  — so neither audience is served the other's string. Any new user-facing failure text should
  follow that shape rather than reusing the telemetry wording.
- `TestProcessJob_DropSitesOmitStudentName` enforces the rule on the two drop paths, asserting on
  the name *value* rather than a field name so an interpolated name is caught too. New telemetry
  is expected to carry the same kind of assertion rather than rely on reviewer memory.
- Three paths still carry teacher-authored free text that can name a child, and are known
  non-compliance rather than exceptions to the rule: `voice_note_upload.go` and
  `voice_note_drive_import.go` log a recording's own `file_name` at `Info` (`Manoe 12 sept.m4a`
  is an ordinary filename on this product), and `sentry.go` puts a bug-report `comment` verbatim
  into the Sentry feedback context. Closing them means deciding what a teacher sees instead of
  their own filename, which is a product question, not a logging one — tracked as #88. Until
  then, `README.md` states the filename exception rather than promising an absolute.
- Existing Sentry records are not purged. Retention is 30 days and this is fix-forward.
