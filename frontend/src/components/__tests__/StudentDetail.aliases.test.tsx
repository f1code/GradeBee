vi.unmock('motion/react')

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listNotes, listAliases, addAlias, removeAlias } from '../../api'
import type * as ApiModule from '../../api'

const stableGetToken = vi.fn().mockResolvedValue('tok')

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: stableGetToken }),
}))

vi.mock('../../api', async () => {
  const actual = await vi.importActual<typeof ApiModule>('../../api')
  return {
    ...actual,
    listNotes: vi.fn(),
    listAliases: vi.fn(),
    addAlias: vi.fn(),
    removeAlias: vi.fn(),
  }
})

const mockListNotes = listNotes as ReturnType<typeof vi.fn>
const mockListAliases = listAliases as ReturnType<typeof vi.fn>
const mockAddAlias = addAlias as ReturnType<typeof vi.fn>
const mockRemoveAlias = removeAlias as ReturnType<typeof vi.fn>

import StudentDetail from '../StudentDetail'

const malia = { id: 7, studentId: 42, alias: 'Malia', createdAt: '' }

function renderDetail() {
  return render(
    <StudentDetail studentId={42} studentName="Amalia" className="Pam & Paul · Wed" onCollapse={() => {}} modal />,
  )
}

describe('StudentDetail alias chips', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListNotes.mockResolvedValue({ notes: [] })
    mockListAliases.mockResolvedValue({ aliases: [malia] })
  })

  // The detail panel renders before its fetches resolve, so the alias list
  // always arrives after StudentAliases has mounted.
  it('shows aliases that arrive after the first render', async () => {
    renderDetail()

    expect(screen.queryByText('Malia')).not.toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByText('Malia')).toBeInTheDocument()
    })
  })

  it('drops a chip once its alias is removed', async () => {
    mockRemoveAlias.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderDetail()

    await waitFor(() => expect(screen.getByText('Malia')).toBeInTheDocument())
    await user.click(screen.getByLabelText('Remove alias Malia'))

    expect(mockRemoveAlias).toHaveBeenCalledWith(42, 7, expect.anything())
    await waitFor(() => expect(screen.queryByText('Malia')).not.toBeInTheDocument())
  })

  it('appends a newly added alias to the loaded ones', async () => {
    mockAddAlias.mockResolvedValue({ id: 8, studentId: 42, alias: 'Mali', createdAt: '' })
    const user = userEvent.setup()
    renderDetail()

    await waitFor(() => expect(screen.getByText('Malia')).toBeInTheDocument())
    await user.click(screen.getByLabelText('Add alias'))
    await user.type(screen.getByPlaceholderText('e.g. Alex'), 'Mali')
    await user.click(screen.getByLabelText('Confirm alias'))

    await waitFor(() => expect(screen.getByText('Mali')).toBeInTheDocument())
    expect(screen.getByText('Malia')).toBeInTheDocument()
  })
})
