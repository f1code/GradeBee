import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../api', () => ({
  listReportExamples: vi.fn().mockResolvedValue({
    examples: [
      { id: '1', name: 'Report.jpg', content: 'Student showed great improvement in math.', status: 'ready' },
    ],
  }),
  uploadReportExample: vi.fn(),
  updateReportExample: vi.fn().mockResolvedValue({ id: '1', name: 'Updated', content: 'New text' }),
  deleteReportExample: vi.fn(),
  importExampleFromDrive: vi.fn(),
  getGoogleToken: vi.fn(),
}))

vi.mock('../../hooks/useDrivePicker', () => ({
  useDrivePicker: () => ({ openPicker: vi.fn() }),
}))

describe('ReportExamples', () => {
  it('renders toggle button', async () => {
    const { default: ReportExamples } = await import('../ReportExamples')
    render(<ReportExamples />)
    expect(screen.getByText(/Example Report Cards/)).toBeInTheDocument()
  })

  it('shows extracted text when example is clicked', async () => {
    const user = userEvent.setup()
    const { default: ReportExamples } = await import('../ReportExamples')
    render(<ReportExamples />)

    // Expand the examples section
    await user.click(screen.getByText(/Example Report Cards/))

    // Wait for the example to appear
    await waitFor(() => {
      expect(screen.getByText('Report.jpg')).toBeInTheDocument()
    })

    // Content should not be visible yet
    expect(screen.queryByText(/great improvement/)).not.toBeInTheDocument()

    // Click the example name to expand it
    await user.click(screen.getByText('Report.jpg'))

    // Content should now be visible
    await waitFor(() => {
      expect(screen.getByText(/great improvement/)).toBeInTheDocument()
    })

    // Click again to collapse
    await user.click(screen.getByText('Report.jpg'))
    await waitFor(() => {
      expect(screen.queryByText(/great improvement/)).not.toBeInTheDocument()
    })
  })

  it('has a trash icon button for deleting', async () => {
    const user = userEvent.setup()
    const { default: ReportExamples } = await import('../ReportExamples')
    render(<ReportExamples />)

    await user.click(screen.getByText(/Example Report Cards/))

    await waitFor(() => {
      expect(screen.getByLabelText('Delete Report.jpg')).toBeInTheDocument()
    })
  })

  it('enters edit mode and saves', async () => {
    const user = userEvent.setup()
    const { updateReportExample } = await import('../../api')

    const { default: ReportExamples } = await import('../ReportExamples')
    render(<ReportExamples />)

    // Expand section
    await user.click(screen.getByText(/Example Report Cards/))
    await waitFor(() => expect(screen.getByText('Report.jpg')).toBeInTheDocument())

    // Click edit button
    await user.click(screen.getByLabelText('Edit Report.jpg'))

    // Should show edit form with name input
    await waitFor(() => {
      expect(screen.getByDisplayValue('Report.jpg')).toBeInTheDocument()
    })

    // Save without changes
    const saveBtn = screen.getByText('Save')
    expect(saveBtn).not.toBeDisabled()
    await user.click(saveBtn)

    await waitFor(() => {
      expect(updateReportExample).toHaveBeenCalled()
    })
  })
})
