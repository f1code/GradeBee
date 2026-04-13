import { useAuth } from '@clerk/react'
import { useEffect, useRef, useState } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { uploadAudio, getGoogleToken, importFromDrive, submitTextNotes } from '../api'
import { useDrivePicker, AUDIO_MIME_TYPES } from '../hooks/useDrivePicker'
import { useMediaQuery } from '../hooks/useMediaQuery'

type UploadStatus = 'idle' | 'uploading' | 'error'

const ACCEPTED_FORMATS = '.mp3,.mp4,.mpeg,.mpga,.m4a,.wav,.webm'
const MAX_SIZE_MB = 25
const MAX_SIZE_BYTES = MAX_SIZE_MB * 1024 * 1024

/** How long to show the success toast before resetting to idle. */
const SUCCESS_TOAST_MS = 3000

async function runBatchUpload(
  items: { name: string; upload: () => Promise<unknown> }[],
  onProgress: (index: number, name: string) => void
): Promise<{ succeeded: number; failed: string[]; lastError: string | null }> {
  const failed: string[] = []
  let succeeded = 0
  let lastError: string | null = null
  for (let i = 0; i < items.length; i++) {
    const item = items[i]
    onProgress(i + 1, item.name)
    try {
      await item.upload()
      succeeded++
    } catch (err) {
      failed.push(item.name)
      lastError = err instanceof Error ? err.message : 'Something went wrong'
    }
  }
  return { succeeded, failed, lastError }
}

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

function PasteIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none">
      <rect x="5" y="3" width="14" height="18" rx="2" stroke="#E8A317" strokeWidth="1.5" fill="none" />
      <path d="M9 3V2a1 1 0 011-1h4a1 1 0 011 1v1" stroke="#E8A317" strokeWidth="1.5" strokeLinecap="round" />
      <line x1="9" y1="9" x2="15" y2="9" stroke="#E8A317" strokeWidth="1.5" strokeLinecap="round" />
      <line x1="9" y1="13" x2="15" y2="13" stroke="#E8A317" strokeWidth="1.5" strokeLinecap="round" />
      <line x1="9" y1="17" x2="12" y2="17" stroke="#E8A317" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

