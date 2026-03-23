import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock all API functions
const mockUploadAudio = vi.fn()
const mockTranscribeAudio = vi.fn()
const mockExtractFromTranscript = vi.fn()
const mockCreateNotes = vi.fn()
const mockGetGoogleToken = vi.fn()
const mockImportFromDrive = vi.fn()

vi.mock('../../api', () => ({
  uploadAudio: (...args: unknown[]) => mockUploadAudio(...args),
  transcribeAudio: (...args: unknown[]) => mockTranscribeAudio(...args),
  extractFromTranscript: (...args: unknown[]) => mockExtractFromTranscript(...args),
  createNotes: (...args: unknown[]) => mockCreateNotes(...args),
  getGoogleToken: (...args: unknown[]) => mockGetGoogleToken(...args),
  importFromDrive: (...args: unknown[]) => mockImportFromDrive(...args),
}))

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../hooks/useDrivePicker', () => ({
  useDrivePicker: () => ({ openPicker: vi.fn().mockResolvedValue(null) }),
}))

vi.mock('../../hooks/useMediaQuery', () => ({
  useMediaQuery: () => false, // desktop by default
}))

describe('AudioUpload', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // Tier 3 — smoke
  it('renders drop zone in idle state', async () => {
    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)
    expect(screen.getByTestId('drop-zone')).toBeInTheDocument()
    expect(screen.getByText('Upload Audio')).toBeInTheDocument()
  })

  // Tier 4 — interactions
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

  it('processes file through upload → transcribe → extract flow', async () => {
    mockUploadAudio.mockResolvedValue({ fileId: 'f1', fileName: 'test.mp3' })
    mockTranscribeAudio.mockResolvedValue({ fileId: 'f1', transcript: 'Alice did well' })
    mockExtractFromTranscript.mockResolvedValue({
      students: [{ name: 'Alice', class: 'Math', summary: 'Good', confidence: 0.9 }],
      date: '2026-03-20',
    })

    const { default: AudioUpload } = await import('../AudioUpload')
    render(<AudioUpload />)

    const file = new File(['audio'], 'test.mp3', { type: 'audio/mpeg' })
    const input = screen.getByTestId('file-input') as HTMLInputElement
    await userEvent.upload(input, file)

    await waitFor(() => {
      expect(screen.getByText('Confirm Student Notes')).toBeInTheDocument()
    })
    expect(mockUploadAudio).toHaveBeenCalled()
    expect(mockTranscribeAudio).toHaveBeenCalledWith('f1', expect.any(Function))
    expect(mockExtractFromTranscript).toHaveBeenCalled()
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
})
