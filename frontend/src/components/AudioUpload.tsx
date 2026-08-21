import { useAuth } from '@clerk/react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { motion, AnimatePresence } from 'motion/react'
import { uploadAudio, getGoogleToken, importFromDrive, submitTextNotes } from '../api'
import { useDrivePicker, AUDIO_MIME_TYPES } from '../hooks/useDrivePicker'
import { useHasLinkedGoogleAccount } from '../hooks/useHasLinkedGoogleAccount'
import { useMediaQuery } from '../hooks/useMediaQuery'
import { useAudioRecorder } from '../hooks/useAudioRecorder'

type UploadStatus = 'idle' | 'uploading' | 'error' | 'recording' | 'recorded'

const ACCEPTED_FORMATS = '.mp3,.mp4,.mpeg,.mpga,.m4a,.wav,.webm'
const MAX_SIZE_MB = 25
const MAX_SIZE_BYTES = MAX_SIZE_MB * 1024 * 1024

/** How long to show the success toast before resetting to idle. */
const SUCCESS_TOAST_MS = 3000

async function runBatchUpload(
  items: { name: string; upload: () => Promise<unknown> }[],
  onProgress: (index: number, name: string) => void,
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
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M8.01 2.56L1.38 14H7.37L14 2.56H8.01Z" fill="#E8A317" opacity="0.7" />
      <path d="M22.62 14H10.38L7.37 19.44H19.61L22.62 14Z" fill="#C4880F" />
      <path d="M14 2.56L22.62 14L19.61 19.44L11 7.56L14 2.56Z" fill="#E8A317" opacity="0.5" />
    </svg>
  )
}

function PasteIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect
        x="5"
        y="3"
        width="14"
        height="18"
        rx="2"
        stroke="#E8A317"
        strokeWidth="1.5"
        fill="none"
      />
      <path
        d="M9 3V2a1 1 0 011-1h4a1 1 0 011 1v1"
        stroke="#E8A317"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      <line x1="9" y1="9" x2="15" y2="9" stroke="#E8A317" strokeWidth="1.5" strokeLinecap="round" />
      <line
        x1="9"
        y1="13"
        x2="15"
        y2="13"
        stroke="#E8A317"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      <line
        x1="9"
        y1="17"
        x2="12"
        y2="17"
        stroke="#E8A317"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  )
}

function RecordIcon({ size = 18 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="7" fill="#D64545" />
    </svg>
  )
}

function formatElapsed(seconds: number): string {
  const mm = Math.floor(seconds / 60)
    .toString()
    .padStart(2, '0')
  const ss = (seconds % 60).toString().padStart(2, '0')
  return `${mm}:${ss}`
}

