import * as Sentry from '@sentry/react'

import type {
  ClassWithCount as ClassItem,
  Student as StudentItem,
  Note,
  ReportResult,
  ReportSummary,
  GenerateReportsHTTPResponse as GenerateReportsResponse,
  VoiceNoteJob,
  JobListResponse,
  AliasResponse,
  Level as LevelItem,
} from './api-types.gen'

export type {
  ClassItem,
  StudentItem,
  Note,
  ReportResult,
  ReportSummary,
  GenerateReportsResponse,
  VoiceNoteJob,
  JobListResponse,
  AliasResponse,
  LevelItem,
}

/**
 * @deprecated Use VoiceNoteJob instead
 */
export type UploadJob = VoiceNoteJob

/**
 * Thrown by addAlias when the server returns a 409 alias conflict.
 * Contains the canonical name of the student who already owns the alias.
 */
export class AliasConflictError extends Error {
  conflictStudentName: string
  constructor(message: string, conflictStudentName: string) {
    super(message)
    this.name = 'AliasConflictError'
    this.conflictStudentName = conflictStudentName
  }
}

/**
 * Thrown by moveStudent when the server returns a 409 name conflict.
 * Contains the canonical name of the student already occupying that name
 * in the target class.
 */
export class MoveConflictError extends Error {
  conflictStudentName: string
  constructor(message: string, conflictStudentName: string) {
    super(message)
    this.name = 'MoveConflictError'
    this.conflictStudentName = conflictStudentName
  }
}

const apiUrl = import.meta.env.VITE_API_URL
// The seven weekday names a Class's Day may take, Monday first — mirrors
// the backend's CHECK constraint (classes.day, sql/014_require_day.sql) and
// validDays in repo_class.go. Full names are shown in the selector; class
// display names abbreviate to the first three letters (done server-side).
export const WEEKDAYS = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'] as const

/**
 * Human-readable message for an HTTP failure whose body carried no JSON
 * `error` field. Reached when the response never came from our handlers:
 * the reverse proxy answers 413 / 5xx with an HTML page, and the auth
 * middleware answers 401 with an empty body.
 */
function httpErrorMessage(status: number): string {
  if (status === 401 || status === 403) return 'Your session expired — please sign in again.'
  if (status === 413) return 'The file is too large to upload. Try a shorter recording.'
  if (status === 429) return 'Too many requests. Wait a moment and try again.'
  if (status >= 500) return `The server is unavailable (HTTP ${status}). Please try again.`
  return status ? `Request failed (HTTP ${status}).` : 'Request failed.'
}

/**
 * Report a failure our own handlers did not produce — a proxy-generated HTML
 * page (413, 502, 504) or an empty body.
 *
 * These never reach the Go backend, so its Sentry client cannot see them, and
 * every caller catches API errors to render them as UI state, so Sentry's
 * global handlers never see them either. Without this capture the failure is
 * invisible to diagnostics — which is how the production 413 went unreported.
 *
 * No-op when Sentry was never initialised (diagnostics consent declined).
 */
function reportOpaqueFailure(resp: Response): void {
  // Session expiry is routine and self-healing; not worth an issue.
  if (resp.status === 401 || resp.status === 403) return

  // Path only: no origin, no query string, nothing user-typed.
  let path: string
  try {
    path = new URL(resp.url ?? '', window.location.origin).pathname
  } catch {
    path = 'unknown'
  }

  Sentry.captureMessage(`API ${resp.status} with non-JSON body: ${path}`, {
    level: 'error',
    tags: { api_status: String(resp.status), api_path: path },
  })
}

/**
 * Read a response body as JSON without assuming it is JSON.
 *
 * A blind `resp.json()` turns any proxy-generated HTML error page into an
 * opaque `SyntaxError` ("unexpected character at line 1 column 1"), hiding the
 * status code from the user and from diagnostics. On a non-JSON failure this
 * returns a synthetic `{ error }` so existing `body.error || fallback` call
 * sites report the real HTTP condition.
 *
 * A non-JSON *success* body still throws: a 200 carrying HTML means the SPA
 * fallback (backend/handler.go's spaHandler) answered instead of an API
 * handler, and returning an empty object there would render as an empty
 * roster / empty job list rather than a failure. Every endpoint that reads the
 * body on success returns a JSON object; the only empty-bodied response is the
 * CORS preflight 204, which no call site parses.
 */
