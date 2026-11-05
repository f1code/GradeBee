import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, onTestFinished, vi } from 'vitest'

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

function report(id: number, html: string) {
  return {
    id, studentId: 10, student: 'Alice', className: 'Math 101', html,
    startDate: '2026-01-01', endDate: '2026-03-31', createdAt: new Date().toISOString(),
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
    expect(screen.getAllByRole('button', { name: /jan/i })).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: /earlier drafts \(1\)/i }))
    expect(screen.getAllByRole('button', { name: /jan/i })).toHaveLength(2)
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
    const newRow = screen.getAllByRole('button', { name: /jan/i })[0]
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
    await user.click(screen.getByRole('button', { name: /jan/i }))
    await waitFor(() => expect(screen.queryByText('Old report')).not.toBeInTheDocument())

    await act(async () => resolveRegen(report(8, '<p>New report</p>')))
    expect(screen.getAllByRole('button', { name: /jan/i })).toHaveLength(1)
    expect(screen.getByRole('button', { name: /earlier drafts \(1\)/i })).toBeInTheDocument()
    expect(screen.queryByText('New report')).not.toBeInTheDocument()
  })

  it('opens the newest report as Latest and keeps earlier drafts read-only behind a toggle', async () => {
    const reports = [
      { ...report(9, '<p>Newest</p>'), startDate: '2026-03-01' },
      { ...report(5, '<p>Older</p>'), startDate: '2026-02-01' },
      { ...report(3, '<p>Oldest</p>'), startDate: '2026-01-01' },
    ]
    mockListStudentReports.mockResolvedValue({ reports })
    mockGetReport.mockImplementation(async (id: number) => reports.find(r => r.id === id))
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    expect(await screen.findByText('Newest')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /mar.*latest/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /feb/i })).not.toBeInTheDocument()

    const toggle = screen.getByRole('button', { name: /earlier drafts \(2\)/i })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.queryByRole('button', { name: /feb.*latest/i })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /feb/i }))
    await user.hover(await screen.findByText('Older'))
    expect(screen.getByRole('button', { name: 'Copy to Clipboard' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy paragraph' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Regenerate' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Tweak paragraph' })).not.toBeInTheDocument()
  })

  it('keeps the last-clicked report when an earlier fetch resolves after it', async () => {
    const reports = [
      { ...report(9, '<p>Newest</p>'), startDate: '2026-03-01' },
      { ...report(5, '<p>Older</p>'), startDate: '2026-02-01' },
    ]
    mockListStudentReports.mockResolvedValue({ reports })
    const pending: Record<number, (r: unknown) => void> = {}
    mockGetReport.mockImplementation((id: number) => new Promise(r => { pending[id] = r }))
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    await user.click(await screen.findByRole('button', { name: /earlier drafts/i }))
    await user.click(screen.getByRole('button', { name: /feb/i }))
    await act(async () => pending[5](reports[1]))
    await act(async () => pending[9](reports[0]))

    expect(screen.getByText('Older')).toBeInTheDocument()
    expect(screen.queryByText('Newest')).not.toBeInTheDocument()
  })

  it('deleting Latest promotes the next report', async () => {
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))
    onTestFinished(() => { vi.unstubAllGlobals() })
    const reports = [
      { ...report(9, '<p>Newest</p>'), startDate: '2026-03-01' },
      { ...report(5, '<p>Older</p>'), startDate: '2026-02-01' },
      { ...report(3, '<p>Oldest</p>'), startDate: '2026-01-01' },
    ]
    mockListStudentReports.mockResolvedValue({ reports })
    mockGetReport.mockImplementation(async (id: number) => reports.find(r => r.id === id))
    mockDeleteReport.mockResolvedValue(undefined)
    const { default: ReportHistory } = await import('../ReportHistory')
    const user = userEvent.setup()
    render(<ReportHistory studentId={10} studentName="Alice" />)

    await screen.findByText('Newest')
    await user.click(screen.getByRole('button', { name: 'Delete' }))

    expect(await screen.findByRole('button', { name: /feb.*latest/i })).toBeInTheDocument()
    await waitFor(() => expect(screen.getAllByRole('button', { name: /latest/i })).toHaveLength(1))
    expect(screen.getByRole('button', { name: /earlier drafts \(1\)/i })).toBeInTheDocument()
  })
})

