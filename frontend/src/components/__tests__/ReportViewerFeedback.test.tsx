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

  // The comment reaches Sentry verbatim and cannot be reliably scrubbed, so the
  // hint is the only mitigation there is — see docs/adr/0003.
  it('warns against student names beside the comment box', async () => {
    const user = await renderViewer()
    expect(screen.queryByTestId('thumb-down-privacy-hint')).not.toBeInTheDocument()

    await user.click(screen.getByTestId('thumb-down'))

    const hint = screen.getByTestId('thumb-down-privacy-hint')
    expect(hint).toHaveTextContent(/student names/i)
    // Announced with the field, not just painted near it — a mitigation a
    // screen-reader user never hears is not a mitigation.
    expect(screen.getByTestId('thumb-down-comment')).toHaveAttribute(
      'aria-describedby',
      hint.id
    )
  })

  // ReportGeneration renders a viewer per report, and AnimatePresence keeps an
  // outgoing one mounted during its exit — so a document-global id would make
  // aria-describedby resolve to the wrong element.
  it('scopes the hint id per report', async () => {
    const { default: ReportViewer } = await import('../ReportViewer')
    const user = userEvent.setup()
    render(
      <>
        <ReportViewer reportId={1} html="<p>a</p>" studentName="Alice" />
        <ReportViewer reportId={2} html="<p>b</p>" studentName="Bo" />
      </>
    )
    const [first, second] = screen.getAllByTestId('thumb-down')
    await user.click(first)
    await user.click(second)

    const ids = screen.getAllByTestId('thumb-down-privacy-hint').map(el => el.id)
    expect(new Set(ids).size).toBe(2)
    expect(ids.every(Boolean)).toBe(true)
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