// resp.json() is typed `any`; preserving that keeps every call site's
// `return body` assignable to its declared response type.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function readBody(resp: Response): Promise<any> {
  const parsed = await resp.json().catch(() => null)
  if (parsed !== null && typeof parsed === 'object') return parsed

  reportOpaqueFailure(resp)
  if (resp.ok) throw new Error('The server returned an unreadable response. Please try again.')
  return { error: httpErrorMessage(resp.status) }
}


// --- Class CRUD ---

export async function listClasses(
  getToken: () => Promise<string | null>
): Promise<{ classes: ClassItem[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/classes`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to list classes')
  return body
}

export async function createClass(
  levelId: number,
  day: string,
  timeSlot: string,
  getToken: () => Promise<string | null>
): Promise<ClassItem> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/classes`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ levelId, day, timeSlot }),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to create class')
  return body
}

export async function renameClass(
  id: number,
  levelId: number,
  day: string,
  timeSlot: string,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/classes/${id}`, {
    method: 'PUT',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ levelId, day, timeSlot }),
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to rename class')
  }
}

export async function deleteClass(
  id: number,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/classes/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to delete class')
  }
}

// --- Student CRUD ---

export async function listStudents(
  classId: number,
  getToken: () => Promise<string | null>
): Promise<{ students: StudentItem[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/classes/${classId}/students`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to list students')
  return body
}

export async function createStudent(
  classId: number,
  name: string,
  getToken: () => Promise<string | null>
): Promise<StudentItem> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/classes/${classId}/students`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ name }),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to create student')
  return body
}

export async function renameStudent(
  id: number,
  name: string,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${id}`, {
    method: 'PUT',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ name }),
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to rename student')
  }
}

export async function moveStudent(
  studentId: number,
  classId: number,
  getToken: () => Promise<string | null>
): Promise<{ droppedAliases: string[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${studentId}`, {
    method: 'PUT',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ classId }),
  })
  const body = await readBody(resp)
  if (!resp.ok) {
    if (resp.status === 409) {
      const conflictStudentName: string = body.details?.conflictStudentName ?? ''
      throw new MoveConflictError(body.message || body.error || 'A student with that name already exists in the target class', conflictStudentName)
    }
    throw new Error(body.error || 'Failed to move student')
  }
  return { droppedAliases: body.droppedAliases ?? [] }
}

export async function deleteStudent(
  id: number,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to delete student')
  }
}

// --- Notes ---

export async function listNotes(
  studentId: number,
  getToken: () => Promise<string | null>
): Promise<{ notes: Note[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${studentId}/notes`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to list notes')
  return body
}

export async function getNote(
  noteId: number,
  getToken: () => Promise<string | null>
): Promise<Note> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/notes/${noteId}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to get note')
  return body
}

export async function createNote(
  studentId: number,
  data: { date: string; summary: string },
  getToken: () => Promise<string | null>
): Promise<Note> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${studentId}/notes`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(data),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to create note')
  return body
}

export async function updateNote(
  noteId: number,
  data: { summary: string },
  getToken: () => Promise<string | null>
): Promise<Note> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/notes/${noteId}`, {
    method: 'PUT',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(data),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to update note')
  return body
}

export async function deleteNote(
  noteId: number,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/notes/${noteId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to delete note')
  }
}

// --- Audio Upload ---

export async function uploadAudio(
  file: File,
  getToken: () => Promise<string | null>
): Promise<{ uploadId: number; fileName: string }> {
  const token = await getToken()
  const form = new FormData()
  form.append('file', file)

  const resp = await fetch(`${apiUrl}/voice-notes/upload`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: form,
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Upload failed')
  return body
}

export async function submitTextNotes(
  text: string,
  getToken: () => Promise<string | null>
): Promise<{ uploadId: number; fileName: string }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/text-notes/upload`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ text }),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to submit text notes')
  return body
}

// --- Reports ---

export async function generateReports(
  req: {
    students: { studentId: number; name: string; className: string }[]
    startDate: string
    endDate: string
    instructions?: string
  },
  getToken: () => Promise<string | null>
): Promise<GenerateReportsResponse> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/reports`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(req),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Report generation failed')
  return body
}

