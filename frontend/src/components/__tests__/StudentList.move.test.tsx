vi.unmock('motion/react')

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listClasses, listStudents, listLevels, listNotes, listAliases, moveStudent } from '../../api'
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
    moveStudent: vi.fn(),
    createClass: vi.fn(),
    renameClass: vi.fn(),
    deleteClass: vi.fn(),
    createStudent: vi.fn(),
    renameStudent: vi.fn(),
    deleteStudent: vi.fn(),
  }
})

const mockListClasses = listClasses as ReturnType<typeof vi.fn>
const mockListStudents = listStudents as ReturnType<typeof vi.fn>
const mockListLevels = listLevels as ReturnType<typeof vi.fn>
const mockListNotes = listNotes as ReturnType<typeof vi.fn>
const mockListAliases = listAliases as ReturnType<typeof vi.fn>
const mockMoveStudent = moveStudent as ReturnType<typeof vi.fn>

import StudentList from '../StudentList'
import StudentDetail from '../StudentDetail'

const mathMon = { id: 1, userId: 'u', name: 'Math · Mon', levelId: 10, levelName: 'Math', day: 'Monday', timeSlot: '', position: 0, createdAt: '', studentCount: 1 }
const mathWed = { id: 2, userId: 'u', name: 'Math · Wed', levelId: 10, levelName: 'Math', day: 'Wednesday', timeSlot: '', position: 1, createdAt: '', studentCount: 0 }
const alice = { id: 5, classId: 1, name: 'Alice', createdAt: '', aliases: [] }

describe('StudentList move-to-class wiring', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListClasses.mockResolvedValue({ classes: [mathMon, mathWed] })
    mockListLevels.mockResolvedValue({ levels: [] })
    mockListStudents.mockResolvedValue({ students: [alice] })
    mockListNotes.mockResolvedValue({ notes: [] })
    mockListAliases.mockResolvedValue({ aliases: [] })
  })

  async function expandToMoveTrigger() {
    render(<StudentList />)
    await waitFor(() => screen.getByTestId('class-toggle-1'))
    await userEvent.click(screen.getByTestId('class-toggle-1'))
    await userEvent.click(screen.getByText('Alice'))
    await waitFor(() => screen.getByTestId('move-student-5'))
    await userEvent.click(screen.getByTestId('move-student-5'))
    await waitFor(() => screen.getByTestId('move-student-target-2'))
  }

  it('updates source and target studentCount and removes the student from the source list after a successful move', async () => {
    mockMoveStudent.mockResolvedValueOnce({ droppedAliases: [] })
    await expandToMoveTrigger()

    await userEvent.click(screen.getByTestId('move-student-target-2'))

    await waitFor(() => {
      expect(mockMoveStudent).toHaveBeenCalledWith(5, 2, expect.any(Function))
    })
    await waitFor(() => {
      expect(screen.queryByTestId('student-5')).not.toBeInTheDocument()
    })
    // Source class count decremented, target incremented.
    expect(screen.getByTestId('class-group-1')).toHaveTextContent('(0)')
    expect(screen.getByTestId('class-group-2')).toHaveTextContent('(1)')
  })

  it('refetches the target class roster if it is already expanded when the move lands', async () => {
    mockListStudents.mockImplementation((classId: number) =>
      Promise.resolve({ students: classId === 1 ? [alice] : [] }))

    render(<StudentList />)
    await waitFor(() => screen.getByTestId('class-toggle-1'))
    await userEvent.click(screen.getByTestId('class-toggle-1'))
    await waitFor(() => screen.getByTestId('class-toggle-2'))
    await userEvent.click(screen.getByTestId('class-toggle-2'))
    await waitFor(() => screen.getByTestId('student-5'))
    await userEvent.click(screen.getByText('Alice'))
    await waitFor(() => screen.getByTestId('move-student-5'))
    await userEvent.click(screen.getByTestId('move-student-5'))
    await waitFor(() => screen.getByTestId('move-student-target-2'))

    mockMoveStudent.mockResolvedValueOnce({ droppedAliases: [] })
    // Once the move lands, the target class's next fetch should include Alice.
    mockListStudents.mockImplementation((classId: number) =>
      Promise.resolve({ students: classId === 2 ? [{ ...alice, classId: 2 }] : [] }))

    await userEvent.click(screen.getByTestId('move-student-target-2'))

    await waitFor(() => {
      expect(mockMoveStudent).toHaveBeenCalledWith(5, 2, expect.any(Function))
    })
    // Alice appears under the already-expanded target class without a manual re-expand.
    await waitFor(() => {
      expect(screen.getByTestId('class-group-2')).toHaveTextContent('Alice')
    })
  })

  it('does not show the move trigger inside the JobStatus note-link modal', async () => {
    // StudentDetail itself renders no trigger when onRequestMove is omitted
    // (the JobStatus usage never passes it) — verified at the component
    // level since JobStatus has its own heavy mocking surface.
    render(<StudentDetail studentId={5} studentName="Alice" className="Math · Mon" onCollapse={() => {}} modal />)
    await waitFor(() => {
      expect(mockListNotes).toHaveBeenCalled()
    })
    expect(screen.queryByTestId('move-student-5')).not.toBeInTheDocument()
  })
})
