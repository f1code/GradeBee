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
