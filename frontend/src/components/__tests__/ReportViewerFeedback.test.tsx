import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, onTestFinished, vi } from 'vitest'

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

async function renderViewer(props?: { onRegenerate?: () => void; html?: string; highlightParagraph?: number }) {
  const { default: ReportViewer } = await import('../ReportViewer')
  const user = userEvent.setup()
  render(
    <ReportViewer
      reportId={42}
      html={props?.html ?? '<p>Report content here</p>'}
      studentName="Alice"
      onRegenerate={props?.onRegenerate}
      highlightParagraph={props?.highlightParagraph}
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

  // Parents key the viewer by report id, so thumbs reset and target the new report.
  it('thumbs after regenerate post to the new report', async () => {
    const { useState } = await import('react')
    const { default: ReportViewer } = await import('../ReportViewer')
    mockRegenerateReport.mockResolvedValue({ id: 43, html: '<p>New report</p>' })
    function Parent() {
      const [report, setReport] = useState({ id: 42, html: '<p>Old report</p>' })
      return (
        <ReportViewer
          key={report.id}
          reportId={report.id}
          html={report.html}
          studentName="Alice"
          onRegenerate={r => setReport(r)}
        />
      )
    }
    const user = userEvent.setup()
    render(<Parent />)

    await user.click(screen.getByTestId('thumb-up'))
    await screen.findByText(/thanks for your feedback/i)

    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    fireEvent.change(screen.getByPlaceholderText(/what should be different/i), { target: { value: 'Shorter' } })
    await user.click(screen.getByRole('button', { name: 'Submit' }))
    await screen.findByText('New report')
    expect(mockRegenerateReport).toHaveBeenCalledWith(42, 'Shorter', expect.any(Function), undefined)

    await user.click(screen.getByTestId('thumb-up'))
    await waitFor(() => {
      expect(mockSubmitFeedback).toHaveBeenLastCalledWith(
        { artifact_type: 'report', artifact_id: 43, rating: 'up' },
        expect.any(Function)
      )
    })
  })
})

// user-event's mouseout carries no relatedTarget, which React reads as leaving
// the page: a pointer click would hide the toolbar before it lands.
const clickToolbar = (name: string) => fireEvent.click(screen.getByRole('button', { name }))

