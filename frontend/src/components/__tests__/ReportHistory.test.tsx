import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, onTestFinished, vi } from 'vitest'
import { formatNoteDate } from '../../lib/date'

const mockListStudentReports = vi.fn()
const mockGetReport = vi.fn()
const mockRegenerateReport = vi.fn()
const mockDeleteReport = vi.fn()

vi.mock('../../api', () => ({
  listStudentReports: (...args: unknown[]) => mockListStudentReports(...args),
  getReport: (...args: unknown[]) => mockGetReport(...args),
  regenerateReport: (...args: unknown[]) => mockRegenerateReport(...args),
  deleteReport: (...args: unknown[]) => mockDeleteReport(...args),
}))

const stableGetToken = vi.fn().mockResolvedValue('tok')
vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: stableGetToken }),
}))

// Local YYYY-MM-DD, daysAgo days back: an "up to now" report ends on its creation day.
const day = (daysAgo: number) => new Date(Date.now() - daysAgo * 86_400_000).toLocaleDateString('en-CA')

const upTo = (daysAgo: number) => new RegExp(`up to ${formatNoteDate(day(daysAgo))}`, 'i')

function report(id: number, html: string, daysAgo = 0) {
  return {
    id, studentId: 10, student: 'Alice', className: 'Math 101', html,
    startDate: '2026-01-01', endDate: day(daysAgo), createdAt: new Date(Date.now() - daysAgo * 86_400_000).toISOString(),
    termId: 1, termName: 'Term 2', termStartDate: '2026-01-01', termEndDate: '2099-12-31', termArchived: false, termCurrent: true,
  }
}

const lastYear = { termId: 2, termName: 'Term 3', termStartDate: '2025-09-01', termEndDate: '2025-12-19', termArchived: true, termCurrent: false }

async function renderHistory(reports: ReturnType<typeof report>[]) {
  mockListStudentReports.mockResolvedValue({ reports })
  mockGetReport.mockImplementation(async (id: number) => reports.find(r => r.id === id))
  const { default: ReportHistory } = await import('../ReportHistory')
  render(<ReportHistory studentId={10} studentName="Alice" />)
  return userEvent.setup()
}

const termToggle = (name: RegExp) => within(screen.getByRole('heading', { name })).getByRole('button')

beforeEach(() => {
  vi.clearAllMocks()
})

