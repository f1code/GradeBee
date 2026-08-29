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
against them. Free text a *teacher* types is governed separately, and in two different ways: where
the teacher wrote it to be read (feedback boxes) it is an accepted exception, and where it rides
along incidentally (recording filenames) it is open non-compliance. See Considered Options and
Consequences.

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
- **Scrubbing, gating or dropping the free text a teacher writes** — the thumbs-down comment
  (`ReportViewer.tsx`, `NotesList.tsx` → `sentry.go` feedback context) and the bug-report widget's
  message body (`FeedbackButton.tsx`). Rejected in favour of an explicit exception, the only one
  this ADR grants. A teacher who types into either box and presses Submit is acting deliberately,
  so that a person will read it; that is not passive diagnostics, and consent gating misreads what
  it is. Implicit consent covers the teacher's own text. It does **not** cover a child named
  inside that text — the teacher has no standing to consent on the child's behalf — and free-text
  name-stripping is the regex approach rejected above. So the residual risk is accepted rather
  than engineered away: both boxes ask for no student names, and `README.md` says the text is
  forwarded as written. Plumbing a consent flag to the backend was rejected separately: it is
  client-asserted, so it is weak evidence, and it adds a consent parameter to an API that has none.
  Consent is *not* what makes the widget path acceptable — the FAB is consent-gated and the
  thumbs-down path is not, and that difference is irrelevant to the child's data in either.

## Consequences

- The backend/frontend consent asymmetry is **intentional**, but not for the reason first written
  here. It does *not* rest on replay capturing on-screen text — it does not; see the replay bullet
  below. It rests on the two surfaces being different in kind. A backend log record is a fixed,
  enumerable set of fields that this ADR can state a rule about and a test can enforce. A replay is
  whatever the page happened to contain, plus interaction traces, DOM structure, timings and URLs
  that describe one identifiable teacher's session; and its text masking is a *default* rather than
  an invariant. Gating the wider, less enumerable surface — the one whose safety no test here
  asserts — is defence in depth. Backend logs are necessary operational telemetry, permitted
  without a gate because they carry no child PII from the roster — a property this ADR establishes,
  save the two filename paths listed below.
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
- Two paths remain **known non-compliance**: `voice_note_upload.go` and
  `voice_note_drive_import.go` log a recording's own `file_name` at `Info`, and
  `Manoe 12 sept.m4a` is an ordinary filename on this product. Closing them means deciding what a
  teacher sees instead of their own filename, which is a product question, not a logging one —
  tracked as #88. Until then, `README.md` states the exception rather than promising an absolute.
- The **feedback free-text boxes are a deliberate exception**, not an oversight — see Considered
  Options. Both the thumbs-down comment and the bug-report widget message ask for no student
  names, `README.md` says the text is forwarded as written, and tests cover each hint so the
  mitigation cannot be silently deleted. The thumbs-down hints use `aria-describedby`, because a
  mitigation a screen-reader user never hears is not a mitigation.
- Session replay is **not** among the gaps, and is called out as an explicit non-gap so the
  enumeration above is not read as silence about it. `replayIntegration()` (`sentryConsent.ts`) is
  constructed with no options, so `maskAllText`, `maskAllInputs` and `blockAllMedia` all default to
  on, as does `maskAttributes` for `title` / `placeholder` / `aria-label` — which matters here,
  because student names appear in `aria-label` on the roster screens. Nothing a teacher can see
  reaches a snapshot. This is a **default, not an invariant**: anyone who starts passing options to
  `replayIntegration()` re-opens the question, and that is part of why the frontend keeps its
  consent gate.
- Existing Sentry records are not purged. Retention is 30 days and this is fix-forward.
