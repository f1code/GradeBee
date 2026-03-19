const apiUrl = import.meta.env.VITE_API_URL

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
