import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  listLevels, createLevel, renameLevel, updateLevelReportInstructions, deleteLevel, createTerm, updateTerm, deleteTerm, rolloverTerms,
} from '../../api'

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
  createTerm: vi.fn(),
  updateTerm: vi.fn(),
  deleteTerm: vi.fn(),
  rolloverTerms: vi.fn(),
}))

const mockListLevels = listLevels as ReturnType<typeof vi.fn>
const mockCreateLevel = createLevel as ReturnType<typeof vi.fn>
const mockRenameLevel = renameLevel as ReturnType<typeof vi.fn>
const mockUpdateInstructions = updateLevelReportInstructions as ReturnType<typeof vi.fn>
const mockDeleteLevel = deleteLevel as ReturnType<typeof vi.fn>
const mockCreateTerm = createTerm as ReturnType<typeof vi.fn>
const mockUpdateTerm = updateTerm as ReturnType<typeof vi.fn>
const mockDeleteTerm = deleteTerm as ReturnType<typeof vi.fn>
const mockRollover = rolloverTerms as ReturnType<typeof vi.fn>

const term1 = { id: 1, groupId: 'org_a', name: 'Term 1', startDate: '2026-08-15', endDate: '2026-11-01' }
const term2 = { id: 2, groupId: 'org_a', name: 'Term 2', startDate: '2026-11-02', endDate: '2027-01-31' }

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
      term: term1,
      terms: [term1],
    })
    mockUpdateInstructions.mockResolvedValueOnce({
      id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: 'Focus on fluency.', createdAt: '2026-01-01T00:00:00Z',
    })

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByText('Marcia')).toBeInTheDocument()
    })

    await user.click(screen.getByText('Marcia'))
    const textarea = await screen.findByLabelText('Report Instructions · Term 1')
    fireEvent.change(textarea, { target: { value: 'Focus on fluency.' } })
    fireEvent.blur(textarea)

    await waitFor(() => {
      expect(mockUpdateInstructions).toHaveBeenCalledWith(1, 'Focus on fluency.', expect.any(Function), 1)
    })
  })

  it('shows a hint when Report Instructions are empty', async () => {
    const user = userEvent.setup()
    mockListLevels.mockResolvedValueOnce({
      levels: [{ id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z' }],
      term: term1,
      terms: [term1],
    })

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByText('Marcia')).toBeInTheDocument()
    })

    await user.click(screen.getByText('Marcia'))

    await waitFor(() => {
      expect(screen.getByText(
        "No Term 1 instructions yet — Term 1 reports for this Level can't be generated until an Admin adds them."
      )).toBeInTheDocument()
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

  it('shows a countable refusal message when the Level is still in use', async () => {
    const user = userEvent.setup()
    mockListLevels.mockResolvedValueOnce({
      levels: [{ id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: '', createdAt: '2026-01-01T00:00:00Z' }],
    })
    mockDeleteLevel.mockRejectedValueOnce(new Error('3 classes use this Level — move them to another Level first'))

    render(<LevelsAdmin />)

    await waitFor(() => {
      expect(screen.getByText('Marcia')).toBeInTheDocument()
    })

    await user.click(screen.getByLabelText('Delete Marcia'))
    await user.click(screen.getByText('Delete'))

    await waitFor(() => {
      expect(screen.getByText(/3 classes use this Level/)).toBeInTheDocument()
    })
    // Refused delete must leave the Level in the list.
    expect(screen.getByText('Marcia')).toBeInTheDocument()
  })

  describe('Terms', () => {
    const marcia = {
      id: 1, groupId: 'org_a', name: 'Marcia', reportInstructions: 'T1 text', createdAt: '2026-01-01T00:00:00Z',
      instructionsByTerm: { 1: 'T1 text' },
    }

    async function renderWithTerms() {
      mockListLevels.mockResolvedValueOnce({ levels: [marcia], term: term1, terms: [term1, term2] })
      render(<LevelsAdmin />)
      await screen.findByText('Marcia')
    }

    it('starts a new school year after confirming, then reloads', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      mockRollover.mockResolvedValueOnce(undefined)
      const copy1 = { ...term1, id: 3, startDate: '2027-08-15', endDate: '2027-11-01' }
      mockListLevels.mockResolvedValueOnce({
        levels: [{ ...marcia, instructionsByTerm: { 3: 'T1 text' } }], term: null, terms: [copy1],
      })

      await user.click(screen.getByRole('button', { name: 'Start new school year' }))
      expect(screen.getByRole('button', { name: 'Start new year' })).toHaveAccessibleDescription(/Term 1 and Term 2 become read-only\./)
      await user.click(screen.getByRole('button', { name: 'Start new year' }))

      expect(mockRollover).toHaveBeenCalledOnce()
      expect(await screen.findByRole('button', { name: /^Term 1/ })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /^Term 2/ })).not.toBeInTheDocument()
      expect(screen.queryByText('⚠ No Term 1 text')).not.toBeInTheDocument()
    })

    it('keeps the confirm open on refusal, and Cancel calls nothing', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      mockRollover.mockRejectedValueOnce(new Error('No active terms'))

      await user.click(screen.getByRole('button', { name: 'Start new school year' }))
      expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
      await user.click(screen.getByRole('button', { name: 'Start new year' }))
      expect(await screen.findByText('No active terms')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Start new year' })).toBeEnabled()

      await user.click(screen.getByRole('button', { name: 'Cancel' }))
      expect(screen.getByRole('button', { name: 'Start new school year' })).toHaveFocus()
      expect(mockRollover).toHaveBeenCalledOnce()
    })

    it('hides Start new school year while the Group has no Terms', async () => {
      mockListLevels.mockResolvedValueOnce({ levels: [marcia], term: null, terms: [] })
      render(<LevelsAdmin />)
      await screen.findByText('Marcia')
      expect(screen.queryByRole('button', { name: 'Start new school year' })).not.toBeInTheDocument()
    })

    it('spells out Add term while the Group has no Terms', async () => {
      mockListLevels.mockResolvedValueOnce({ levels: [marcia], term: null, terms: [] })
      render(<LevelsAdmin />)
      expect(await screen.findByText('+ Add term')).toBeInTheDocument()
      expect(screen.queryByLabelText('Add term')).not.toBeInTheDocument()
    })

    it('switching Terms swaps the instructions box and flags missing text', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      await user.click(screen.getByText('Marcia'))
      expect(await screen.findByLabelText('Report Instructions · Term 1')).toHaveValue('T1 text')
      expect(screen.queryByText('⚠ No Term 1 text')).not.toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: /^Term 2/ }))
      expect(await screen.findByLabelText('Report Instructions · Term 2')).toHaveValue('')
      expect(screen.getByText('⚠ No Term 2 text')).toBeInTheDocument()
      expect(screen.getByText('Report Instructions below are for Term 2.')).toBeInTheDocument()
    })

    it('saves text to the Term it was typed under', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      mockUpdateInstructions.mockResolvedValueOnce(marcia)
      await user.click(screen.getByRole('button', { name: /^Term 2/ }))
      await user.click(screen.getByText('Marcia'))
      const box = await screen.findByLabelText('Report Instructions · Term 2')
      fireEvent.change(box, { target: { value: 'T2 text' } })
      fireEvent.blur(box)
      await waitFor(() => {
        expect(mockUpdateInstructions).toHaveBeenCalledWith(1, 'T2 text', expect.any(Function), 2)
      })
      await waitFor(() => expect(screen.queryByText('⚠ No Term 2 text')).not.toBeInTheDocument())
    })

    it('adds a Term inline and picks it', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      const term3 = { id: 3, groupId: 'org_a', name: 'Term 3', startDate: '2027-02-01', endDate: '2027-04-30' }
      mockCreateTerm.mockResolvedValueOnce(term3)

      await user.click(screen.getByLabelText('Add term'))
      await user.type(screen.getByLabelText('Name'), 'Term 3')
      fireEvent.change(screen.getByLabelText('From'), { target: { value: '2027-02-01' } })
      fireEvent.change(screen.getByLabelText('To'), { target: { value: '2027-04-30' } })
      await user.click(screen.getByText('Add'))

      expect(mockCreateTerm).toHaveBeenCalledWith(
        { name: 'Term 3', startDate: '2027-02-01', endDate: '2027-04-30' }, expect.any(Function))
      expect(await screen.findByText('⚠ No Term 3 text')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /^Term 3/ })).toHaveAttribute('aria-pressed', 'true')
    })

    it('shows the picked Term read-only until Edit', async () => {
      await renderWithTerms()
      expect(screen.getByText('Aug 15, 2026 – Nov 1, 2026')).toBeInTheDocument()
      expect(screen.getByText('Report Instructions below are for Term 1.')).toBeInTheDocument()
      expect(screen.queryByLabelText('Name')).not.toBeInTheDocument()
    })

    it('saves Term edits and shows an overlap refusal', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      mockUpdateTerm.mockRejectedValueOnce(new Error('Overlaps Term 2'))
      await user.click(screen.getByLabelText('Edit Term 1'))
      fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-12-01' } })
      await user.click(screen.getByText('Save'))
      expect(mockUpdateTerm).toHaveBeenCalledWith(
        1, { name: 'Term 1', startDate: '2026-08-15', endDate: '2026-12-01' }, expect.any(Function))
      expect(await screen.findByText('Overlaps Term 2')).toBeInTheDocument()
    })

    it('renames a Term and updates its chip', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      mockUpdateTerm.mockResolvedValueOnce({ ...term1, name: 'Autumn' })
      await user.click(screen.getByLabelText('Edit Term 1'))
      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Autumn' } })
      await user.click(screen.getByText('Save'))
      expect(await screen.findByRole('button', { name: /^Autumn/ })).toHaveAttribute('aria-pressed', 'true')
      expect(screen.queryByRole('button', { name: /^Term 1/ })).not.toBeInTheDocument()
    })

    it('deletes a Term and picks the latest started one left', async () => {
      vi.useFakeTimers({ toFake: ['Date'] })
      vi.setSystemTime(new Date('2026-12-01T12:00:00Z'))
      try {
        const user = userEvent.setup()
        const term0 = { id: 9, groupId: 'org_a', name: 'Term 0', startDate: '2026-01-01', endDate: '2026-06-30' }
        const term3 = { id: 3, groupId: 'org_a', name: 'Term 3', startDate: '2027-02-01', endDate: '2027-04-30' }
        mockListLevels.mockResolvedValueOnce({ levels: [marcia], term: term2, terms: [term0, term1, term2, term3] })
        mockDeleteTerm.mockResolvedValueOnce(undefined)
        render(<LevelsAdmin />)
        await user.click(await screen.findByLabelText('Delete Term 2'))
        expect(await screen.findByRole('button', { name: /^Term 1/ })).toHaveAttribute('aria-pressed', 'true')
        expect(screen.queryByRole('button', { name: /^Term 2/ })).not.toBeInTheDocument()
      } finally {
        vi.useRealTimers()
      }
    })

    it('deleting another Term keeps the picked one', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      mockDeleteTerm.mockResolvedValueOnce(undefined)
      await user.click(screen.getByLabelText('Delete Term 2'))
      await waitFor(() => expect(screen.queryByRole('button', { name: /^Term 2/ })).not.toBeInTheDocument())
      expect(screen.getByRole('button', { name: /^Term 1/ })).toHaveAttribute('aria-pressed', 'true')
    })

    it('shows a refused delete while the edit form is open', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      mockDeleteTerm.mockRejectedValueOnce(new Error('Term in use'))
      await user.click(screen.getByLabelText('Edit Term 1'))
      await user.click(screen.getByLabelText('Delete Term 1'))
      expect(await screen.findByRole('alert')).toHaveTextContent('Term 1 Term in use')
      expect(screen.queryByLabelText('Name')).not.toBeInTheDocument()
    })

    it('keeps a Term in use and says so', async () => {
      const user = userEvent.setup()
      await renderWithTerms()
      mockDeleteTerm.mockRejectedValueOnce(new Error('Term in use'))
      await user.click(screen.getByLabelText('Delete Term 1'))
      expect(await screen.findByRole('alert')).toHaveTextContent('Term 1 Term in use')
      expect(screen.getByRole('button', { name: /^Term 1/ })).toBeInTheDocument()
    })
  })
})
