const apiUrl = import.meta.env.VITE_API_URL

// --- Class & Student Types ---

export interface ClassItem {
  id: number
  name: string
  studentCount: number
}

export interface StudentItem {
  id: number
  name: string
  classId: number
}

// --- Class CRUD ---

export async function listClasses(
  getToken: () => Promise<string | null>
): Promise<{ classes: ClassItem[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/classes`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Failed to list classes')
  return body
}

export async function createClass(
  name: string,
  getToken: () => Promise<string | null>
): Promise<ClassItem> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/classes`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ name }),
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Failed to create class')
  return body
}

export async function renameClass(
  id: number,
  name: string,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/classes/${id}`, {
    method: 'PUT',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ name }),
  })
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({}))
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
    const body = await resp.json().catch(() => ({}))
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
  const body = await resp.json()
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
  const body = await resp.json()
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
    const body = await resp.json().catch(() => ({}))
    throw new Error(body.error || 'Failed to rename student')
  }
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
    const body = await resp.json().catch(() => ({}))
    throw new Error(body.error || 'Failed to delete student')
  }
}

// --- Notes ---

export interface Note {
  id: number
  studentId: number
  date: string        // YYYY-MM-DD
  summary: string
  transcript: string | null
  source: 'auto' | 'manual'
  createdAt: string
  updatedAt: string
}

export async function listNotes(
  studentId: number,
  getToken: () => Promise<string | null>
): Promise<{ notes: Note[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/students/${studentId}/notes`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await resp.json()
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
  const body = await resp.json()
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
  const body = await resp.json()
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
  const body = await resp.json()
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
    const body = await resp.json().catch(() => ({}))
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

  const resp = await fetch(`${apiUrl}/upload`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: form,
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Upload failed')
  return body
}

// --- Report Examples ---

export interface ReportExampleItem {
  id: number
  name: string
  content: string
}

export async function listReportExamples(
  getToken: () => Promise<string | null>
): Promise<{ examples: ReportExampleItem[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/report-examples`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Failed to list examples')
  return body
}

export async function uploadReportExample(
  data: { name: string; content: string } | File,
  getToken: () => Promise<string | null>
): Promise<ReportExampleItem> {
  const token = await getToken()
  let resp: Response
  if (data instanceof File) {
    const form = new FormData()
    form.append('file', data)
    resp = await fetch(`${apiUrl}/report-examples`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: form,
    })
  } else {
    resp = await fetch(`${apiUrl}/report-examples`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(data),
    })
  }
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Failed to upload example')
  return body
}

export async function deleteReportExample(
  id: number,
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/report-examples`, {
    method: 'DELETE',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ id }),
  })
  if (!resp.ok) {
    const body = await resp.json()
    throw new Error(body.error || 'Failed to delete example')
  }
}

export async function updateReportExample(
  id: number,
  name: string,
  content: string,
  getToken: () => Promise<string | null>
): Promise<ReportExampleItem> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/report-examples/${id}`, {
    method: 'PUT',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ name, content }),
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Failed to update example')
  return body
}

// --- Reports ---

export interface ReportResult {
  id: number
  student: string
  class: string
  studentId: number
  html: string
  startDate: string
  endDate: string
  instructions?: string
  createdAt: string
}

export interface ReportSummary {
  id: number
  startDate: string
  endDate: string
  createdAt: string
}

export interface GenerateReportsResponse {
  reports: ReportResult[]
  error: string | null
}

export async function generateReports(
  req: {
    students: { studentId: number; name: string; class: string }[]
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
  const body = await resp.json()
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
  const body = await resp.json()
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
  const body = await resp.json()
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
  const body = await resp.json()
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
    const body = await resp.json().catch(() => ({}))
    throw new Error(body.error || 'Failed to delete report')
  }
}

// --- Google Token / Drive Import ---

export async function getGoogleToken(
  getToken: () => Promise<string | null>
): Promise<{ accessToken: string }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/google-token`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Failed to get Google token')
  return body
}

export async function importExampleFromDrive(
  fileId: string,
  fileName: string,
  getToken: () => Promise<string | null>
): Promise<ReportExampleItem> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/drive-import-example`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ fileId, fileName }),
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Drive import example failed')
  return body
}

export async function importFromDrive(
  fileId: string,
  fileName: string,
  getToken: () => Promise<string | null>
): Promise<{ uploadId: number; fileName: string }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/drive-import`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ fileId, fileName }),
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Drive import failed')
  return body
}

// --- Async Jobs ---

export interface UploadJob {
  uploadId: number
  fileName: string
  status: 'queued' | 'transcribing' | 'extracting' | 'creating_notes' | 'done' | 'failed'
  error?: string
  noteLinks?: { name: string; noteId: number; studentId: number; className: string }[]
  createdAt: string
}

export interface JobListResponse {
  active: UploadJob[]
  failed: UploadJob[]
  done: UploadJob[]
}

export async function fetchJobs(
  getToken: () => Promise<string | null>
): Promise<JobListResponse> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/jobs`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Failed to fetch jobs')
  return body
}

export async function retryFailedJobs(
  getToken: () => Promise<string | null>
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/jobs/retry`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) {
    const body = await resp.json()
    throw new Error(body.error || 'Failed to retry jobs')
  }
}

export async function dismissJobs(
  getToken: () => Promise<string | null>,
  uploadIds: number[]
): Promise<void> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/jobs/dismiss`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ uploadIds }),
  })
  if (!resp.ok) {
    const body = await resp.json()
    throw new Error(body.error || 'Failed to dismiss jobs')
  }
}
