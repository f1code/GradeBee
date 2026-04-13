import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockUploadAudio = vi.fn()
const mockGetGoogleToken = vi.fn()
const mockImportFromDrive = vi.fn()
const mockSubmitTextNotes = vi.fn()

vi.mock('../../api', () => ({
  uploadAudio: (...args: unknown[]) => mockUploadAudio(...args),
  getGoogleToken: (...args: unknown[]) => mockGetGoogleToken(...args),
  importFromDrive: (...args: unknown[]) => mockImportFromDrive(...args),
  submitTextNotes: (...args: unknown[]) => mockSubmitTextNotes(...args),
}))

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../hooks/useDrivePicker', () => ({
  useDrivePicker: () => ({ openPicker: vi.fn().mockResolvedValue(null) }),
  AUDIO_MIME_TYPES: 'audio/mpeg',
}))

vi.mock('../../hooks/useMediaQuery', () => ({
  useMediaQuery: () => false, // desktop by default
}))

describe('AudioUpload', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // jsdom doesn't implement scrollIntoView
    Element.prototype.scrollIntoView = vi.fn()
  })

  it('renders drop zone in idle state', async () => {
    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)
    expect(screen.getByTestId('drop-zone')).toBeInTheDocument()
    expect(screen.getByText('Add Notes')).toBeInTheDocument()
  })

  it('rejects files over 25MB', async () => {
    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    const bigFile = new File(['x'.repeat(100)], 'big.mp3', { type: 'audio/mpeg' })
    Object.defineProperty(bigFile, 'size', { value: 26 * 1024 * 1024 })

    const input = screen.getByTestId('file-input') as HTMLInputElement
    await userEvent.upload(input, bigFile)

    await waitFor(() => {
      expect(screen.getByTestId('upload-error')).toHaveTextContent(/too large/)
    })
  })

  it('shows success toast after upload completes', async () => {
    mockUploadAudio.mockResolvedValue({ uploadId: 1, fileName: 'test.mp3' })

    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    const file = new File(['audio'], 'test.mp3', { type: 'audio/mpeg' })
    const input = screen.getByTestId('file-input') as HTMLInputElement
    await userEvent.upload(input, file)

    await waitFor(() => {
      expect(screen.getByTestId('upload-success')).toHaveTextContent(/Processing in background/)
    })
    expect(mockUploadAudio).toHaveBeenCalled()
    // Should return to drop zone (idle state) while toast is visible
    expect(screen.getByTestId('drop-zone')).toBeInTheDocument()
  })

  it('shows error state on API failure', async () => {
    mockUploadAudio.mockRejectedValue(new Error('Network error'))

    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    const file = new File(['audio'], 'test.mp3', { type: 'audio/mpeg' })
    const input = screen.getByTestId('file-input') as HTMLInputElement
    await userEvent.upload(input, file)

    await waitFor(() => {
      expect(screen.getByTestId('upload-error')).toHaveTextContent('Network error')
    })
  })

  it('does not call transcribe or extract after upload', async () => {
    mockUploadAudio.mockResolvedValue({ uploadId: 1, fileName: 'test.mp3' })

    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    const file = new File(['audio'], 'test.mp3', { type: 'audio/mpeg' })
    const input = screen.getByTestId('file-input') as HTMLInputElement
    await userEvent.upload(input, file)

    await waitFor(() => {
      expect(screen.getByTestId('upload-success')).toBeInTheDocument()
    })
    // These should not exist as API functions anymore
    expect(mockUploadAudio).toHaveBeenCalledTimes(1)
  })

  it('shows paste textarea when Paste Text is clicked', async () => {
    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    // Paste area should not be visible initially
    expect(screen.queryByTestId('paste-area')).not.toBeInTheDocument()

    // Click Paste Text button
    await userEvent.click(screen.getByTestId('paste-text-btn'))

    await waitFor(() => {
      expect(screen.getByTestId('paste-textarea')).toBeInTheDocument()
    })
    expect(screen.getByTestId('paste-submit-btn')).toBeDisabled()
  })

  it('submits pasted text and shows success', async () => {
    mockSubmitTextNotes.mockResolvedValue({ uploadId: 1, fileName: 'pasted-text' })

    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    await userEvent.click(screen.getByTestId('paste-text-btn'))
    fireEvent.change(screen.getByTestId('paste-textarea'), { target: { value: 'Alice did great today' } })

    expect(screen.getByTestId('paste-submit-btn')).not.toBeDisabled()
    await userEvent.click(screen.getByTestId('paste-submit-btn'))

    await waitFor(() => {
      expect(screen.getByTestId('upload-success')).toHaveTextContent(/Processing in background/)
    })
    expect(mockSubmitTextNotes).toHaveBeenCalledTimes(1)
    expect(mockSubmitTextNotes.mock.calls[0][0]).toBe('Alice did great today')
  })

  it('shows error when paste submission fails', async () => {
    mockSubmitTextNotes.mockRejectedValue(new Error('Extraction failed'))

    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    await userEvent.click(screen.getByTestId('paste-text-btn'))
    fireEvent.change(screen.getByTestId('paste-textarea'), { target: { value: 'Some notes' } })
    await userEvent.click(screen.getByTestId('paste-submit-btn'))

    await waitFor(() => {
      expect(screen.getByTestId('upload-error')).toHaveTextContent('Extraction failed')
    })
  })

  it('focuses paste textarea when Paste Text is clicked', async () => {
    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    await userEvent.click(screen.getByTestId('paste-text-btn'))

    await waitFor(() => {
      expect(screen.getByTestId('paste-textarea')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByTestId('paste-textarea'))
    })
  })

  it('uploads multiple files sequentially and shows success count', async () => {
    mockUploadAudio.mockResolvedValue({ uploadId: 1 })

    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    const file1 = new File(['audio'], 'a.mp3', { type: 'audio/mpeg' })
    const file2 = new File(['audio'], 'b.mp3', { type: 'audio/mpeg' })
    const input = screen.getByTestId('file-input') as HTMLInputElement
    await userEvent.upload(input, [file1, file2])

    await waitFor(() => {
      expect(screen.getByTestId('upload-success')).toHaveTextContent('2 files uploaded')
    })
    expect(mockUploadAudio).toHaveBeenCalledTimes(2)
  })

  it('shows partial failure summary when some files fail', async () => {
    mockUploadAudio
      .mockResolvedValueOnce({ uploadId: 1 })
      .mockRejectedValueOnce(new Error('Server error'))

    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    const file1 = new File(['audio'], 'ok.mp3', { type: 'audio/mpeg' })
    const file2 = new File(['audio'], 'fail.mp3', { type: 'audio/mpeg' })
    const input = screen.getByTestId('file-input') as HTMLInputElement
    await userEvent.upload(input, [file1, file2])

    await waitFor(() => {
      const errorEl = screen.getByTestId('upload-error')
      expect(errorEl).toHaveTextContent('1 file uploaded')
      expect(errorEl).toHaveTextContent('fail.mp3')
    })
    expect(mockUploadAudio).toHaveBeenCalledTimes(2)
  })

  it('rejects all files when any file exceeds 25MB', async () => {
    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    const okFile = new File(['audio'], 'ok.mp3', { type: 'audio/mpeg' })
    const bigFile = new File(['x'.repeat(100)], 'big.mp3', { type: 'audio/mpeg' })
    Object.defineProperty(bigFile, 'size', { value: 26 * 1024 * 1024 })

    const input = screen.getByTestId('file-input') as HTMLInputElement
    await userEvent.upload(input, [okFile, bigFile])

    await waitFor(() => {
      expect(screen.getByTestId('upload-error')).toHaveTextContent(/too large|exceed the 25 MB limit/)
    })
    expect(mockUploadAudio).not.toHaveBeenCalled()
  })
})
