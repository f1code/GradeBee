import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'

const mockGenerateReports = vi.fn()
const mockListClasses = vi.fn()
const mockListStudents = vi.fn()

vi.mock('../../api', () => ({
  generateReports: (...args: unknown[]) => mockGenerateReports(...args),
  listClasses: (...args: unknown[]) => mockListClasses(...args),
  listStudents: (...args: unknown[]) => mockListStudents(...args),
  listReportExamples: vi.fn().mockResolvedValue({ examples: [] }),
  uploadReportExample: vi.fn(),
  deleteReportExample: vi.fn(),
  regenerateReport: vi.fn(),
  deleteReport: vi.fn(),
}))

const stableGetToken = vi.fn().mockResolvedValue('tok')
vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: stableGetToken }),
}))

async function renderWithStudents() {
  mockListClasses.mockResolvedValue({
    classes: [{ id: 1, name: 'Math 101', studentCount: 2 }],
  })
  mockListStudents.mockResolvedValue({
    students: [
      { id: 10, name: 'Alice', classId: 1 },
      { id: 11, name: 'Bob', classId: 1 },
    ],
  })
  const { default: ReportGeneration } = await import('../ReportGeneration')
  const user = userEvent.setup()
  render(<ReportGeneration />)
  await waitFor(() => screen.getByText('Math 101'))
  return user
}

describe('ReportGeneration', () => {
  it('shows loading then class selection', async () => {
    await renderWithStudents()
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
  })

  it('select all toggles entire class', async () => {
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))
    expect(screen.getByText(/Generate 2 Report/)).toBeInTheDocument()

    await user.click(screen.getByText('Math 101'))
    expect(screen.getByText(/Generate 0 Report/)).toBeInTheDocument()
  })

  it('generates reports on submit', async () => {
    mockGenerateReports.mockResolvedValue({
      reports: [
        { id: 1, student: 'Alice', class: 'Math 101', studentId: 10, html: '<p>Alice report</p>', startDate: '2026-01-01', endDate: '2026-03-27', createdAt: '2026-03-27T12:00:00Z' },
        { id: 2, student: 'Bob', class: 'Math 101', studentId: 11, html: '<p>Bob report</p>', startDate: '2026-01-01', endDate: '2026-03-27', createdAt: '2026-03-27T12:00:00Z' },
      ],
      error: null,
    })
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))
    expect(screen.getByText(/Generate 2 Report/)).toBeInTheDocument()

    await user.click(screen.getByText(/Generate 2 Report/))
    await waitFor(() => {
      expect(screen.getByText('Generated Reports')).toBeInTheDocument()
    })
    // Results show student names in result cards
    expect(screen.getAllByText('Alice')).toHaveLength(2) // selector + result
    expect(screen.getAllByText('Bob')).toHaveLength(2)
  })

  it('shows error on failed generation', async () => {
    mockGenerateReports.mockRejectedValue(new Error('Generation failed'))
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))

    await user.click(screen.getByText(/Generate 2 Report/))
    await waitFor(() => {
      expect(screen.getByText(/Generation failed/)).toBeInTheDocument()
    })
  })
})
