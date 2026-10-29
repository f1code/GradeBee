import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, onTestFinished, vi } from 'vitest'

const mockListStudentReports = vi.fn()
const mockGetReport = vi.fn()
const mockRegenerateReport = vi.fn()

vi.mock('../../api', () => ({
  listStudentReports: (...args: unknown[]) => mockListStudentReports(...args),
  getReport: (...args: unknown[]) => mockGetReport(...args),
  regenerateReport: (...args: unknown[]) => mockRegenerateReport(...args),
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

    await user.click(await screen.findByRole('button', { name: /jan/i }))
    await screen.findByText('Old report')

    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    await user.type(screen.getByPlaceholderText(/what should be different/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))

    expect(await screen.findByText('New report')).toBeInTheDocument()
    expect(screen.queryByText('Old report')).not.toBeInTheDocument()
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

    await user.click(await screen.findByRole('button', { name: /jan/i }))
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

    await user.click(await screen.findByRole('button', { name: /jan/i }))
    await screen.findByText('Old report')
    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    await user.type(screen.getByPlaceholderText(/what should be different/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))
    await user.click(screen.getByRole('button', { name: /jan/i }))
    await waitFor(() => expect(screen.queryByText('Old report')).not.toBeInTheDocument())

    await act(async () => resolveRegen(report(8, '<p>New report</p>')))
    expect(screen.getAllByRole('button', { name: /jan/i })).toHaveLength(2)
    expect(screen.queryByText('New report')).not.toBeInTheDocument()
  })
})

