import { useAuth } from '@clerk/clerk-react'
import { useState } from 'react'

type SetupStatus = 'idle' | 'loading' | 'success' | 'error'

interface SetupResult {
  folderId: string
  folderUrl: string
}

export default function DriveSetup() {
  const { getToken } = useAuth()
  const [status, setStatus] = useState<SetupStatus>('idle')
  const [result, setResult] = useState<SetupResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  const apiUrl = import.meta.env.VITE_API_URL

  async function handleSetup() {
    setStatus('loading')
    setError(null)
    try {
      const token = await getToken()
      const resp = await fetch(`${apiUrl}/setup`, {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/json',
        },
      })
      if (!resp.ok) {
        const body = await resp.text()
        throw new Error(body || resp.statusText)
      }
      const data: SetupResult = await resp.json()
      setResult(data)
      setStatus('success')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Setup failed')
      setStatus('error')
    }
  }

  if (status === 'success' && result) {
    return (
      <div className="setup-done" data-testid="drive-setup-success">
        <h2>Google Drive Connected</h2>
        <p>Your GradeBee folders are ready.</p>
        <a href={result.folderUrl} target="_blank" rel="noopener noreferrer" data-testid="drive-link">
          Open GradeBee folder in Drive
        </a>
      </div>
    )
  }

  return (
    <div className="setup" data-testid="drive-setup">
      <h2>Connect Google Drive</h2>
      <p>
        GradeBee stores notes and reports in your Google Drive.
        Click below to create the folder structure.
      </p>
      <button onClick={handleSetup} disabled={status === 'loading'} data-testid="setup-button">
        {status === 'loading' ? 'Setting up...' : 'Set Up Google Drive'}
      </button>
      {status === 'error' && <p className="error" data-testid="setup-error">{error}</p>}
    </div>
  )
}
