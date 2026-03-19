const apiUrl = import.meta.env.VITE_API_URL

export interface MatchedStudent {
  name: string
  class: string
  summary: string
  confidence: number
  candidates?: { name: string; class: string }[]
}

export interface ExtractResult {
  students: MatchedStudent[]
  date: string
}

export interface CreateNotesRequest {
  fileId: string
  students: { name: string; class: string; summary: string }[]
  transcript: string
  date: string
}

export interface NoteResult {
  student: string
  class: string
  docId: string
  docUrl: string
}

export async function uploadAudio(
  file: File,
  getToken: () => Promise<string | null>
): Promise<{ fileId: string; fileName: string }> {
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

export async function transcribeAudio(
  fileId: string,
  getToken: () => Promise<string | null>
): Promise<{ fileId: string; transcript: string }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/transcribe`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ fileId }),
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Transcription failed')
  return body
}

export async function extractFromTranscript(
  transcript: string,
  fileId: string,
  getToken: () => Promise<string | null>
): Promise<ExtractResult> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/extract`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ transcript, fileId }),
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Extraction failed')
  return body
}

export async function createNotes(
  req: CreateNotesRequest,
  getToken: () => Promise<string | null>
): Promise<{ notes: NoteResult[] }> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/notes`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(req),
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Note creation failed')
  return body
}

// --- Report Examples ---

export interface ReportExampleItem {
  id: string
  name: string
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
  id: string,
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

// --- Reports ---

export interface ReportResult {
  student: string
  class: string
  docId: string
  docUrl: string
  skipped: boolean
}

export interface GenerateReportsResponse {
  reports: ReportResult[]
  error: string | null
}

export async function generateReports(
  req: {
    students: { name: string; class: string }[]
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
  req: {
    docId: string
    student: string
    class: string
    startDate: string
    endDate: string
    instructions?: string
  },
  getToken: () => Promise<string | null>
): Promise<ReportResult> {
  const token = await getToken()
  const resp = await fetch(`${apiUrl}/reports/regenerate`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(req),
  })
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Report regeneration failed')
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
  const body = await resp.json()
  if (!resp.ok) throw new Error(body.error || 'Failed to get Google token')
  return body
}

export async function importFromDrive(
  fileId: string,
  fileName: string,
  getToken: () => Promise<string | null>
): Promise<{ fileId: string; fileName: string }> {
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
