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
function day(daysAgo: number) {
  const d = new Date(Date.now() - daysAgo * 86_400_000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const upTo = (daysAgo: number, latest = false) =>
  new RegExp(`up to ${formatNoteDate(day(daysAgo))}${latest ? '.*latest' : ''}`, 'i')

function report(id: number, html: string, daysAgo = 0) {
  return {
    id, studentId: 10, student: 'Alice', className: 'Math 101', html,
    startDate: '2026-01-01', endDate: day(daysAgo), createdAt: new Date(Date.now() - daysAgo * 86_400_000).toISOString(),
    termId: 1, termName: 'Term 2', termStartDate: '2026-01-01', termEndDate: '2099-12-31', termArchived: false, termCurrent: true,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('ReportHistory', () => {
  it('lists and expands the new report after regenerate, keeping the old one', async () => {
    const old = report(7, '<p>Old report</p>')
    mockListStudentReports.mockResolvedValue({ reports: [old] })
    mockGetReport.mockResolvedValue(old)
    mockRegenerateReport.mockResolvedValue(report(8, '<p>New report</p>'))
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    await screen.findByText('Old report')

    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    await user.type(screen.getByPlaceholderText(/what should be different/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))

    expect(await screen.findByText('New report')).toBeInTheDocument()
    expect(screen.queryByText('Old report')).not.toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /notes up to/i })).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: /1 earlier draft/i }))
    expect(screen.getAllByRole('button', { name: /notes up to/i })).toHaveLength(2)
    expect(mockListStudentReports).toHaveBeenCalledTimes(1)
  })

  it('highlights the paragraph a Tweak rewrote in the new report', async () => {
    Element.prototype.scrollIntoView = vi.fn()
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: false }))
    onTestFinished(() => {
      vi.unstubAllGlobals()
      delete (Element.prototype as Partial<Element>).scrollIntoView
    })
    const old = report(7, '<p>Old intro</p><p>Old body</p>')
    mockListStudentReports.mockResolvedValue({ reports: [old] })
    mockGetReport.mockResolvedValue(old)
    mockRegenerateReport.mockResolvedValue(report(8, '<p>Old intro</p><p>New body</p>'))
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    await user.hover(await screen.findByText('Old body'))
    fireEvent.click(screen.getByRole('button', { name: 'Tweak paragraph' }))
    await user.type(screen.getByPlaceholderText(/how should this paragraph change/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))

    expect(await screen.findByText('New body')).toHaveClass('para-tweaked')
    expect(screen.getByText('Old intro')).not.toHaveClass('para-tweaked')

    mockGetReport.mockResolvedValue(report(8, '<p>Old intro</p><p>New body</p>'))
    const newRow = screen.getAllByRole('button', { name: /notes up to/i })[0]
    await user.click(newRow)
    await waitFor(() => expect(screen.queryByText('New body')).not.toBeInTheDocument())
    await user.click(newRow)
    expect(await screen.findByText('New body')).not.toHaveClass('para-tweaked')
  })

  it('a regenerate finishing after the report closed lists the new one but leaves it closed', async () => {
    const old = report(7, '<p>Old report</p>')
    mockListStudentReports.mockResolvedValue({ reports: [old] })
    mockGetReport.mockResolvedValue(old)
    let resolveRegen: (r: unknown) => void = () => {}
    mockRegenerateReport.mockReturnValue(new Promise(r => { resolveRegen = r }))
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    await screen.findByText('Old report')
    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    await user.type(screen.getByPlaceholderText(/what should be different/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))
    await user.click(screen.getByRole('button', { name: /notes up to/i }))
    await waitFor(() => expect(screen.queryByText('Old report')).not.toBeInTheDocument())

    await act(async () => resolveRegen(report(8, '<p>New report</p>')))
    expect(screen.getAllByRole('button', { name: /notes up to/i })).toHaveLength(1)
    expect(screen.getByRole('button', { name: /1 earlier draft/i })).toBeInTheDocument()
    expect(screen.queryByText('New report')).not.toBeInTheDocument()
  })

  it('opens the newest report as Latest and keeps earlier drafts read-only behind a toggle', async () => {
    const reports = [
      report(9, '<p>Newest</p>'),
      report(5, '<p>Older</p>', 2),
      report(3, '<p>Oldest</p>', 3),
    ]
    mockListStudentReports.mockResolvedValue({ reports })
    mockGetReport.mockImplementation(async (id: number) => reports.find(r => r.id === id))
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    expect(await screen.findByText('Newest')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: upTo(0, true) })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: upTo(2) })).not.toBeInTheDocument()

    const toggle = screen.getByRole('button', { name: /2 earlier drafts/i })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    // Creation time tells drafts apart; the newest row keeps it in a tooltip.
    expect(screen.getByText('2 days ago')).toBeInTheDocument()
    expect(screen.queryByText('now')).not.toBeInTheDocument()
    expect(toggle).toHaveTextContent('Hide earlier drafts')
    expect(screen.queryByRole('button', { name: upTo(2, true) })).not.toBeInTheDocument()
    // Drafts fold inside the Term's one card, under the footer.
    const card = document.getElementById(toggle.getAttribute('aria-controls')!)!.closest('.report-history-card')!
    expect(document.querySelectorAll('.report-history-card')).toHaveLength(1)
    expect(within(card as HTMLElement).getAllByRole('button', { name: /notes up to/i })).toHaveLength(3)

    await user.click(screen.getByRole('button', { name: upTo(2) }))
    await user.hover(await screen.findByText('Older'))
    expect(screen.getByRole('button', { name: 'Copy to Clipboard' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy paragraph' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Regenerate' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Tweak paragraph' })).not.toBeInTheDocument()
  })

  it('keeps the last-clicked report when an earlier fetch resolves after it', async () => {
    const reports = [
      report(9, '<p>Newest</p>'),
      report(5, '<p>Older</p>', 2),
    ]
    mockListStudentReports.mockResolvedValue({ reports })
    const pending: Record<number, (r: unknown) => void> = {}
    mockGetReport.mockImplementation((id: number) => new Promise(r => { pending[id] = r }))
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    await user.click(await screen.findByRole('button', { name: /earlier draft/i }))
    await user.click(screen.getByRole('button', { name: upTo(2) }))
    await act(async () => pending[5](reports[1]))
    await act(async () => pending[9](reports[0]))

    expect(screen.getByText('Older')).toBeInTheDocument()
    expect(screen.queryByText('Newest')).not.toBeInTheDocument()
  })

  it('deleting Latest promotes the next report', async () => {
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))
    onTestFinished(() => { vi.unstubAllGlobals() })
    const reports = [
      report(9, '<p>Newest</p>'),
      report(5, '<p>Older</p>', 2),
      report(3, '<p>Oldest</p>', 3),
    ]
    mockListStudentReports.mockResolvedValue({ reports })
    mockGetReport.mockImplementation(async (id: number) => reports.find(r => r.id === id))
    mockDeleteReport.mockResolvedValue(undefined)
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    await screen.findByText('Newest')
    await user.click(screen.getByRole('button', { name: 'Delete' }))

    expect(await screen.findByRole('button', { name: upTo(2, true) })).toBeInTheDocument()
    await waitFor(() => expect(screen.getAllByRole('button', { name: /latest/i })).toHaveLength(1))
    expect(screen.getByRole('button', { name: /1 earlier draft/i })).toBeInTheDocument()
  })

  it('groups by Term: each Term keeps its own editable Latest and read-only drafts', async () => {
    const lastYear = { termId: 2, termName: 'Term 3', termStartDate: '2025-09-01', termEndDate: '2025-12-19', termArchived: true, termCurrent: false }
    const reports = [
      report(9, '<p>Current</p>'),
      { ...report(8, '<p>Last year final</p>', 1), ...lastYear },
      { ...report(4, '<p>Last year draft</p>', 2), ...lastYear },
    ]
    mockListStudentReports.mockResolvedValue({ reports })
    mockGetReport.mockImplementation(async (id: number) => reports.find(r => r.id === id))
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    expect(await screen.findByText('Current')).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 4 }).map(h => h.textContent))
      .toEqual(['Term 2 · Jan 1, 2026 – Dec 31, 2099', 'Term 3 (archived) · Sep 1, 2025 – Dec 19, 2025'])
    // Latest badge on the newest Term only; past Terms' newest stays editable without it.
    expect(screen.getAllByRole('button', { name: /latest/i })).toHaveLength(1)

    const archived = screen.getByRole('heading', { name: /Term 3/ }).closest('section')!
    expect(within(archived).queryByText('Last year final')).not.toBeInTheDocument()
    expect(within(archived).queryByText('Latest')).not.toBeInTheDocument()
    await user.click(within(archived).getByRole('button', { name: upTo(1) }))
    expect(await screen.findByText('Last year final')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Regenerate' })).toBeInTheDocument()

    expect(within(archived).queryByRole('button', { name: upTo(2) })).not.toBeInTheDocument()
    await user.click(within(archived).getByRole('button', { name: /1 earlier draft/i }))
    await user.click(within(archived).getByRole('button', { name: upTo(2) }))
    expect(await screen.findByText('Last year draft')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Regenerate' })).not.toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /earlier draft/i })).toHaveLength(1)
    expect(archived.querySelectorAll('.report-history-card')).toHaveLength(1)
  })

  it('orders Terms by start date even when an older Term holds the newest report', async () => {
    const reports = [
      { ...report(8, '<p>Last year redo</p>'), termId: 2, termName: 'Term 3', termStartDate: '2025-09-01', termEndDate: '2025-12-19', termArchived: true, termCurrent: false },
      report(9, '<p>Current</p>', 1),
    ]
    mockListStudentReports.mockResolvedValue({ reports })
    mockGetReport.mockImplementation(async (id: number) => reports.find(r => r.id === id))
    const { default: ReportHistory } = await import('../ReportHistory')
    render(<ReportHistory studentId={10} studentName="Alice" />)

    expect(await screen.findByText('Last year redo')).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 4 }).map(h => h.textContent?.split(' · ')[0]))
      .toEqual(['Term 2', 'Term 3 (archived)'])
  })

  it('shows no Latest badge when no report sits in the current Term', async () => {
    const reports = [{ ...report(9, '<p>Last Term</p>'), termStartDate: '2025-09-01', termEndDate: '2025-12-19', termCurrent: false }]
    mockListStudentReports.mockResolvedValue({ reports })
    mockGetReport.mockResolvedValue(reports[0])
    const { default: ReportHistory } = await import('../ReportHistory')
    render(<ReportHistory studentId={10} studentName="Alice" />)

    expect(await screen.findByText('Last Term')).toBeInTheDocument()
    expect(screen.queryByText('Latest')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Regenerate' })).toBeInTheDocument()
  })
})

