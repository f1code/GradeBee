import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, vi } from 'vitest'

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