export default function AudioUpload({ onUploadDone }: { onUploadDone?: () => void }) {
  const { getToken } = useAuth()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [status, setStatus] = useState<UploadStatus>('idle')
  const [fileName, setFileName] = useState<string>('')
  const [error, setError] = useState<string>('')
  const [uploadIndex, setUploadIndex] = useState(0)
  const [uploadTotal, setUploadTotal] = useState(0)
  const [failedFiles, setFailedFiles] = useState<string[]>([])
  const [successCount, setSuccessCount] = useState(0)
  const [dragOver, setDragOver] = useState(false)
  const [showSuccess, setShowSuccess] = useState(false)
  const [showPaste, setShowPaste] = useState(false)
  const [pasteText, setPasteText] = useState('')
  const pasteRef = useRef<HTMLTextAreaElement>(null)
  const { openPicker } = useDrivePicker()
  const isMobile = useMediaQuery('(max-width: 640px)')

  useEffect(() => {
    if (showPaste) {
      requestAnimationFrame(() => {
        pasteRef.current?.focus()
        pasteRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
      })
    }
  }, [showPaste])

  function reset() {
    setStatus('idle')
    setFileName('')
    setError('')
    setShowSuccess(false)
    setFailedFiles([])
    setSuccessCount(0)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  function onUploadComplete(count: number) {
    setStatus('idle')
    setShowSuccess(true)
    setSuccessCount(count)
    setPasteText('')
    setShowPaste(false)
    if (fileInputRef.current) fileInputRef.current.value = ''
    onUploadDone?.()
    setTimeout(() => setShowSuccess(false), SUCCESS_TOAST_MS)
  }

  async function processFiles(files: File[]) {
    const oversized = files.filter(f => f.size > MAX_SIZE_BYTES)
    if (oversized.length > 0) {
      setError(
        oversized.length === 1
          ? `File too large (${(oversized[0].size / 1024 / 1024).toFixed(1)} MB). Max ${MAX_SIZE_MB} MB.`
          : `${oversized.length} files exceed the ${MAX_SIZE_MB} MB limit.`
      )
      setStatus('error')
      return
    }

    setError('')
    setShowSuccess(false)
    setFailedFiles([])
    setUploadTotal(files.length)

    setStatus('uploading')
    const { succeeded, failed, lastError } = await runBatchUpload(
      files.map(file => ({ name: file.name, upload: () => uploadAudio(file, getToken) })),
      (index, name) => { setUploadIndex(index); setFileName(name) }
    )

    if (failed.length > 0) {
      setFailedFiles(failed)
      setStatus('error')
      if (files.length === 1) {
        setError(lastError ?? 'Something went wrong')
      } else if (succeeded > 0) {
        setError(`${succeeded} file${succeeded > 1 ? 's' : ''} uploaded. ${failed.length} failed:`)
        onUploadDone?.()
      } else {
        setError(`All ${files.length} files failed to upload:`)
      }
    } else {
      onUploadComplete(succeeded)
    }
  }

  async function handleDriveImport() {
    setError('')
    setShowSuccess(false)

    try {
      const { accessToken } = await getGoogleToken(getToken)
      const picked = await openPicker(accessToken, { mimeTypes: AUDIO_MIME_TYPES, multiSelect: true, title: 'Select audio files' })
      if (!picked || picked.length === 0) return

      setUploadTotal(picked.length)
      setStatus('uploading')
      const { succeeded, failed } = await runBatchUpload(
        picked.map(item => ({ name: item.name, upload: () => importFromDrive(item.id, item.name, getToken) })),
        (index, name) => { setUploadIndex(index); setFileName(name) }
      )

      if (failed.length > 0) {
        setFailedFiles(failed)
        setStatus('error')
        if (succeeded > 0) {
          setError(`${succeeded} file${succeeded > 1 ? 's' : ''} uploaded. ${failed.length} failed:`)
          onUploadDone?.()
        } else {
          setError(`All ${picked.length} files failed to upload:`)
        }
      } else {
        onUploadComplete(succeeded)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
      setStatus('error')
    }
  }

  async function handlePasteSubmit() {
    if (!pasteText.trim()) return
    setError('')
    setShowSuccess(false)
    setFileName('pasted-text')

    try {
      setStatus('uploading')
      await submitTextNotes(pasteText, getToken)
      onUploadComplete(1)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
      setStatus('error')
    }
  }

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const files = Array.from(e.target.files ?? [])
    if (files.length > 0) processFiles(files)
  }

  function handleDrop(e: React.DragEvent) {
    e.preventDefault()
    setDragOver(false)
    const files = Array.from(e.dataTransfer.files ?? [])
    if (files.length > 0) processFiles(files)
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
      <h2>Add Notes</h2>

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
                  🎙️ Choose Audio Files
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
                <button
                  type="button"
                  className="mobile-upload-btn btn-secondary"
                  onClick={() => setShowPaste(!showPaste)}
                  data-testid="paste-text-btn"
                >
                  <PasteIcon />
                  Paste Text
                </button>
                <p className="hint">Accepted audio: mp3, mp4, m4a, wav, webm (max {MAX_SIZE_MB} MB each)</p>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept={ACCEPTED_FORMATS}
                  onChange={handleFileChange}
                  multiple
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
                  <p>Drag & drop audio files here, or click to browse</p>
                  <p className="hint">Accepted: mp3, mp4, m4a, wav, webm (max {MAX_SIZE_MB} MB each)</p>
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept={ACCEPTED_FORMATS}
                    onChange={handleFileChange}
                    multiple
                    style={{ display: 'none' }}
                    data-testid="file-input"
                  />
                </div>
                <div className="secondary-actions">
                  <button
                    type="button"
                    className="btn-secondary"
                    onClick={handleDriveImport}
                    data-testid="drive-import-btn"
                  >
                    <DriveIcon />
                    Add from Drive
                  </button>
                  <button
                    type="button"
                    className="btn-secondary"
                    onClick={() => setShowPaste(!showPaste)}
                    data-testid="paste-text-btn"
                  >
                    <PasteIcon />
                    Paste Text
                  </button>
                </div>
              </>
            )}

            {/* Paste text area */}
            <AnimatePresence>
              {showPaste && (
                <motion.div
                  className="paste-area"
                  initial={{ opacity: 0, height: 0 }}
                  animate={{ opacity: 1, height: 'auto' }}
                  exit={{ opacity: 0, height: 0 }}
                  transition={{ duration: 0.25 }}
                  data-testid="paste-area"
                >
                  <textarea
                    ref={pasteRef}
                    className="paste-textarea"
                    placeholder="Paste your notes here... Include student names and dates — we'll sort them out."
                    value={pasteText}
                    onChange={e => setPasteText(e.target.value)}
                    rows={6}
                    data-testid="paste-textarea"
                  />
                  <div className="paste-actions">
                    <p className="hint">Include student names and dates — we'll match them to your roster.</p>
                    <button
                      type="button"
                      onClick={handlePasteSubmit}
                      disabled={!pasteText.trim()}
                      data-testid="paste-submit-btn"
                    >
                      Process Notes
                    </button>
                  </div>
                </motion.div>
              )}
            </AnimatePresence>
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
            <p>{fileName === 'pasted-text'
              ? 'Processing notes...'
              : uploadTotal > 1
                ? <>Uploading <strong>{uploadIndex}/{uploadTotal}</strong>: {fileName}...</>
                : <>Uploading <strong>{fileName}</strong>...</>}</p>
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
            {fileName === 'pasted-text'
              ? 'Submitted'
              : successCount > 1
                ? `${successCount} files uploaded`
                : 'Uploaded'}! Processing in background.
          </motion.div>
        )}
      </AnimatePresence>

      {status === 'error' && (
        <div className="upload-error" data-testid="upload-error">
          <p>{error}</p>
          {failedFiles.length > 0 && (
            <ul className="upload-error-list">
              {failedFiles.map((f, i) => <li key={i}>{f}</li>)}
            </ul>
          )}
          <button className="btn-secondary" onClick={reset} style={{ marginTop: '0.5rem' }}>
            Try again
          </button>
        </div>
      )}
    </motion.div>
  )
}