describe('ReportViewer paragraph tweak', () => {
  it('scopes the regenerate feedback to the clicked paragraph', async () => {
    mockRegenerateReport.mockResolvedValue({ id: 43, html: '<p>new</p>' })
    const onRegenerate = vi.fn()
    const user = await renderViewer({ onRegenerate })

    await user.hover(screen.getByText('Report content here'))
    clickToolbar('Tweak paragraph')
    expect(screen.getByTestId('report-tweak-quote')).toHaveTextContent('Report content here')

    await user.type(screen.getByRole('textbox'), 'about 50 words')
    await user.click(screen.getByRole('button', { name: 'Submit' }))

    await waitFor(() => expect(onRegenerate).toHaveBeenCalled())
    expect(mockRegenerateReport).toHaveBeenCalledWith(
      42,
      'about 50 words',
      expect.any(Function),
      { paragraph: 0, excerpt: 'Report content here' }
    )
    expect(onRegenerate).toHaveBeenCalledWith({ id: 43, html: '<p>new</p>' }, 0)
  })

  it.each([
    [true, 'auto'],
    [false, 'smooth'],
  ])('scrolls to and highlights the tweaked paragraph, counting li (reduced motion %s)', async (reduce, behavior) => {
    const scrollIntoView = vi.fn()
    Element.prototype.scrollIntoView = scrollIntoView
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: reduce }))
    onTestFinished(() => {
      vi.unstubAllGlobals()
      delete (Element.prototype as Partial<Element>).scrollIntoView
    })
    await renderViewer({ html: '<ul><li>Name: Alice</li></ul><p>Rewritten</p>', highlightParagraph: 1 })

    expect(screen.getByText('Rewritten')).toHaveClass('para-tweaked')
    await waitFor(() => expect(scrollIntoView).toHaveBeenCalledWith({ block: 'center', behavior }))
  })

  it('focuses the ask; Regenerate drops the paragraph scope', async () => {
    mockRegenerateReport.mockResolvedValue({ id: 43, html: '<p>new</p>' })
    const user = await renderViewer({ onRegenerate: vi.fn() })

    await user.hover(screen.getByText('Report content here'))
    clickToolbar('Tweak paragraph')
    expect(screen.getByRole('textbox')).toHaveFocus()

    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    expect(screen.queryByTestId('report-tweak-quote')).not.toBeInTheDocument()
    await user.type(screen.getByRole('textbox'), 'shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))

    await waitFor(() =>
      expect(mockRegenerateReport).toHaveBeenCalledWith(42, 'shorter', expect.any(Function), undefined)
    )
  })

  it('numbers the paragraph as the server does, counting li', async () => {
    mockRegenerateReport.mockResolvedValue({ id: 43, html: '<p>new</p>' })
    const user = await renderViewer({ onRegenerate: vi.fn(), html: '<ul><li>Name: Alice</li></ul><p>Comment here</p>' })

    await user.hover(screen.getByText('Comment here'))
    await user.hover(screen.getByText('Name: Alice'))
    expect(screen.queryByRole('button', { name: 'Copy paragraph' })).not.toBeInTheDocument()

    await user.hover(screen.getByText('Comment here'))
    clickToolbar('Tweak paragraph')
    await user.type(screen.getByRole('textbox'), 'warmer')
    await user.click(screen.getByRole('button', { name: 'Submit' }))
    await waitFor(() =>
      expect(mockRegenerateReport).toHaveBeenCalledWith(42, 'warmer', expect.any(Function), { paragraph: 1, excerpt: 'Comment here' })
    )
  })

  it('copies the hovered paragraph and hides on leave', async () => {
    const user = await renderViewer({ html: '<p>First</p><p>Second</p>' })
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()

    await user.hover(screen.getByText('Second'))
    clickToolbar('Copy paragraph')
    expect(writeText).toHaveBeenCalledWith('Second')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Copy paragraph' })).toHaveClass('para-btn-success'))
    await user.hover(screen.getByText('First'))
    expect(screen.getByRole('button', { name: 'Copy paragraph' })).not.toHaveClass('para-btn-success')

    await user.unhover(screen.getByText('Second').closest('.report-viewer-body')!)
    expect(screen.queryByRole('button', { name: 'Copy paragraph' })).not.toBeInTheDocument()
  })

  it('serves the paragraph holding the selection, also after the pointer leaves', async () => {
    await renderViewer({ html: '<p>First</p><p>Second</p>' })
    document.getSelection()!.selectAllChildren(screen.getByText('Second'))
    fireEvent(document, new Event('selectionchange'))
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    await screen.findByRole('button', { name: 'Copy paragraph' })
    fireEvent.mouseLeave(screen.getByText('First').closest('.report-viewer-body')!)
    fireEvent.click(await screen.findByRole('button', { name: 'Copy paragraph' }))
    expect(writeText).toHaveBeenCalledWith('Second')
  })

  it('keeps a focused toolbar after the pointer leaves, until focus moves on', async () => {
    const user = await renderViewer()
    await user.hover(screen.getByText('Report content here'))
    const copy = screen.getByRole('button', { name: 'Copy paragraph' })
    copy.focus()
    await user.unhover(copy.closest('.report-viewer-body')!)
    expect(copy).toHaveFocus()

    screen.getByTestId('thumb-up').focus()
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Copy paragraph' })).not.toBeInTheDocument())
  })

  it('keeps the report nodes across re-renders', async () => {
    const user = await renderViewer({ html: '<p>First</p><p>Second</p>' })
    const first = screen.getByText('First')
    await user.hover(first)
    await user.hover(screen.getByText('Second'))
    expect(first.isConnected).toBe(true)
  })

  it('has no Tweak button without regenerate', async () => {
    const user = await renderViewer()
    await user.hover(screen.getByText('Report content here'))
    expect(screen.getByRole('button', { name: 'Copy paragraph' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Tweak paragraph' })).not.toBeInTheDocument()
  })
})
