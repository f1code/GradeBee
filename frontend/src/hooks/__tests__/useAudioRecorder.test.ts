import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useAudioRecorder, type RecorderError } from '../useAudioRecorder'

class FakeMediaRecorder {
  static isTypeSupported = vi.fn(() => true)
  state: 'inactive' | 'recording' = 'inactive'
  mimeType: string
  ondataavailable: ((e: { data: Blob }) => void) | null = null
  onstop: (() => void) | null = null
  options?: { mimeType?: string; audioBitsPerSecond?: number }
  constructor(_stream: MediaStream, options?: { mimeType?: string; audioBitsPerSecond?: number }) {
    this.mimeType = options?.mimeType ?? ''
    this.options = options
    FakeMediaRecorder.lastInstance = this
  }
  static lastInstance: FakeMediaRecorder | null = null
  start() {
    this.state = 'recording'
  }
  stop() {
    this.state = 'inactive'
    this.ondataavailable?.({ data: new Blob(['chunk'], { type: this.mimeType || 'audio/webm' }) })
    this.onstop?.()
  }
}

const stopTrack = vi.fn()
const fakeStream = { getTracks: () => [{ stop: stopTrack }] } as unknown as MediaStream
const getUserMedia = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  stopTrack.mockClear()
  vi.stubGlobal('MediaRecorder', FakeMediaRecorder as unknown as typeof MediaRecorder)
  Object.defineProperty(navigator, 'mediaDevices', {
    value: { getUserMedia },
    configurable: true,
  })
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  getUserMedia.mockResolvedValue(fakeStream)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useAudioRecorder', () => {
  it('reports support when getUserMedia, MediaRecorder, and secure context are present', async () => {
    const { result } = renderHook(() => useAudioRecorder())
    expect(result.current.isSupported).toBe(true)
  })

  it('start() surfaces a permission-denied error and does not start recording', async () => {
    const err = new Error('denied')
    err.name = 'NotAllowedError'
    getUserMedia.mockRejectedValue(err)

    const { result } = renderHook(() => useAudioRecorder())

    const holder: { returned: RecorderError | null } = { returned: null }
    await act(async () => {
      holder.returned = await result.current.start()
    })

    expect(holder.returned?.reason).toBe('denied')
    expect(result.current.error?.reason).toBe('denied')
    expect(result.current.isRecording).toBe(false)
  })

  it('start() then stop() produces a File and stops tracks', async () => {
    const { result } = renderHook(() => useAudioRecorder())

    await act(async () => {
      await result.current.start()
    })
    expect(result.current.isRecording).toBe(true)

    let file: File | null = null
    await act(async () => {
      file = await result.current.stop()
    })

    expect(file).not.toBeNull()
    expect(file!.name).toMatch(/^recording-.*\.(webm|m4a)$/)
    expect(stopTrack).toHaveBeenCalled()
    await waitFor(() => expect(result.current.isRecording).toBe(false))
  })

  it('cancel() discards the recording and stops tracks', async () => {
    const { result } = renderHook(() => useAudioRecorder())

    await act(async () => {
      await result.current.start()
    })

    act(() => {
      result.current.cancel()
    })

    expect(stopTrack).toHaveBeenCalled()
    expect(result.current.isRecording).toBe(false)
  })

  it('records mono at a capped bitrate so sessions stay under the upload limit', async () => {
    const { result } = renderHook(() => useAudioRecorder())

    await act(async () => {
      await result.current.start()
    })

    // Browser defaults are ~128 kbps (~0.9 MB/min), which pushes a 30-minute
    // session past the 25 MB upload limit.
    expect(getUserMedia).toHaveBeenCalledWith({ audio: { channelCount: 1 } })
    expect(FakeMediaRecorder.lastInstance?.options?.audioBitsPerSecond).toBe(32000)
  })

  it('cancelling while the permission prompt is still pending stops the stream once it resolves', async () => {
    let resolveGetUserMedia: (stream: MediaStream) => void = () => {}
    getUserMedia.mockReturnValue(new Promise<MediaStream>(resolve => { resolveGetUserMedia = resolve }))
    const { result } = renderHook(() => useAudioRecorder())

    let startPromise: Promise<RecorderError | null>
    act(() => {
      startPromise = result.current.start()
    })

    act(() => {
      result.current.cancel()
    })
    expect(result.current.isRecording).toBe(false)

    await act(async () => {
      resolveGetUserMedia(fakeStream)
      await startPromise
    })

    expect(stopTrack).toHaveBeenCalled()
    expect(result.current.isRecording).toBe(false)
  })

  it('stopping while the permission prompt is still pending stops the stream once it resolves', async () => {
    let resolveGetUserMedia: (stream: MediaStream) => void = () => {}
    getUserMedia.mockReturnValue(new Promise<MediaStream>(resolve => { resolveGetUserMedia = resolve }))
    const { result } = renderHook(() => useAudioRecorder())

    let startPromise: Promise<RecorderError | null>
    act(() => {
      startPromise = result.current.start()
    })

    let stopPromise: Promise<File | null>
    act(() => {
      stopPromise = result.current.stop()
    })
    expect(result.current.isRecording).toBe(false)

    await act(async () => {
      resolveGetUserMedia(fakeStream)
      await startPromise
      expect(await stopPromise).toBeNull()
    })

    expect(stopTrack).toHaveBeenCalled()
    expect(result.current.isRecording).toBe(false)
  })

  it('unmount stops tracks (no dangling mic access)', async () => {
    const { result, unmount } = renderHook(() => useAudioRecorder())

    await act(async () => {
      await result.current.start()
    })

    unmount()

    expect(stopTrack).toHaveBeenCalled()
  })
})