describe('ReportHistory', () => {
  it('swaps in the new report after regenerate and keeps the old one as a draft', async () => {
    const user = await renderHistory([report(7, '<p>Old report</p>')])
    mockRegenerateReport.mockResolvedValue(report(8, '<p>New report</p>'))

    await screen.findByText('Old report')
    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    await user.type(screen.getByPlaceholderText(/what should be different/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))

    expect(await screen.findByText('New report')).toBeInTheDocument()
    expect(screen.queryByText('Old report')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /notes up to/i })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /1 earlier draft/i }))
    expect(screen.getAllByRole('button', { name: /notes up to/i })).toHaveLength(1)
    expect(mockListStudentReports).toHaveBeenCalledTimes(1)
  })

  it('highlights the paragraph a Tweak rewrote until the Term folds', async () => {
    Element.prototype.scrollIntoView = vi.fn()
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: false }))
    onTestFinished(() => {
      vi.unstubAllGlobals()
      delete (Element.prototype as Partial<Element>).scrollIntoView
    })
    const user = await renderHistory([report(7, '<p>Old intro</p><p>Old body</p>')])
    mockRegenerateReport.mockResolvedValue(report(8, '<p>Old intro</p><p>New body</p>'))

    await user.hover(await screen.findByText('Old body'))
    fireEvent.click(screen.getByRole('button', { name: 'Tweak paragraph' }))
    await user.type(screen.getByPlaceholderText(/how should this paragraph change/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))

    expect(await screen.findByText('New body')).toHaveClass('para-tweaked')
    expect(screen.getByText('Old intro')).not.toHaveClass('para-tweaked')

    await user.click(termToggle(/Term 2/))
    await waitFor(() => expect(screen.queryByText('New body')).not.toBeInTheDocument())
    await user.click(termToggle(/Term 2/))
    expect(await screen.findByText('New body')).not.toHaveClass('para-tweaked')
  })

  it('shows the current report with no row to collapse it; drafts open read-only beside it', async () => {
    const user = await renderHistory([
      report(9, '<p>Newest</p>'),
      report(5, '<p>Older</p>', 2),
      report(3, '<p>Oldest</p>', 3),
    ])

    expect(await screen.findByText('Newest')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /notes up to/i })).not.toBeInTheDocument()
    expect(screen.queryByText('Latest')).not.toBeInTheDocument()

    const toggle = screen.getByRole('button', { name: /2 earlier drafts/i })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    expect(toggle).toHaveTextContent('Hide earlier drafts')
    // Creation time tells drafts apart.
    expect(screen.getByText('2 days ago')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /notes up to/i })).toHaveLength(2)

    await user.click(screen.getByRole('button', { name: upTo(2) }))
    await user.hover(await screen.findByText('Older'))
    expect(screen.getByText('Newest')).toBeInTheDocument()
    // One Regenerate: the current report's; the draft has none.
    expect(screen.getAllByRole('button', { name: 'Regenerate' })).toHaveLength(1)
    expect(screen.getAllByRole('button', { name: 'Delete' })).toHaveLength(2)
    expect(screen.getByRole('button', { name: 'Copy paragraph' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Tweak paragraph' })).not.toBeInTheDocument()
  })

  it('a failed report load says so and retries when the Term reopens', async () => {
    const user = await renderHistory([report(9, '<p>Newest</p>')])
    mockGetReport.mockRejectedValueOnce(new Error('Network down'))

    expect(await screen.findByText(/Network down/)).toBeInTheDocument()
    expect(screen.getByText('Could not load this report.')).toBeInTheDocument()
    let resolveRetry: (r: unknown) => void = () => {}
    mockGetReport.mockReturnValueOnce(new Promise(r => { resolveRetry = r }))
    await user.click(termToggle(/Term 2/))
    await user.click(termToggle(/Term 2/))
    expect(screen.queryByText('Could not load this report.')).not.toBeInTheDocument()
    expect(document.querySelector('.report-history-card .honeycomb-spinner')).toBeInTheDocument()

    await act(async () => resolveRetry(report(9, '<p>Newest</p>')))
    expect(screen.getByText('Newest')).toBeInTheDocument()
    expect(screen.queryByText(/Network down/)).not.toBeInTheDocument()
  })

  it('a failed draft load retries when the draft reopens', async () => {
    const user = await renderHistory([report(9, '<p>Newest</p>'), report(5, '<p>Older</p>', 2)])
    await screen.findByText('Newest')
    mockGetReport.mockRejectedValueOnce(new Error('Network down'))

    await user.click(screen.getByRole('button', { name: /1 earlier draft/i }))
    await user.click(screen.getByRole('button', { name: upTo(2) }))
    expect(await screen.findByText('Could not load this report.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: upTo(2) }))
    await user.click(screen.getByRole('button', { name: upTo(2) }))
    expect(await screen.findByText('Older')).toBeInTheDocument()
  })

  it('regenerating a past final swaps it in place and keeps no draft', async () => {
    const user = await renderHistory([{ ...report(8, '<p>Last year final</p>'), ...lastYear }])
    mockRegenerateReport.mockResolvedValue({ ...report(10, '<p>Last year redo</p>'), ...lastYear })

    await screen.findByText('Last year final')
    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    await user.type(screen.getByPlaceholderText(/what should be different/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))

    expect(await screen.findByText('Last year redo')).toBeInTheDocument()
    expect(screen.queryByText('Last year final')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /earlier draft/i })).not.toBeInTheDocument()
  })

  it('deleting a Term\'s only report removes the Term', async () => {
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))
    onTestFinished(() => { vi.unstubAllGlobals() })
    mockDeleteReport.mockResolvedValue(undefined)
    const user = await renderHistory([report(9, '<p>Current</p>'), { ...report(8, '<p>Last year</p>', 1), ...lastYear }])

    await screen.findByText('Current')
    await user.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(screen.queryByRole('heading', { name: /Term 2/ })).not.toBeInTheDocument())
    expect(screen.getByRole('heading', { name: /Term 3/ })).toBeInTheDocument()
  })

  it('deleting the shown report shows the next one', async () => {
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))
    onTestFinished(() => { vi.unstubAllGlobals() })
    mockDeleteReport.mockResolvedValue(undefined)
    const user = await renderHistory([
      report(9, '<p>Newest</p>'),
      report(5, '<p>Older</p>', 2),
      report(3, '<p>Oldest</p>', 3),
    ])

    await screen.findByText('Newest')
    await user.click(screen.getByRole('button', { name: 'Delete' }))

    expect(await screen.findByText('Older')).toBeInTheDocument()
    expect(screen.queryByText('Newest')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /1 earlier draft/i })).toBeInTheDocument()
  })

  it('folds each Term; a past Term opens on its final report, editable, with no drafts', async () => {
    const user = await renderHistory([
      report(9, '<p>Current</p>'),
      { ...report(8, '<p>Last year final</p>', 1), ...lastYear },
      { ...report(4, '<p>Last year draft</p>', 2), ...lastYear },
    ])

    expect(await screen.findByText('Current')).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 4 }).map(h => h.textContent)).toEqual([
      'Term 2 · January 1, 2026 – December 31, 2099',
      'Term 3 (archived) · September 1, 2025 – December 19, 2025',
    ])
    expect(termToggle(/Term 2/)).toHaveAttribute('aria-expanded', 'true')
    expect(termToggle(/Term 3/)).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Last year final')).not.toBeInTheDocument()

    await user.click(termToggle(/Term 3/))
    expect(await screen.findByText('Last year final')).toBeInTheDocument()
    const archived = screen.getByRole('heading', { name: /Term 3/ }).closest('section')!
    expect(within(archived).getByRole('button', { name: 'Regenerate' })).toBeInTheDocument()
    expect(within(archived).queryByRole('button', { name: /earlier draft|notes up to/i })).not.toBeInTheDocument()
    expect(mockGetReport).not.toHaveBeenCalledWith(4, expect.anything())

    await user.click(termToggle(/Term 2/))
    await waitFor(() => expect(screen.queryByText('Current')).not.toBeInTheDocument())
    expect(screen.getByText('Last year final')).toBeInTheDocument()
  })

  it('opens the newest Term with reports, ordered by start date', async () => {
    const user = await renderHistory([
      { ...report(8, '<p>Last year redo</p>'), ...lastYear },
      report(9, '<p>Current</p>', 1),
    ])

    expect(await screen.findByText('Current')).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 4 }).map(h => h.textContent?.split(' · ')[0]))
      .toEqual(['Term 2', 'Term 3 (archived)'])
    expect(screen.queryByText('Last year redo')).not.toBeInTheDocument()
    await user.click(termToggle(/Term 3/))
    expect(await screen.findByText('Last year redo')).toBeInTheDocument()
  })

  it('opens last Term when the current Term has no reports', async () => {
    await renderHistory([
      { ...report(9, '<p>Last Term</p>'), ...lastYear },
      { ...report(4, '<p>Last Term draft</p>', 2), ...lastYear },
    ])

    expect(await screen.findByText('Last Term')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Regenerate' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /earlier draft/i })).not.toBeInTheDocument()
  })
})
