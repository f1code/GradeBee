vi.unmock('motion/react')

import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listClasses, listLevels } from '../../api'

const stableGetToken = vi.fn().mockResolvedValue('tok')

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: stableGetToken }),
}))

vi.mock('../../hooks/useMediaQuery', () => ({
  useMediaQuery: () => false,
}))

vi.mock('../../api', () => ({
  listClasses: vi.fn(),
  listStudents: vi.fn(),
  createClass: vi.fn(),
  renameClass: vi.fn(),
  deleteClass: vi.fn(),
  createStudent: vi.fn(),
  renameStudent: vi.fn(),
  deleteStudent: vi.fn(),
  listLevels: vi.fn(),
  WEEKDAYS: ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'],
}))

const mockListClasses = listClasses as ReturnType<typeof vi.fn>
const mockListLevels = listLevels as ReturnType<typeof vi.fn>

import StudentList from '../StudentList'

describe('StudentList empty-slot presence', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListClasses.mockResolvedValue({ classes: [] })
    mockListLevels.mockResolvedValue({ levels: [] })
  })

  it('does not show No Classes Yet under the exiting No Levels yet card', async () => {
    const user = userEvent.setup()
    render(<StudentList />)

    await waitFor(() => {
      expect(screen.getByTestId('student-list-empty')).toBeInTheDocument()
    })
    await user.click(screen.getByTestId('add-class-btn'))
    await waitFor(() => {
      expect(screen.getByTestId('add-class-no-levels')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByTestId('add-class-cancel'))

    expect(screen.getByRole('heading', { name: 'No Levels yet' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'No Classes Yet' })).not.toBeInTheDocument()

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'No Classes Yet' })).toBeInTheDocument()
    })
    expect(screen.queryByRole('heading', { name: 'No Levels yet' })).not.toBeInTheDocument()
  })
})
