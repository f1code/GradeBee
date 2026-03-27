# Phased Implementation Plan: Unified Upload Pipeline + Mobile Share

## Phase 1: NATS Infrastructure + Processing Pipeline
**Goal:** Backend can process uploads asynchronously via NATS. No frontend changes yet — existing web flow still works alongside.

- `nats.go` — connection, stream `UPLOADS`, KV bucket `UPLOAD_JOBS`, `UploadQueue` interface
- `upload_process.go` — consumer pipeline (transcribe → extract → auto-create notes), reusing existing `Transcriber`, `Extractor`, `NoteCreator`
- `jobs_list.go` — `GET /jobs`
- `jobs_retry.go` — `POST /jobs/retry`
- Tests against NATS test server

**Milestone:** Can manually publish a job to NATS and see notes auto-created in Drive.

## Phase 2: Web Upload → NATS
**Goal:** Web uploads go through NATS pipeline. Old synchronous flow removed.

- Modify `upload.go` + `drive_import.go` to publish to NATS, return immediately
- Remove `/transcribe`, `/extract`, `/notes` endpoints
- Frontend: simplify `AudioUpload.tsx` to upload + poll `GET /jobs` for status
- Frontend: remove `NoteConfirmation.tsx`, add `JobStatus.tsx`
- Frontend: "new" badge + delete button on notes list

**Milestone:** Web user uploads audio, walks away, comes back to see notes in list.

## Phase 3: Mobile App
**Goal:** Expo share-target app that uploads audio to the same pipeline.

- Expo project setup, Clerk auth with `expo-secure-store`
- `share_upload.go` — `POST /share-upload` (mobile endpoint, publishes to same NATS stream)
- Share intent handling (iOS + Android)
- `QueueScreen` — job status + retry
- EAS Build, TestFlight / internal track

**Milestone:** Share audio from Voice Memos → notes appear in Drive.

## Why this order

- Phase 1 is independently testable with no user-facing risk
- Phase 2 is the riskiest change (replaces existing web UX) — isolated so it can be validated before adding mobile complexity
- Phase 3 is additive — just a new client for an already-working pipeline