export async function regenerateReport(
  reportId: number,
  feedback: string,
  getToken: () => Promise<string | null>
): Promise<ReportResult> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/reports/${reportId}/regenerate`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ feedback }),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Report regeneration failed')
  return body
}

export async function listStudentReports(
  studentId: number,
  getToken: () => Promise<string | null>
): Promise<{ reports: ReportSummary[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${studentId}/reports`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to list reports')
  return body
}

export async function getReport(
  id: number,
  getToken: () => Promise<string | null>
): Promise<ReportResult> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/reports/${id}`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to get report')
  return body
}

export async function deleteReport(
  id: number,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/reports/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to delete report')
  }
}

// --- Aliases ---

export async function listAliases(
  studentId: number,
  getToken: () => Promise<string | null>
): Promise<{ aliases: AliasResponse[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${studentId}/aliases`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to list aliases')
  return body
}

export async function addAlias(
  studentId: number,
  alias: string,
  getToken: () => Promise<string | null>
): Promise<AliasResponse> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${studentId}/aliases`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ alias }),
  })
  const body = await readBody(resp)
  if (!resp.ok) {
    if (resp.status === 409) {
      const conflictStudentName: string = body.details?.conflictStudentName ?? ''
      throw new AliasConflictError(body.message || body.error || 'Alias already in use', conflictStudentName)
    }
    throw new Error(body.error || 'Failed to add alias')
  }
  return body
}

export async function removeAlias(
  studentId: number,
  aliasId: number,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${studentId}/aliases/${aliasId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to remove alias')
  }
}

// --- Artifact Feedback ---

export async function submitFeedback(
  req: {
    artifact_type: 'report' | 'note'
    artifact_id: number
    rating: 'up' | 'down'
    comment?: string
  },
  getToken: () => Promise<string | null>
): Promise<{ id: number; created_at: string }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/feedback`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(req),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to submit feedback')
  return body
}


// --- Google Token / Drive Import ---

export async function getGoogleToken(
  getToken: () => Promise<string | null>
): Promise<{ accessToken: string }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/google-token`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to get Google token')
  return body
}

export async function importFromDrive(
  driveFileId: string,
  fileName: string,
  getToken: () => Promise<string | null>
): Promise<{ uploadId: number; fileName: string }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/voice-notes/drive-import`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ fileId: driveFileId, fileName }),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Drive import failed')
  return body
}

// --- Async Jobs ---

export async function fetchJobs(
  getToken: () => Promise<string | null>
): Promise<JobListResponse> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/voice-notes/jobs`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to fetch jobs')
  return body
}

export async function retryFailedJobs(
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/voice-notes/jobs/retry`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to retry jobs')
  }
}

export async function dismissJobs(
  getToken: () => Promise<string | null>,
  uploadIds: number[]
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/voice-notes/jobs/dismiss`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ uploadIds }),
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to dismiss jobs')
  }
}

// --- Levels (admin) ---

export async function listLevels(
  getToken: () => Promise<string | null>
): Promise<{ levels: LevelItem[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/levels`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to list levels')
  return body
}

export async function createLevel(
  name: string,
  getToken: () => Promise<string | null>
): Promise<LevelItem> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/levels`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ name }),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to create level')
  return body
}

export async function renameLevel(
  id: number,
  name: string,
  getToken: () => Promise<string | null>
): Promise<LevelItem> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/levels/${id}`, {
    method: 'PUT',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ name }),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to rename level')
  return body
}

export async function updateLevelReportInstructions(
  id: number,
  reportInstructions: string,
  getToken: () => Promise<string | null>
): Promise<LevelItem> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/levels/${id}`, {
    method: 'PUT',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ reportInstructions }),
  })
  const body = await readBody(resp)
  if (!resp.ok) throw new Error(body.error || 'Failed to update report instructions')
  return body
}

export async function deleteLevel(
  id: number,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/levels/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) {
    const body = await readBody(resp)
    throw new Error(body.error || 'Failed to delete level')
  }
}
