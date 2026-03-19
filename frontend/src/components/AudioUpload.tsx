import { useAuth } from '@clerk/react'
import { useRef, useState } from 'react'
import { uploadAudio, transcribeAudio } from '../api'

type UploadStatus = 'idle' | 'uploading' | 'transcribing' | 'done' | 'error'

const ACCEPTED_FORMATS = '.mp3,.mp4,.mpeg,.mpga,.m4a,.wav,.webm'
const MAX_SIZE_MB = 25
const MAX_SIZE_BYTES = MAX_SIZE_MB * 1024 * 1024

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
    <div className="audio-upload" data-testid="audio-upload">
      <h2>Upload Audio</h2>

      {(status === 'idle' || status === 'error') && (
        <div
          className={`drop-zone${dragOver ? ' drag-over' : ''}`}
          onDrop={handleDrop}
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onClick={() => fileInputRef.current?.click()}
          data-testid="drop-zone"
        >
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
      )}

      {status === 'error' && (
        <div className="upload-error" data-testid="upload-error">
          <p>{error}</p>
        </div>
      )}

      {status === 'uploading' && (
        <div className="upload-progress" data-testid="upload-progress">
          <p>Uploading <strong>{fileName}</strong>...</p>
        </div>
      )}

      {status === 'transcribing' && (
        <div className="upload-progress" data-testid="transcribe-progress">
          <p>Transcribing <strong>{fileName}</strong>... This may take a moment.</p>
        </div>
      )}

      {status === 'done' && (
        <div className="upload-done" data-testid="upload-done">
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
        </div>
      )}
    </div>
  )
}
