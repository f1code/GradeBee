import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../api', () => ({
  listReportExamples: vi.fn().mockResolvedValue({ examples: [] }),
  uploadReportExample: vi.fn(),
  deleteReportExample: vi.fn(),
}))

describe('ReportExamples', () => {
  it('renders toggle button', async () => {
    const { default: ReportExamples } = await import('../ReportExamples')
    render(<ReportExamples />)
    expect(screen.getByText(/Example Report Cards/)).toBeInTheDocument()
  })
})
