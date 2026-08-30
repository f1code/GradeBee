vi.unmock('motion/react')

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listClasses, listStudents, listLevels, listNotes, listAliases, addAlias, removeAlias } from '../../api'
import type * as ApiModule from '../../api'

const stableGetToken = vi.fn().mockResolvedValue('tok')

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: stableGetToken }),
}))

vi.mock('../../hooks/useMediaQuery', () => ({
  useMediaQuery: () => false,
}))

vi.mock('../../api', async () => {
  const actual = await vi.importActual<typeof ApiModule>('../../api')
  return {
    ...actual,
    listClasses: vi.fn(),
    listStudents: vi.fn(),
    listLevels: vi.fn(),
    listNotes: vi.fn(),
    listAliases: vi.fn(),
    addAlias: vi.fn(),
    removeAlias: vi.fn(),
  }
})

const mockListClasses = listClasses as ReturnType<typeof vi.fn>
const mockListStudents = listStudents as ReturnType<typeof vi.fn>
const mockListLevels = listLevels as ReturnType<typeof vi.fn>
const mockListNotes = listNotes as ReturnType<typeof vi.fn>
const mockListAliases = listAliases as ReturnType<typeof vi.fn>
const mockAddAlias = addAlias as ReturnType<typeof vi.fn>
const mockRemoveAlias = removeAlias as ReturnType<typeof vi.fn>

import StudentList from '../StudentList'

const pamWed = { id: 1, userId: 'u', name: 'Pam & Paul · Wed', levelId: 10, levelName: 'Pam & Paul', day: 'Wednesday', timeSlot: '16.30', position: 0, createdAt: '', studentCount: 1 }
const amalia = { id: 42, classId: 1, name: 'Amalia', createdAt: '', aliases: ['Malia'] }

describe('StudentList alias sync', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListClasses.mockResolvedValue({ classes: [pamWed] })
    mockListLevels.mockResolvedValue({ levels: [] })
    mockListStudents.mockResolvedValue({ students: [amalia] })
    mockListNotes.mockResolvedValue({ notes: [] })
    mockListAliases.mockResolvedValue({ aliases: [{ id: 7, studentId: 42, alias: 'Malia', createdAt: '' }] })
  })

  async function expandAmalia() {
    render(<StudentList />)
    await waitFor(() => screen.getByTestId('class-toggle-1'))
    await userEvent.click(screen.getByTestId('class-toggle-1'))
    await waitFor(() => screen.getByText('Amalia'))
    await userEvent.click(screen.getByText('Amalia'))
    await waitFor(() => screen.getByLabelText('Remove alias Malia'))
  }

  it('clears the collapsed AKA line when the alias is removed from the panel', async () => {
    mockRemoveAlias.mockResolvedValue(undefined)
    await expandAmalia()

    expect(screen.getByTestId('class-group-1')).toHaveTextContent('AKA')

    await userEvent.click(screen.getByLabelText('Remove alias Malia'))

    await waitFor(() => {
      expect(screen.getByTestId('class-group-1')).not.toHaveTextContent('AKA')
    })
  })

  it('keeps the AKA line while the panel is still loading its aliases', async () => {
    // The panel mounts with an empty list and fills it asynchronously; that
    // empty first render must not be reported to the roster as "no aliases".
    mockListAliases.mockReturnValue(new Promise(() => {}))
    render(<StudentList />)
    await waitFor(() => screen.getByTestId('class-toggle-1'))
    await userEvent.click(screen.getByTestId('class-toggle-1'))
    await waitFor(() => screen.getByText('Amalia'))
    await userEvent.click(screen.getByText('Amalia'))

    await waitFor(() => screen.getByLabelText('Add alias'))
    expect(screen.getByTestId('class-group-1')).toHaveTextContent('AKA')
    expect(screen.getByTestId('class-group-1')).toHaveTextContent('Malia')
  })

  it('keeps the AKA line when the alias fetch fails', async () => {
    mockListAliases.mockRejectedValue(new Error('boom'))
    render(<StudentList />)
    await waitFor(() => screen.getByTestId('class-toggle-1'))
    await userEvent.click(screen.getByTestId('class-toggle-1'))
    await waitFor(() => screen.getByText('Amalia'))
    await userEvent.click(screen.getByText('Amalia'))

    await waitFor(() => screen.getByLabelText('Add alias'))
    expect(screen.getByTestId('class-group-1')).toHaveTextContent('Malia')
  })

  it('adds a newly created alias to the AKA line', async () => {
    mockAddAlias.mockResolvedValue({ id: 8, studentId: 42, alias: 'Ama', createdAt: '' })
    await expandAmalia()

    await userEvent.click(screen.getByLabelText('Add alias'))
    await userEvent.type(screen.getByPlaceholderText('e.g. Alex'), 'Ama')
    await userEvent.click(screen.getByLabelText('Confirm alias'))

    await waitFor(() => {
      expect(screen.getByTestId('class-group-1')).toHaveTextContent('Ama, Malia')
    })
  })
})
