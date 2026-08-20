vi.unmock('motion/react')

import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listLevels } from '../../api'

const stableGetToken = vi.fn().mockResolvedValue('tok')

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: stableGetToken }),
}))

vi.mock('../../api', () => ({
  listLevels: vi.fn(),
  createLevel: vi.fn(),
  renameLevel: vi.fn(),
  updateLevelReportInstructions: vi.fn(),
  deleteLevel: vi.fn(),
}))

const mockListLevels = listLevels as ReturnType<typeof vi.fn>

import LevelsAdmin from '../LevelsAdmin'

describe('LevelsAdmin empty-slot presence', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListLevels.mockResolvedValue({ levels: [] })
  })

  it('does not show No Levels yet under the exiting add-level form', async () => {
    const user = userEvent.setup()
    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'No Levels yet' })).toBeInTheDocument()
    })
    await user.click(screen.getByText('+ Add Level'))
    await waitFor(() => {
      expect(screen.getByPlaceholderText(/Level name/)).toBeInTheDocument()
    })
    expect(screen.queryByRole('heading', { name: 'No Levels yet' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.getByPlaceholderText(/Level name/)).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'No Levels yet' })).not.toBeInTheDocument()

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'No Levels yet' })).toBeInTheDocument()
    })
    expect(screen.queryByPlaceholderText(/Level name/)).not.toBeInTheDocument()
  })
})
