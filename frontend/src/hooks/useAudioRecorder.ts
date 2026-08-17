import { useCallback, useEffect, useRef, useState } from 'react'

const MIME_TYPE_PREFERENCE = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', '']

function pickMimeType(): string {
  if (typeof window === 'undefined' || !window.MediaRecorder) return ''
  for (const type of MIME_TYPE_PREFERENCE) {
    if (type === '' || MediaRecorder.isTypeSupported(type)) return type
  }
  return ''
}

function extensionForMimeType(mimeType: string): string {
  if (mimeType.startsWith('audio/mp4')) return 'm4a'
  return 'webm'
}

export function isRecordingSupported(): boolean {
  if (typeof window === 'undefined') return false
  if (!window.isSecureContext) return false
  if (!navigator.mediaDevices?.getUserMedia) return false
  if (!window.MediaRecorder) return false
  return pickMimeType() !== '' || MIME_TYPE_PREFERENCE.includes('')
}

export type RecorderErrorReason = 'denied' | 'not-found' | 'in-use' | 'unknown'

export interface RecorderError {
  reason: RecorderErrorReason
  message: string
}

function classifyError(err: unknown): RecorderError {
  const name = err instanceof Error ? err.name : ''
  switch (name) {
    case 'NotAllowedError':
      return {
        reason: 'denied',
        message: 'Microphone access was blocked. Allow microphone access in your browser/site settings and try again.',
      }
    case 'NotFoundError':
      return { reason: 'not-found', message: 'No microphone was found on this device.' }
    case 'NotReadableError':
      return { reason: 'in-use', message: 'The microphone is in use by another app. Close it and try again.' }
    default:
      return { reason: 'unknown', message: err instanceof Error ? err.message : 'Could not access the microphone.' }
  }
}

export function useAudioRecorder() {
  const [isRecording, setIsRecording] = useState(false)
  const [elapsedSeconds, setElapsedSeconds] = useState(0)
  const [recordedBytes, setRecordedBytes] = useState(0)
  const [error, setError] = useState<RecorderError | null>(null)

  const mediaRecorderRef = useRef<MediaRecorder | null>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const chunksRef = useRef<Blob[]>([])
  const intervalRef = useRef<number | null>(null)
  const mimeTypeRef = useRef<string>('')

  const stopTracks = useCallback(() => {
    streamRef.current?.getTracks().forEach(track => track.stop())
    streamRef.current = null
  }, [])

  const clearTimer = useCallback(() => {
    if (intervalRef.current !== null) {
      clearInterval(intervalRef.current)
      intervalRef.current = null
    }
  }, [])

  const teardown = useCallback(() => {
    clearTimer()
    stopTracks()
    mediaRecorderRef.current = null
    chunksRef.current = []
    setIsRecording(false)
  }, [clearTimer, stopTracks])

  useEffect(() => teardown, [teardown])

  const start = useCallback(async (): Promise<RecorderError | null> => {
    setError(null)
    setElapsedSeconds(0)
    setRecordedBytes(0)
    chunksRef.current = []

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      streamRef.current = stream

      const mimeType = pickMimeType()
      mimeTypeRef.current = mimeType
      const recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream)
      recorder.ondataavailable = (e: BlobEvent) => {
        if (e.data.size > 0) {
          chunksRef.current.push(e.data)
          setRecordedBytes(prev => prev + e.data.size)
        }
      }
      mediaRecorderRef.current = recorder
      recorder.start()
      setIsRecording(true)

      intervalRef.current = setInterval(() => {
        setElapsedSeconds(prev => prev + 1)
      }, 1000)
      return null
    } catch (err) {
      stopTracks()
      const recorderError = classifyError(err)
      setError(recorderError)
      return recorderError
    }
  }, [stopTracks])

  const stop = useCallback((): Promise<File | null> => {
    const recorder = mediaRecorderRef.current
    if (!recorder) return Promise.resolve(null)

    const { promise, resolve } = Promise.withResolvers<File | null>()
    recorder.onstop = () => {
      const mimeType = mimeTypeRef.current || recorder.mimeType || 'audio/webm'
      const blob = new Blob(chunksRef.current, { type: mimeType })
      const ext = extensionForMimeType(mimeType)
      const file = new File([blob], `recording-${new Date().toISOString().replace(/[:.]/g, '-')}.${ext}`, {
        type: mimeType,
      })
      teardown()
      resolve(file)
    }
    recorder.stop()
    return promise
  }, [teardown])

  const cancel = useCallback(() => {
    const recorder = mediaRecorderRef.current
    if (recorder) {
      recorder.onstop = null
      if (recorder.state !== 'inactive') recorder.stop()
    }
    teardown()
  }, [teardown])

  return {
    isSupported: isRecordingSupported(),
    isRecording,
    elapsedSeconds,
    recordedBytes,
    error,
    start,
    stop,
    cancel,
  }
}
