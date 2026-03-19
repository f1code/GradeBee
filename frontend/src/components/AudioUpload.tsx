import { useAuth } from '@clerk/react'
import { useRef, useState } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { uploadAudio, transcribeAudio, extractFromTranscript, createNotes } from '../api'
import type { ExtractResult, NoteResult } from '../api'
import NoteConfirmation from './NoteConfirmation'

type UploadStatus = 'idle' | 'uploading' | 'transcribing' | 'extracting' | 'confirming' | 'saving' | 'saved' | 'error'

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
  const [fileId, setFileId] = useState<string>('')
  const [extractResult, setExtractResult] = useState<ExtractResult | null>(null)
  const [savedNotes, setSavedNotes] = useState<NoteResult[] | null>(null)
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
    setExtractResult(null)
    setSavedNotes(null)

    try {
      setStatus('uploading')
      const uploadResult = await uploadAudio(file, getToken)
      setFileId(uploadResult.fileId)

      setStatus('transcribing')
      const transcribeResult = await transcribeAudio(uploadResult.fileId, getToken)
      setTranscript(transcribeResult.transcript)

      setStatus('extracting')
      const extraction = await extractFromTranscript(
        transcribeResult.transcript,
        uploadResult.fileId,
        getToken
      )
      setExtractResult(extraction)
      setStatus('confirming')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
      setStatus('error')
    }
  }

  async function handleSaveNotes(
    students: { name: string; class: string; summary: string }[],
    date: string
  ) {
    try {
      setStatus('saving')
      const result = await createNotes(
        { fileId, students, transcript, date },
        getToken
      )
      setSavedNotes(result.notes)
      setStatus('saved')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create notes')
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
    setFileId('')
    setExtractResult(null)
    setSavedNotes(null)
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

        {status === 'extracting' && (
          <motion.div
            key="extracting"
            className="upload-progress"
            data-testid="extract-progress"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.25 }}
          >
            <HoneycombSpinner />
            <p>Analyzing transcript...</p>
          </motion.div>
        )}

        {(status === 'confirming' || status === 'saving' || status === 'saved') && extractResult && (
          <motion.div
            key="confirming"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.3 }}
          >
            <NoteConfirmation
              extractResult={extractResult}
              transcript={transcript}
              fileId={fileId}
              onSave={handleSaveNotes}
              onCancel={reset}
              saving={status === 'saving'}
              savedNotes={savedNotes}
              onReset={reset}
            />
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
