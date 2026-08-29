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
against them. Free text a *teacher* types is governed separately, and the split turns on whether the
teacher chose to send it. Where they wrote it to be read (feedback boxes) it is an accepted
exception. Where it rides along incidentally (recording filenames) the rule applies in full: both
upload paths log `file_ext` rather than the filename, so the format signal a support conversation
needs survives without the stem a teacher named after a child. What is logged is an extension we
*accept*, not whatever `filepath.Ext` returns — see Consequences for why that distinction is the
whole fix. See also Considered Options.

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
- **`upload_id` only, or a hash, in place of the filename.** Rejected in favour of logging
  `file_ext`, which is already computed and in scope at both sites. `upload_id` alone loses every
  format signal, and format is what upload and transcription failures turn on. A hash is worse than
  it looks: a stable hash of a child-named file is a pseudonym that links every event about that
  child over time, so it trades a readable name for a durable identifier — a different privacy
  question, not a smaller one. The extension carries the debugging value and nothing about the child.
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
  without a gate because they carry no child PII — from the roster or from a recording's own
  filename — a property this ADR establishes and tests enforce.
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
- **A recording's filename is telemetry's problem, not the teacher's.** `TestHandleUpload_OmitsFileName`
  and `TestHandleDriveImport_OmitsFileName` assert the absence of the name *value* at both upload
  sites, and assert the completion record still fires, so neither passes because the handler bailed
  out early. Each also asserts the response body still carries the filename: the teacher recognises
  their upload by the name they gave it, and stripping their copy would be the regression described
  two bullets up, not a privacy win.
- **`filepath.Ext` is not an extension, and swapping the field alone would not have closed this.**
  It returns everything after the final dot, so `Dr. Manoe 12 sept` — a name with a title prefix and
  no extension, ordinary on this product — yields `". Manoe 12 sept"`, putting the child's name into
  the field added to keep it out. The same value is concatenated into the on-disk name by
  `saveToUploadsDir`, and that path is logged at `voice_note_process.go` (Warn) and
  `voice_note_cleanup.go` (**Info, on every purge**) — and, because Go's `*PathError` embeds the
  path in `Error()`, inside `log.Error(..., "error", err)` strings that no field-name grep finds. So
  the stem had three further routes to Sentry beyond the field this task named.
- **`audioExtension` tests membership, not shape** — `allowedAudioExts`, the formats this endpoint
  accepts; anything else falls back to the declared MIME type. This is the part worth remembering,
  because the shape rule that came first (`^\.[a-z0-9]{1,5}$`) looked sufficient and was not: written
  without a space, `Dr.Manoe` makes the given name itself pass as an extension, and the names we see
  are mostly short and alphabetic. A rule that asks whether a string *looks like* a format will keep
  admitting names that look like formats; only a closed set does not. The fallback is free — nothing
  downstream reads the on-disk extension, since transcription is handed the teacher's original name.
- The absence tests run every assertion over six filenames — an accepted extension, an uppercase one,
  a spaced title prefix, an unspaced one, a trailing segment that is a given name, and a name with no
  dot — assert on the queued job's `FilePath` as well as the log record, and pick a request MIME type
  whose fallback differs from the well-formed case, so a helper that ignored the filename entirely
  would fail rather than pass. The general lesson: a field swap is only as good as the value put in
  the new field, and a value that also names a file names it everywhere that path is logged.
- **Residual risk, noted rather than closed:** `job.FileName` still reaches `Transcribe` and the
  OpenAI/Mistral SDKs. Our wrappers never interpolate it, but an SDK error that echoes the multipart
  filename would surface through `log.Error(..., "error", err)`. No field-name grep and no absence
  test in this repo can catch that — it depends on a third party's error strings. Anyone touching the
  transcription wrappers should treat a returned error as potentially carrying the filename.
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
