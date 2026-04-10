import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listClasses, createClass } from '../../api'

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
}))

const mockListClasses = listClasses as ReturnType<typeof vi.fn>
const mockCreateClass = createClass as ReturnType<typeof vi.fn>

import StudentList from '../StudentList'

describe('StudentList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows loading state initially', () => {
    mockListClasses.mockReturnValue(new Promise(() => {}))
    render(<StudentList />)
    expect(screen.getByTestId('student-list-loading')).toBeInTheDocument()
  })

  it('renders class groups after fetch', async () => {
    mockListClasses.mockResolvedValueOnce({
      classes: [
        { id: 1, name: 'Math 101', studentCount: 2 },
      ],
    })

    render(<StudentList />)

    await waitFor(() => {
      expect(screen.getByTestId('student-list')).toBeInTheDocument()
    })
    expect(screen.getByText('Math 101')).toBeInTheDocument()
    expect(screen.getByText('(2)')).toBeInTheDocument()
  })

  it('shows empty state when no classes', async () => {
    mockListClasses.mockResolvedValueOnce({
      classes: [],
    })

    render(<StudentList />)

    await waitFor(() => {
      expect(screen.getByTestId('student-list-empty')).toBeInTheDocument()
    })
    expect(screen.getByText('No Classes Yet')).toBeInTheDocument()
  })

  it('shows error state on fetch failure', async () => {
    mockListClasses.mockRejectedValueOnce(new Error('Network error'))

    render(<StudentList />)

    await waitFor(() => {
      expect(screen.getByTestId('student-list-error')).toBeInTheDocument()
    })
    expect(screen.getByText('Network error')).toBeInTheDocument()
  })

  it('expands newly created class and shows add-student form', async () => {
    const user = userEvent.setup()
    mockListClasses.mockResolvedValueOnce({
      classes: [{ id: 1, name: 'Math 101', studentCount: 2 }],
    })
    mockCreateClass.mockResolvedValueOnce({ id: 5, name: 'Science', studentCount: 0 })

    render(<StudentList />)

    await waitFor(() => {
      expect(screen.getByTestId('student-list')).toBeInTheDocument()
    })

    await user.click(screen.getByTestId('add-class-btn'))
    const input = screen.getByTestId('add-class-input')
    await user.type(input, 'Science')
    await user.click(screen.getByTestId('add-class-submit'))

    await waitFor(() => {
      expect(screen.getByTestId('class-group-5')).toBeInTheDocument()
    })

    // The new class should be expanded with add-student input visible
    expect(screen.getByTestId('add-student-input')).toBeInTheDocument()
  })
})
