import { useAuth } from '@clerk/react'
import { useRef, useState } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { uploadAudio, getGoogleToken, importFromDrive } from '../api'
import { useDrivePicker, AUDIO_MIME_TYPES } from '../hooks/useDrivePicker'
import { useMediaQuery } from '../hooks/useMediaQuery'

type UploadStatus = 'idle' | 'uploading' | 'error'

const ACCEPTED_FORMATS = '.mp3,.mp4,.mpeg,.mpga,.m4a,.wav,.webm'
const MAX_SIZE_MB = 25
const MAX_SIZE_BYTES = MAX_SIZE_MB * 1024 * 1024

/** How long to show the success toast before resetting to idle. */
const SUCCESS_TOAST_MS = 3000

function MicIcon() {
  return (
    <svg className="drop-zone-icon" width="40" height="40" viewBox="0 0 40 40" fill="none">
      <rect x="14" y="6" width="12" height="20" rx="6" fill="#E8A317" opacity="0.25" />
      <rect x="15" y="7" width="10" height="18" rx="5" stroke="#E8A317" strokeWidth="1.5" fill="none" />
      <path d="M10 22C10 27.523 14.477 32 20 32C25.523 32 30 27.523 30 22" stroke="#E8A317" strokeWidth="1.5" strokeLinecap="round" fill="none" />
      <line x1="20" y1="32" x2="20" y2="36" stroke="#E8A317" strokeWidth="1.5" strokeLinecap="round" />
      <line x1="15" y1="36" x2="25" y2="36" stroke="#E8A317" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

function HoneycombSpinner() {
  return (
    <div className="honeycomb-spinner">
      <div className="hex" />
      <div className="hex" />
      <div className="hex" />
    </div>
  )
}

function DriveIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none">
      <path d="M8.01 2.56L1.38 14H7.37L14 2.56H8.01Z" fill="#E8A317" opacity="0.7" />
      <path d="M22.62 14H10.38L7.37 19.44H19.61L22.62 14Z" fill="#C4880F" />
      <path d="M14 2.56L22.62 14L19.61 19.44L11 7.56L14 2.56Z" fill="#E8A317" opacity="0.5" />
    </svg>
  )
}

export default function AudioUpload({ onUploadDone }: { onUploadDone?: () => void }) {
  const { getToken } = useAuth()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [status, setStatus] = useState<UploadStatus>('idle')
  const [fileName, setFileName] = useState<string>('')
  const [error, setError] = useState<string>('')
  const [dragOver, setDragOver] = useState(false)
  const [showSuccess, setShowSuccess] = useState(false)
  const { openPicker } = useDrivePicker()
  const isMobile = useMediaQuery('(max-width: 640px)')

  function reset() {
    setStatus('idle')
    setFileName('')
    setError('')
    setShowSuccess(false)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  function onUploadComplete() {
    setStatus('idle')
    setShowSuccess(true)
    if (fileInputRef.current) fileInputRef.current.value = ''
    onUploadDone?.()
    setTimeout(() => setShowSuccess(false), SUCCESS_TOAST_MS)
  }

  async function processFile(file: File) {
    if (file.size > MAX_SIZE_BYTES) {
      setError(`File too large (${(file.size / 1024 / 1024).toFixed(1)} MB). Max ${MAX_SIZE_MB} MB.`)
      setStatus('error')
      return
    }

    setFileName(file.name)
    setError('')
    setShowSuccess(false)

    try {
      setStatus('uploading')
      await uploadAudio(file, getToken)
      onUploadComplete()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
      setStatus('error')
    }
  }

  async function handleDriveImport() {
    setError('')
    setShowSuccess(false)

    try {
      const { accessToken } = await getGoogleToken(getToken)
      const picked = await openPicker(accessToken, { mimeTypes: AUDIO_MIME_TYPES })
      if (!picked) return

      setFileName(picked.name)
      setStatus('uploading')
      await importFromDrive(picked.id, picked.name, getToken)
      onUploadComplete()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
      setStatus('error')
    }
  }

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (file) processFile(file)
  }

  function handleDrop(e: React.DragEvent) {
    e.preventDefault()
    setDragOver(false)
    const file = e.dataTransfer.files?.[0]
    if (file) processFile(file)
  }

  function handleDragOver(e: React.DragEvent) {
    e.preventDefault()
    setDragOver(true)
  }

  function handleDragLeave() {
    setDragOver(false)
  }

  return (
    <motion.div
      className="audio-upload"
      data-testid="audio-upload"
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.35, delay: 0.15 }}
    >
      <h2>Upload Audio</h2>

      <AnimatePresence mode="wait">
        {(status === 'idle' || status === 'error') && (
          <motion.div
            key="dropzone"
            initial={{ opacity: 0, scale: 0.98 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.98 }}
            transition={{ duration: 0.25 }}
          >
            {isMobile ? (
              <div className="mobile-upload-actions" data-testid="mobile-upload">
                <button
                  type="button"
                  className="mobile-upload-btn"
                  onClick={() => fileInputRef.current?.click()}
                  data-testid="mobile-file-btn"
                >
                  🎙️ Choose Audio File
                </button>
                <button
                  type="button"
                  className="mobile-upload-btn btn-secondary"
                  onClick={handleDriveImport}
                  data-testid="drive-import-btn"
                >
                  <DriveIcon />
                  Add from Drive
                </button>
                <p className="hint">Accepted: mp3, mp4, m4a, wav, webm (max {MAX_SIZE_MB} MB)</p>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept={ACCEPTED_FORMATS}
                  onChange={handleFileChange}
                  style={{ display: 'none' }}
                  data-testid="file-input"
                />
              </div>
            ) : (
              <>
                <div
                  className={`drop-zone${dragOver ? ' drag-over' : ''}`}
                  onDrop={handleDrop}
                  onDragOver={handleDragOver}
                  onDragLeave={handleDragLeave}
                  onClick={() => fileInputRef.current?.click()}
                  data-testid="drop-zone"
                >
                  <MicIcon />
                  <p>Drag & drop an audio file here, or click to browse</p>
                  <p className="hint">Accepted: mp3, mp4, m4a, wav, webm (max {MAX_SIZE_MB} MB)</p>
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept={ACCEPTED_FORMATS}
                    onChange={handleFileChange}
                    style={{ display: 'none' }}
                    data-testid="file-input"
                  />
                </div>
                <div className="drive-import-row">
                  <button
                    type="button"
                    className="btn-secondary"
                    onClick={handleDriveImport}
                    data-testid="drive-import-btn"
                  >
                    <DriveIcon />
                    Add from Drive
                  </button>
                </div>
              </>
            )}
          </motion.div>
        )}

        {status === 'uploading' && (
          <motion.div
            key="uploading"
            className="upload-progress"
            data-testid="upload-progress"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.25 }}
          >
            <HoneycombSpinner />
            <p>Uploading <strong>{fileName}</strong>...</p>
          </motion.div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {showSuccess && (
          <motion.div
            className="upload-success-toast"
            data-testid="upload-success"
            initial={{ opacity: 0, y: -8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -8 }}
            transition={{ duration: 0.25 }}
          >
            <span className="upload-success-icon">✓</span>
            Uploaded! Processing in background.
          </motion.div>
        )}
      </AnimatePresence>

      {status === 'error' && (
        <div className="upload-error" data-testid="upload-error">
          <p>{error}</p>
          <button className="btn-secondary" onClick={reset} style={{ marginTop: '0.5rem' }}>
            Try again
          </button>
        </div>
      )}
    </motion.div>
  )
}
