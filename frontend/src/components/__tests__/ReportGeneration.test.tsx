import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'

const mockGenerateReports = vi.fn()

vi.mock('../../api', () => ({
  generateReports: (...args: unknown[]) => mockGenerateReports(...args),
  listReportExamples: vi.fn().mockResolvedValue({ examples: [] }),
  uploadReportExample: vi.fn(),
  deleteReportExample: vi.fn(),
}))

const stableGetToken = vi.fn().mockResolvedValue('tok')
vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: stableGetToken }),
}))

async function renderWithStudents() {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({
      classes: [
        { name: 'Math 101', students: [{ name: 'Alice' }, { name: 'Bob' }] },
      ],
    }),
  }))
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
    // Click the label text to toggle class
    await user.click(screen.getByText('Math 101'))
    expect(screen.getByText(/Generate 2 Report/)).toBeInTheDocument()

    await user.click(screen.getByText('Math 101'))
    expect(screen.getByText(/Generate 0 Report/)).toBeInTheDocument()
  })

  it('generates reports on submit', async () => {
    mockGenerateReports.mockResolvedValue({
      reports: [
        { student: 'Alice', class: 'Math 101', docId: 'd1', docUrl: 'https://docs/d1', skipped: false },
        { student: 'Bob', class: 'Math 101', docId: 'd2', docUrl: 'https://docs/d2', skipped: false },
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
    expect(screen.getAllByText('Open in Docs →')).toHaveLength(2)
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
