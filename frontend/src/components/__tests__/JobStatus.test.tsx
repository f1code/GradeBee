import { render, screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { JobListResponse } from '../../api'

const mockFetchJobs = vi.fn()
const mockRetryFailedJobs = vi.fn()

vi.mock('../../api', () => ({
  fetchJobs: (...args: unknown[]) => mockFetchJobs(...args),
  retryFailedJobs: (...args: unknown[]) => mockRetryFailedJobs(...args),
}))

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

const emptyJobs: JobListResponse = { active: [], failed: [], done: [] }

describe('JobStatus', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers({ shouldAdvanceTime: true })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders nothing when no jobs exist', async () => {
    mockFetchJobs.mockResolvedValue(emptyJobs)
    const { default: JobStatus } = await import('../JobStatus')

    const { container } = render(<JobStatus />)
    await waitFor(() => {
      expect(mockFetchJobs).toHaveBeenCalled()
    })
    expect(container.querySelector('[data-testid="job-status"]')).not.toBeInTheDocument()
  })

  it('renders active jobs with spinner and status label', async () => {
    mockFetchJobs.mockResolvedValue({
      active: [{ uploadId: 1, fileId: 'f1', fileName: 'lesson.m4a', status: 'transcribing', createdAt: '2026-03-26T10:00:00Z' }],
      failed: [],
      done: [],
    })
    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-active')).toBeInTheDocument()
    })
    expect(screen.getByText('lesson.m4a')).toBeInTheDocument()
    expect(screen.getByText('Transcribing')).toBeInTheDocument()
  })

  it('renders failed jobs with retry button', async () => {
    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [{ uploadId: 2, fileId: 'f2', fileName: 'bad.mp3', status: 'failed', error: 'Whisper down', createdAt: '2026-03-26T09:00:00Z' }],
      done: [],
    })
    mockRetryFailedJobs.mockResolvedValue(undefined)
    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-failed')).toBeInTheDocument()
    })
    expect(screen.getByText('bad.mp3')).toBeInTheDocument()
    expect(screen.getByText('Whisper down')).toBeInTheDocument()
    expect(screen.getByTestId('job-retry-btn')).toBeInTheDocument()
  })

  it('retry button calls retryFailedJobs and re-polls', async () => {
    vi.useRealTimers()
    const user = userEvent.setup()

    // All polls return failed jobs until retry resolves
    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [{ uploadId: 2, fileId: 'f2', fileName: 'bad.mp3', status: 'failed', error: 'err', createdAt: '2026-03-26T09:00:00Z' }],
      done: [],
    })
    mockRetryFailedJobs.mockResolvedValue(undefined)

    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-retry-btn')).toBeInTheDocument()
    })

    await user.click(screen.getByTestId('job-retry-btn'))

    await waitFor(() => {
      expect(mockRetryFailedJobs).toHaveBeenCalled()
    })
  })

  it('renders done jobs with note count', async () => {
    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [],
      done: [{
        uploadId: 3,
        fileId: 'f3',
        fileName: 'complete.m4a',
        status: 'done' as const,
        noteLinks: [{ name: 'Alice', url: 'https://docs.google.com/document/d/doc1/edit' }, { name: 'Bob', url: 'https://docs.google.com/document/d/doc2/edit' }],
        createdAt: '2026-03-26T08:00:00Z',
      }],
    })
    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-done')).toBeInTheDocument()
    })
    expect(screen.getByText('complete.m4a')).toBeInTheDocument()
    expect(screen.getByText('2 notes created')).toBeInTheDocument()
    const links = screen.getAllByText(/Alice|Bob/)
    expect(links).toHaveLength(2)
  })

  it('shows "new" badge for newly completed jobs', async () => {
    // First poll: no done jobs
    mockFetchJobs
      .mockResolvedValueOnce({ active: [{ uploadId: 1, fileId: 'f1', fileName: 'a.m4a', status: 'transcribing', createdAt: '2026-03-26T10:00:00Z' }], failed: [], done: [] })
      // Second poll: job is done
      .mockResolvedValue({ active: [], failed: [], done: [{ uploadId: 1, fileId: 'f1', fileName: 'a.m4a', status: 'done' as const, noteLinks: [{ name: 'Student', url: 'url' }], createdAt: '2026-03-26T10:00:00Z' }] })

    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-active')).toBeInTheDocument()
    })

    // Advance timer to trigger second poll
    await act(async () => {
      vi.advanceTimersByTime(3_500)
    })

    await waitFor(() => {
      expect(screen.getByTestId('job-new-badge')).toBeInTheDocument()
    })
  })
})
