import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listClasses, createClass, listLevels } from '../../api'

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
const mockCreateClass = createClass as ReturnType<typeof vi.fn>
const mockListLevels = listLevels as ReturnType<typeof vi.fn>

import StudentList from '../StudentList'

describe('StudentList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListLevels.mockResolvedValue({ levels: [{ id: 1, name: 'Science', groupId: 'g1', reportInstructions: '', createdAt: '' }] })
  })

  it('shows loading state initially', () => {
    mockListClasses.mockReturnValue(new Promise(() => {}))
    render(<StudentList />)
    expect(screen.getByTestId('student-list-loading')).toBeInTheDocument()
  })

  it('renders class groups after fetch', async () => {
    mockListClasses.mockResolvedValueOnce({
      classes: [
        { id: 1, name: 'Math 101 · Wed', levelId: 1, levelName: "Math 101", day: "Wednesday", timeSlot: "", studentCount: 2 },
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
    expect(screen.getByRole('heading', { name: 'Your Classes' })).toBeInTheDocument()
    expect(screen.getByTestId('add-class-btn')).toBeInTheDocument()
    expect(screen.getByText('No Classes Yet')).toBeInTheDocument()
    expect(screen.queryByTestId('add-class-level-select')).not.toBeInTheDocument()
  })

  it('opens the add class form from the empty state header', async () => {
    const user = userEvent.setup()
    mockListClasses.mockResolvedValueOnce({
      classes: [],
    })

    render(<StudentList />)

    await waitFor(() => {
      expect(screen.getByTestId('student-list-empty')).toBeInTheDocument()
    })
    await user.click(screen.getByTestId('add-class-btn'))
    await waitFor(() => {
      expect(screen.getByTestId('add-class-level-select')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('student-list-empty')).not.toBeInTheDocument()
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
      classes: [{ id: 1, name: 'Math 101 · Wed', levelId: 2, levelName: "Math 101", day: "Wednesday", timeSlot: "", studentCount: 2 }],
    })
    mockCreateClass.mockResolvedValueOnce({ id: 5, name: 'Science · Mon', levelId: 1, levelName: "Science", day: "Monday", timeSlot: "", studentCount: 0 })

    render(<StudentList />)

    await waitFor(() => {
      expect(screen.getByTestId('student-list')).toBeInTheDocument()
    })

    await user.click(screen.getByTestId('add-class-btn'))
    await waitFor(() => {
      expect(screen.getByTestId('add-class-level-select')).toBeInTheDocument()
    })
    await user.selectOptions(screen.getByTestId('add-class-level-select'), '1')
    await user.selectOptions(screen.getByTestId('add-class-day-select'), 'Monday')
    await user.click(screen.getByTestId('add-class-submit'))

    await waitFor(() => {
      expect(screen.getByTestId('class-group-5')).toBeInTheDocument()
    })

    // The new class should be expanded with add-student input visible
    expect(screen.getByTestId('add-student-input')).toBeInTheDocument()
  })
})
