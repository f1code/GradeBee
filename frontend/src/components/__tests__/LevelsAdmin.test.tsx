import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listLevels, createLevel, renameLevel, updateLevelReportInstructions, deleteLevel } from '../../api'

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
const mockCreateLevel = createLevel as ReturnType<typeof vi.fn>
const mockRenameLevel = renameLevel as ReturnType<typeof vi.fn>
const mockUpdateInstructions = updateLevelReportInstructions as ReturnType<typeof vi.fn>
const mockDeleteLevel = deleteLevel as ReturnType<typeof vi.fn>

import LevelsAdmin from '../LevelsAdmin'

describe('LevelsAdmin', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows loading state initially', () => {
    mockListLevels.mockReturnValue(new Promise(() => {}))
    render(<LevelsAdmin />)
    expect(screen.getByTestId('levels-admin-loading')).toBeInTheDocument()
  })

  it('renders Levels after fetch', async () => {
    mockListLevels.mockResolvedValueOnce({
      levels: [
        { id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z' },
      ],
    })

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByTestId('levels-admin')).toBeInTheDocument()
    })
    expect(screen.getByText('Marcia')).toBeInTheDocument()
  })

  it('shows empty state when no Levels', async () => {
    mockListLevels.mockResolvedValueOnce({ levels: [] })

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByText('No Levels yet')).toBeInTheDocument()
    })
  })

  it('shows error state on fetch failure', async () => {
    mockListLevels.mockRejectedValueOnce(new Error('Network error'))

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByTestId('levels-admin-error')).toBeInTheDocument()
    })
    expect(screen.getByText('Network error')).toBeInTheDocument()
  })

  it('creates a Level and adds it to the list', async () => {
    const user = userEvent.setup()
    mockListLevels.mockResolvedValueOnce({ levels: [] })
    mockCreateLevel.mockResolvedValueOnce({
      id: 2, groupId: 'org_a', name: 'Oliver', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z',
    })

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByTestId('levels-admin')).toBeInTheDocument()
    })

    await user.click(screen.getByText('+ Add Level'))
    fireEvent.change(screen.getByPlaceholderText(/Level name/), { target: { value: 'Oliver' } })
    await user.click(screen.getByText('Add'))
    await waitFor(() => {
      expect(mockCreateLevel).toHaveBeenCalledWith('Oliver', expect.any(Function))
    })
    expect(screen.getByText('Oliver')).toBeInTheDocument()
  })

  it('refuses to submit a duplicate name client-side without calling the API', async () => {
    const user = userEvent.setup()
    mockListLevels.mockResolvedValueOnce({
      levels: [{ id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z' }],
    })

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByTestId('levels-admin')).toBeInTheDocument()
    })

    await user.click(screen.getByText('+ Add Level'))
    fireEvent.change(screen.getByPlaceholderText(/Level name/), { target: { value: 'Marcia' } })
    await user.click(screen.getByText('Add'))

    await waitFor(() => {
      expect(screen.getByText(/already exists/)).toBeInTheDocument()
    })
    expect(mockCreateLevel).not.toHaveBeenCalled()
  })

  it('renames a Level on Enter', async () => {
    const user = userEvent.setup()
    mockListLevels.mockResolvedValueOnce({
      levels: [{ id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z' }],
    })
    mockRenameLevel.mockResolvedValueOnce({
      id: 1, groupId: 'org_a', name: 'Marcia Renamed', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z',
    })

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByText('Marcia')).toBeInTheDocument()
    })

    await user.click(screen.getByLabelText('Rename Marcia'))
    const input = screen.getByDisplayValue('Marcia')
    fireEvent.change(input, { target: { value: 'Marcia Renamed' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => {
      expect(mockRenameLevel).toHaveBeenCalledWith(1, 'Marcia Renamed', expect.any(Function))
    })
  })

  it('saves Report Instructions on blur after expanding a Level', async () => {
    const user = userEvent.setup()
    mockListLevels.mockResolvedValueOnce({
      levels: [{ id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z' }],
    })
    mockUpdateInstructions.mockResolvedValueOnce({
      id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: 'Focus on fluency.', createdAt: '2026-01-01T00:00:00Z',
    })

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByText('Marcia')).toBeInTheDocument()
    })

    await user.click(screen.getByText('Marcia'))
    const textarea = await screen.findByPlaceholderText(/how report cards should read/)
    fireEvent.change(textarea, { target: { value: 'Focus on fluency.' } })
    fireEvent.blur(textarea)

    await waitFor(() => {
      expect(mockUpdateInstructions).toHaveBeenCalledWith(1, 'Focus on fluency.', expect.any(Function))
    })
  })

  it('shows a hint when Report Instructions are empty', async () => {
    const user = userEvent.setup()
    mockListLevels.mockResolvedValueOnce({
      levels: [{ id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z' }],
    })

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByText('Marcia')).toBeInTheDocument()
    })

    await user.click(screen.getByText('Marcia'))

    await waitFor(() => {
      expect(screen.getByText(/reports for this Level can't be generated/)).toBeInTheDocument()
    })
  })

  it('deletes a Level after confirming', async () => {
    const user = userEvent.setup()
    mockListLevels.mockResolvedValueOnce({
      levels: [{ id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z' }],
    })
    mockDeleteLevel.mockResolvedValueOnce(undefined)

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByText('Marcia')).toBeInTheDocument()
    })

    await user.click(screen.getByLabelText('Delete Marcia'))
    await user.click(screen.getByText('Delete'))

    await waitFor(() => {
      expect(mockDeleteLevel).toHaveBeenCalledWith(1, expect.any(Function))
    })
    await waitFor(() => {
      expect(screen.queryByText('Marcia')).not.toBeInTheDocument()
    })
  })
})
