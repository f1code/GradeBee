import { useAuth } from '@clerk/react'
import { useState } from 'react'
import { motion } from 'motion/react'

type SetupStatus = 'idle' | 'loading' | 'success' | 'error'

interface SetupResult {
  folderId: string
  folderUrl: string
  spreadsheetId: string
  spreadsheetUrl: string
}

interface DriveSetupProps {
  onComplete?: (result: SetupResult) => void
}

export default function DriveSetup({ onComplete }: DriveSetupProps) {
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
      if (!token) throw new Error('Not authenticated')
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

  function handleContinue() {
    if (result) {
      onComplete?.(result)
    }
  }

  if (status === 'success' && result) {
    return (
      <motion.div
        className="setup-done"
        data-testid="drive-setup-success"
        initial={{ opacity: 0, y: 16 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.4, ease: 'easeOut' }}
      >
        <h2>Google Drive Connected</h2>
        <p>Your GradeBee folders are ready.</p>
        <div className="setup-done-links">
          <a
            href={result.folderUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="drive-link-card"
            data-testid="drive-link"
          >
            <span className="link-icon">📁</span>
            <span className="link-label">Open GradeBee folder in Drive</span>
          </a>
          <a
            href={result.spreadsheetUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="drive-link-card"
            data-testid="spreadsheet-link"
          >
            <span className="link-icon">📊</span>
            <span className="link-label">Open ClassSetup spreadsheet</span>
          </a>
        </div>
        <p className="setup-instruction">
          Add your students to the ClassSetup spreadsheet, then continue.
        </p>
        <button onClick={handleContinue} data-testid="setup-continue-btn">
          Continue
        </button>
      </motion.div>
    )
  }

  return (
    <motion.div
      className="setup"
      data-testid="drive-setup"
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, ease: 'easeOut' }}
    >
      <h2>Connect Google Drive</h2>
      <p>
        GradeBee stores notes and reports in your Google Drive.
        Click below to create the folder structure.
      </p>
      <button onClick={handleSetup} disabled={status === 'loading'} data-testid="setup-button">
        {status === 'loading' ? 'Setting up...' : 'Set Up Google Drive'}
      </button>
      {status === 'error' && <p className="error" data-testid="setup-error">{error}</p>}
    </motion.div>
  )
}
