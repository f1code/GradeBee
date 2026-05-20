import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, vi } from 'vitest'

const mockSubmitFeedback = vi.fn()
const mockRegenerateReport = vi.fn()
const mockDeleteReport = vi.fn()

vi.mock('../../api', () => ({
  submitFeedback: (...args: unknown[]) => mockSubmitFeedback(...args),
  regenerateReport: (...args: unknown[]) => mockRegenerateReport(...args),
  deleteReport: (...args: unknown[]) => mockDeleteReport(...args),
}))

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

beforeEach(() => {
  vi.clearAllMocks()
  mockSubmitFeedback.mockResolvedValue({ id: 1, created_at: '2026-01-01' })
})

async function renderViewer(props?: { onRegenerate?: () => void }) {
  const { default: ReportViewer } = await import('../ReportViewer')
  const user = userEvent.setup()
  render(
    <ReportViewer
      reportId={42}
      html="<p>Report content here</p>"
      studentName="Alice"
      onRegenerate={props?.onRegenerate}
    />
  )
  return user
}

describe('ReportViewer thumbs feedback', () => {
  it('shows thumbs buttons', async () => {
    await renderViewer()
    expect(screen.getByTestId('thumb-up')).toBeInTheDocument()
    expect(screen.getByTestId('thumb-down')).toBeInTheDocument()
  })

  it('thumbs-up fires submitFeedback immediately', async () => {
    const user = await renderViewer()
    await user.click(screen.getByTestId('thumb-up'))
    await waitFor(() => {
      expect(mockSubmitFeedback).toHaveBeenCalledWith(
        { artifact_type: 'report', artifact_id: 42, rating: 'up' },
        expect.any(Function)
      )
    })
    expect(screen.getByText(/thanks for your feedback/i)).toBeInTheDocument()
  })

  it('thumbs-down reveals comment textarea', async () => {
    const user = await renderViewer()
    await user.click(screen.getByTestId('thumb-down'))
    expect(screen.getByTestId('thumb-down-comment')).toBeInTheDocument()
  })

  it('thumbs-down submits with comment', async () => {
    const user = await renderViewer()
    await user.click(screen.getByTestId('thumb-down'))
    // Use fireEvent.change for reliable state update in jsdom
    fireEvent.change(screen.getByTestId('thumb-down-comment'), { target: { value: 'Tone was off' } })
    await user.click(screen.getByTestId('thumb-down-submit'))
    await waitFor(() => {
      expect(mockSubmitFeedback).toHaveBeenCalledWith(
        {
          artifact_type: 'report',
          artifact_id: 42,
          rating: 'down',
          comment: 'Tone was off',
        },
        expect.any(Function)
      )
    })
    expect(screen.getByText(/thanks for your feedback/i)).toBeInTheDocument()
  })

  it('thumbs-down without comment omits comment field', async () => {
    const user = await renderViewer()
    await user.click(screen.getByTestId('thumb-down'))
    // Submit without typing a comment (empty textarea → undefined)
    await user.click(screen.getByTestId('thumb-down-submit'))
    await waitFor(() => {
      expect(mockSubmitFeedback).toHaveBeenCalledWith(
        {
          artifact_type: 'report',
          artifact_id: 42,
          rating: 'down',
          comment: undefined,
        },
        expect.any(Function)
      )
    })
    expect(screen.getByText(/thanks for your feedback/i)).toBeInTheDocument()
  })
})