function isFileDrag(e: DragEvent): boolean {
  return Array.from(e.dataTransfer?.types ?? []).includes('Files')
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
  const pasteBtnRef = useRef<HTMLButtonElement>(null)
  const restorePasteFocusRef = useRef(false)
  const { openPicker } = useDrivePicker()
  const hasLinkedGoogleAccount = useHasLinkedGoogleAccount()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const recorder = useAudioRecorder()
  const [recordedFile, setRecordedFile] = useState<File | null>(null)
  const dragDepthRef = useRef(0)
  const leaveFrameRef = useRef<number | null>(null)

  const acceptDrop = (status === 'idle' || status === 'error') && !showPaste

  useEffect(() => {
    if (!showPaste) return
    requestAnimationFrame(() => {
      pasteRef.current?.focus()
    })
  }, [showPaste])

  function closePasteModal() {
    restorePasteFocusRef.current = true
    setShowPaste(false)
  }

  useEffect(() => {
    if (showPaste || !restorePasteFocusRef.current) return
    restorePasteFocusRef.current = false
    pasteBtnRef.current?.focus()
  }, [showPaste])

  useEffect(() => {
    if (!showPaste) return
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') closePasteModal()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [showPaste])

  function reset() {
    setStatus('idle')
    setFileName('')
    setError('')
    setShowSuccess(false)
    setFailedFiles([])
    setSuccessCount(0)
    setRecordedFile(null)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  const onUploadComplete = useCallback(
    (count: number) => {
      setStatus('idle')
      setShowSuccess(true)
      setSuccessCount(count)
      setPasteText('')
      setShowPaste(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
      onUploadDone?.()
      setTimeout(() => setShowSuccess(false), SUCCESS_TOAST_MS)
    },
    [onUploadDone],
  )

  const processFiles = useCallback(
    async (files: File[]) => {
      const oversized = files.filter(f => f.size > MAX_SIZE_BYTES)
      if (oversized.length > 0) {
        setError(
          oversized.length === 1
            ? `File too large (${(oversized[0].size / 1024 / 1024).toFixed(1)} MB). Max ${MAX_SIZE_MB} MB.`
            : `${oversized.length} files exceed the ${MAX_SIZE_MB} MB limit.`,
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
        (index, name) => {
          setUploadIndex(index)
          setFileName(name)
        },
      )

      if (failed.length > 0) {
        setFailedFiles(failed)
        setStatus('error')
        if (files.length === 1) {
          setError(lastError ?? 'Something went wrong')
        } else if (succeeded > 0) {
          setError(
            `${succeeded} file${succeeded > 1 ? 's' : ''} uploaded. ${failed.length} failed:`,
          )
          onUploadDone?.()
        } else {
          setError(`All ${files.length} files failed to upload:`)
        }
      } else {
        onUploadComplete(succeeded)
      }
    },
    [getToken, onUploadComplete, onUploadDone],
  )

  useEffect(() => {
    function cancelLeaveHide() {
      if (leaveFrameRef.current !== null) {
        cancelAnimationFrame(leaveFrameRef.current)
        leaveFrameRef.current = null
      }
    }

    function onDragEnter(e: DragEvent) {
      if (!isFileDrag(e)) return
      e.preventDefault()
      cancelLeaveHide()
      dragDepthRef.current += 1
      if (acceptDrop) setDragOver(true)
    }

    function onDragOver(e: DragEvent) {
      if (!isFileDrag(e)) return
      e.preventDefault()
    }

    function onDragLeave(e: DragEvent) {
      if (!isFileDrag(e)) return
      dragDepthRef.current = Math.max(0, dragDepthRef.current - 1)
      if (dragDepthRef.current === 0) {
        leaveFrameRef.current = requestAnimationFrame(() => {
          leaveFrameRef.current = null
          if (dragDepthRef.current === 0) setDragOver(false)
        })
      }
    }

    function onDrop(e: DragEvent) {
      if (!isFileDrag(e)) return
      e.preventDefault()
      cancelLeaveHide()
      dragDepthRef.current = 0
      setDragOver(false)
      if (!acceptDrop) return
      const files = Array.from(e.dataTransfer?.files ?? [])
      if (files.length > 0) void processFiles(files)
    }

    window.addEventListener('dragenter', onDragEnter, true)
    window.addEventListener('dragover', onDragOver, true)
    window.addEventListener('dragleave', onDragLeave, true)
    window.addEventListener('drop', onDrop, true)
    return () => {
      cancelLeaveHide()
      window.removeEventListener('dragenter', onDragEnter, true)
      window.removeEventListener('dragover', onDragOver, true)
      window.removeEventListener('dragleave', onDragLeave, true)
      window.removeEventListener('drop', onDrop, true)
    }
  }, [acceptDrop, processFiles])

  async function handleDriveImport() {
    setError('')
    setShowSuccess(false)

    try {
      const { accessToken } = await getGoogleToken(getToken)
      const picked = await openPicker(accessToken, {
        mimeTypes: AUDIO_MIME_TYPES,
        multiSelect: true,
        title: 'Select audio files',
      })
      if (!picked || picked.length === 0) return

      setUploadTotal(picked.length)
      setStatus('uploading')
      const { succeeded, failed } = await runBatchUpload(
        picked.map(item => ({
          name: item.name,
          upload: () => importFromDrive(item.id, item.name, getToken),
        })),
        (index, name) => {
          setUploadIndex(index)
          setFileName(name)
        },
      )

      if (failed.length > 0) {
        setFailedFiles(failed)
        setStatus('error')
        if (succeeded > 0) {
          setError(
            `${succeeded} file${succeeded > 1 ? 's' : ''} uploaded. ${failed.length} failed:`,
          )
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
    setShowPaste(false)
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

  async function handleRecordStart() {
    setError('')
    setShowSuccess(false)
    setStatus('recording')
    const recorderError = await recorder.start()
    if (recorderError) {
      setError(recorderError.message)
      setStatus('error')
    }
  }

  async function handleRecordStop() {
    const file = await recorder.stop()
    if (file) {
      setRecordedFile(file)
      setStatus('recorded')
    } else {
      setStatus('idle')
    }
  }

  function handleRecordCancel() {
    recorder.cancel()
    setRecordedFile(null)
    setStatus('idle')
  }

  function handleRecordDiscard() {
    setRecordedFile(null)
    setStatus('idle')
  }

  function handleRecordUpload() {
    if (!recordedFile) return
    const file = recordedFile
    setRecordedFile(null)
    processFiles([file])
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

      <p className="hint">
        Include level and time slot, and use student names or aliases - we'll match them to your
        roster.
      </p>
      <p className="upload-pii-hint">
        Use first names or initials — avoid full names when possible.
      </p>
      <AnimatePresence mode="wait">
        {(status === 'idle' || status === 'error') && (
          <motion.div
            key="idle-actions"
            initial={{ opacity: 0, scale: 0.98 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.98 }}
            transition={{ duration: 0.25 }}
          >
            <div
              className={
                isMobile ? 'idle-note-actions idle-note-actions--mobile' : 'idle-note-actions'
              }
              data-testid={isMobile ? 'mobile-upload' : 'idle-note-actions'}
            >
              <div className="recording-card" data-testid="recording-card">
                {recorder.isSupported ? (
                  <>
                    <h3 className="recording-card-heading">Record observations live</h3>
                    <p className="recording-card-copy">
                      You can review the recording before it's processed.
                    </p>
                    <button type="button" onClick={handleRecordStart} data-testid="record-btn">
                      <RecordIcon size={20} />
                      Start recording
                    </button>
                  </>
                ) : (
                  <>
                    <p className="recording-card-copy">
                      Live recording isn't available in this browser.
                    </p>
                    <button
                      type="button"
                      onClick={() => fileInputRef.current?.click()}
                      data-testid="upload-audio-btn"
                    >
                      Upload audio
                    </button>
                  </>
                )}
              </div>
              <p className="existing-notes-label">Or add existing notes</p>
              <div
                className={
                  isMobile ? 'secondary-actions secondary-actions--stack' : 'secondary-actions'
                }
                data-testid="secondary-actions"
              >
                {recorder.isSupported && (
                  <button
                    type="button"
                    className="btn-secondary"
                    onClick={() => fileInputRef.current?.click()}
                    data-testid="upload-audio-btn"
                  >
                    Upload audio
                  </button>
                )}
                {hasLinkedGoogleAccount && (
                  <button
                    type="button"
                    className="btn-secondary"
                    onClick={handleDriveImport}
                    data-testid="drive-import-btn"
                  >
                    <DriveIcon />
                    Select from Drive
                  </button>
                )}
                <button
                  type="button"
                  className="btn-secondary"
                  ref={pasteBtnRef}
                  onClick={() => setShowPaste(true)}
                  data-testid="paste-text-btn"
                >
                  <PasteIcon />
                  Enter text
                </button>
              </div>
              <input
                ref={fileInputRef}
                type="file"
                accept={isMobile ? 'audio/*' : ACCEPTED_FORMATS}
                onChange={handleFileChange}
                multiple
                style={{ display: 'none' }}
                data-testid="file-input"
              />
            </div>
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
            <p>
              {fileName === 'pasted-text' ? (
                'Processing notes...'
              ) : uploadTotal > 1 ? (
                <>
                  Uploading{' '}
                  <strong>
                    {uploadIndex}/{uploadTotal}
                  </strong>
                  : {fileName}...
                </>
              ) : (
                <>
                  Uploading <strong>{fileName}</strong>...
                </>
              )}
            </p>
          </motion.div>
        )}

        {status === 'recording' && (
          <motion.div
            key="recording"
            className="recording-card recording-panel"
            data-testid="recording-panel"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.25 }}
          >
            <p role="status" aria-live="polite" className="recording-status">
              <span className="recording-indicator" aria-hidden="true">
                ●
              </span>
              Recording
            </p>
            <p className="recording-time" data-testid="recording-time" aria-hidden="true">
              {formatElapsed(recorder.elapsedSeconds)}
            </p>
            <p className="recording-size" aria-hidden="true">
              {(recorder.recordedBytes / 1024 / 1024).toFixed(1)} MB
            </p>
            <div className="secondary-actions">
              <button type="button" onClick={handleRecordStop} data-testid="record-stop-btn">
                Stop
              </button>
              <button
                type="button"
                className="btn-secondary"
                onClick={handleRecordCancel}
                data-testid="record-cancel-btn"
              >
                Cancel
              </button>
            </div>
          </motion.div>
        )}

        {status === 'recorded' && recordedFile && (
          <motion.div
            key="recorded"
            className="recording-card recording-panel"
            data-testid="recorded-panel"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.25 }}
          >
            <p role="status" aria-live="polite" className="recording-review-name">
              {recordedFile.name} — {formatElapsed(recorder.elapsedSeconds)}
            </p>
            <div className="secondary-actions">
              <button type="button" onClick={handleRecordUpload} data-testid="record-upload-btn">
                Upload
              </button>
              <button
                type="button"
                className="btn-secondary"
                onClick={handleRecordDiscard}
                data-testid="record-discard-btn"
              >
                Discard
              </button>
            </div>
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
                : 'Uploaded'}
            ! Processing in background.
          </motion.div>
        )}
      </AnimatePresence>

      {status === 'error' && (
        <div className="upload-error" data-testid="upload-error" role="alert">
          <p>{error}</p>
          {failedFiles.length > 0 && (
            <ul className="upload-error-list">
              {failedFiles.map((f, i) => (
                <li key={i}>{f}</li>
              ))}
            </ul>
          )}
          <button className="btn-secondary" onClick={reset} style={{ marginTop: '0.5rem' }}>
            Try again
          </button>
        </div>
      )}

      <AnimatePresence>
        {showPaste && (
          <motion.div
            className="how-it-works-overlay"
            data-testid="paste-text-overlay"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onDragOver={e => {
              e.preventDefault()
              e.stopPropagation()
            }}
            onDrop={e => {
              e.preventDefault()
              e.stopPropagation()
            }}
          >
            <motion.div
              className="how-it-works-card card"
              role="dialog"
              aria-modal="true"
              aria-labelledby="paste-text-modal-heading"
              initial={{ opacity: 0, y: 30, scale: 0.97 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 20 }}
              transition={{ duration: 0.3, ease: 'easeOut' }}
              onClick={e => e.stopPropagation()}
            >
              <button
                type="button"
                className="how-it-works-close paste-text-modal-close"
                onClick={closePasteModal}
                aria-label="Close"
              >
                ×
              </button>
              <h2 id="paste-text-modal-heading">Enter text</h2>
              <textarea
                ref={pasteRef}
                className="paste-textarea"
                placeholder="Type or paste your observations here..."
                value={pasteText}
                onChange={e => setPasteText(e.target.value)}
                rows={6}
                data-testid="paste-textarea"
              />
              <div className="paste-actions">
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
          </motion.div>
        )}
      </AnimatePresence>

      {dragOver &&
        acceptDrop &&
        createPortal(
          <div
            className="notes-drop-overlay"
            data-testid="drop-overlay"
            role="status"
            aria-live="polite"
          >
            Drop audio to upload
          </div>,
          document.body,
        )}
    </motion.div>
  )
}
