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
  return true
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
  const startGenerationRef = useRef(0)

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
    startGenerationRef.current += 1
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
    startGenerationRef.current += 1
    const generation = startGenerationRef.current

    try {
      // Mono: a second channel doubles the source data and adds nothing for
      // speech. Bare values are "ideal", so a mono-incapable device still works.
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1 } })

      if (generation !== startGenerationRef.current) {
        // Cancelled or unmounted while the permission prompt was pending — discard the stream.
        stream.getTracks().forEach(track => track.stop())
        return null
      }

      streamRef.current = stream

      const mimeType = pickMimeType()
      mimeTypeRef.current = mimeType
      // Chromium's MediaRecorder defaults to 128 kbps regardless of channel
      // count (measured: ~0.9 MB/min), which is far more than transcription
      // needs and puts a 30-minute session past the 25 MB upload limit. Opus
      // stays intelligible for speech at 32 kbps; AAC (Safari's audio/mp4) is
      // less efficient, so it gets more headroom.
      const audioBitsPerSecond = mimeType.startsWith('audio/mp4') ? 48000 : 32000
      const recorder = new MediaRecorder(
        stream,
        mimeType ? { mimeType, audioBitsPerSecond } : { audioBitsPerSecond },
      )
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
      if (generation !== startGenerationRef.current) return null
      stopTracks()
      const recorderError = classifyError(err)
      setError(recorderError)
      return recorderError
    }
  }, [stopTracks])

  const stop = useCallback((): Promise<File | null> => {
    const recorder = mediaRecorderRef.current
    if (!recorder) {
      // No recorder yet — start() is still awaiting getUserMedia. Tear down now
      // so the pending stream is discarded as soon as it resolves (matches cancel()).
      teardown()
      return Promise.resolve(null)
    }

    const finalize = (): File => {
      const mimeType = mimeTypeRef.current || recorder.mimeType || 'audio/webm'
      const blob = new Blob(chunksRef.current, { type: mimeType })
      const ext = extensionForMimeType(mimeType)
      const file = new File([blob], `recording-${new Date().toISOString().replace(/[:.]/g, '-')}.${ext}`, {
        type: mimeType,
      })
      teardown()
      return file
    }

    if (recorder.state === 'inactive') {
      return Promise.resolve(finalize())
    }

    // MediaRecorder's stop is inherently event-callback-driven (onstop fires
    // asynchronously); Promise.withResolvers is ES2024 and unsupported on
    // iOS Safari < 17.4, a primary target for this feature, so the executor
    // form is required here rather than the stored-resolver pattern.
    return new Promise(resolve => {
      recorder.onstop = () => resolve(finalize())
      recorder.stop()
    })
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
