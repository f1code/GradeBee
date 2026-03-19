import { useAuth } from '@clerk/react'
import { useRef, useState } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { uploadAudio, transcribeAudio } from '../api'

type UploadStatus = 'idle' | 'uploading' | 'transcribing' | 'done' | 'error'

const ACCEPTED_FORMATS = '.mp3,.mp4,.mpeg,.mpga,.m4a,.wav,.webm'
const MAX_SIZE_MB = 25
const MAX_SIZE_BYTES = MAX_SIZE_MB * 1024 * 1024

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

export default function AudioUpload() {
  const { getToken } = useAuth()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [status, setStatus] = useState<UploadStatus>('idle')
  const [fileName, setFileName] = useState<string>('')
  const [transcript, setTranscript] = useState<string>('')
  const [error, setError] = useState<string>('')
  const [dragOver, setDragOver] = useState(false)

  async function processFile(file: File) {
    if (file.size > MAX_SIZE_BYTES) {
      setError(`File too large (${(file.size / 1024 / 1024).toFixed(1)} MB). Max ${MAX_SIZE_MB} MB.`)
      setStatus('error')
      return
    }

    setFileName(file.name)
    setError('')
    setTranscript('')

    try {
      setStatus('uploading')
      const uploadResult = await uploadAudio(file, getToken)

      setStatus('transcribing')
      const transcribeResult = await transcribeAudio(uploadResult.fileId, getToken)

      setTranscript(transcribeResult.transcript)
      setStatus('done')
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

  function reset() {
    setStatus('idle')
    setFileName('')
    setTranscript('')
    setError('')
    if (fileInputRef.current) fileInputRef.current.value = ''
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

        {status === 'transcribing' && (
          <motion.div
            key="transcribing"
            className="upload-progress"
            data-testid="transcribe-progress"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.25 }}
          >
            <HoneycombSpinner />
            <p>Transcribing <strong>{fileName}</strong>... This may take a moment.</p>
          </motion.div>
        )}

        {status === 'done' && (
          <motion.div
            key="done"
            className="upload-done"
            data-testid="upload-done"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.3 }}
          >
            <p>✅ Transcription complete for <strong>{fileName}</strong></p>
            <textarea
              className="transcript-text"
              readOnly
              value={transcript}
              rows={10}
              data-testid="transcript-text"
            />
            <button onClick={reset} data-testid="upload-another">
              Upload another
            </button>
          </motion.div>
        )}
      </AnimatePresence>

      {status === 'error' && (
        <div className="upload-error" data-testid="upload-error">
          <p>{error}</p>
        </div>
      )}
    </motion.div>
  )
}
