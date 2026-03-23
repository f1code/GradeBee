import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

describe('DriveSetup', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  it('renders setup button', async () => {
    const { default: DriveSetup } = await import('../DriveSetup')
    render(<DriveSetup />)
    expect(screen.getByTestId('setup-button')).toHaveTextContent('Set Up Google Drive')
  })

  it('shows success state after setup', async () => {
    const user = userEvent.setup()
    const mockFetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          folderId: 'f1', folderUrl: 'https://drive/f1',
          spreadsheetId: 's1', spreadsheetUrl: 'https://sheets/s1',
        }),
      })
    vi.stubGlobal('fetch', mockFetch)

    const { default: DriveSetup } = await import('../DriveSetup')
    render(<DriveSetup />)
    await user.click(screen.getByTestId('setup-button'))

    await waitFor(() => {
      expect(screen.getByTestId('drive-setup-success')).toBeInTheDocument()
    })
  })

  it('shows error on failure', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce({
      ok: false,
      text: () => Promise.resolve('Server error'),
    }))

    const { default: DriveSetup } = await import('../DriveSetup')
    render(<DriveSetup />)
    await user.click(screen.getByTestId('setup-button'))

    await waitFor(() => {
      expect(screen.getByTestId('setup-error')).toBeInTheDocument()
    })
  })
})
