import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { listClasses, moveStudent, MoveConflictError } from '../../api'
import type * as ApiModule from '../../api'

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../api', async () => {
  const actual = await vi.importActual<typeof ApiModule>('../../api')
  return {
    ...actual,
    listClasses: vi.fn(),
    moveStudent: vi.fn(),
  }
})

const mockListClasses = listClasses as ReturnType<typeof vi.fn>
const mockMoveStudent = moveStudent as ReturnType<typeof vi.fn>

import MoveStudentModal, { type MoveStudentModalProps } from '../MoveStudentModal'

const classes = [
  { id: 1, userId: 'u', name: 'Algebra I · Mon', levelId: 10, levelName: 'Algebra I', day: 'Monday', timeSlot: '', position: 0, createdAt: '', studentCount: 3 },
  { id: 2, userId: 'u', name: 'Algebra I · Wed', levelId: 10, levelName: 'Algebra I', day: 'Wednesday', timeSlot: '', position: 1, createdAt: '', studentCount: 2 },
  { id: 3, userId: 'u', name: 'Geometry · Tue', levelId: 20, levelName: 'Geometry', day: 'Tuesday', timeSlot: '', position: 2, createdAt: '', studentCount: 1 },
]

function baseProps(overrides: Partial<MoveStudentModalProps> = {}): MoveStudentModalProps {
  return {
    studentId: 42,
    studentName: 'Alexander',
    currentClassId: 1,
    currentLevelId: 10,
    onClose: vi.fn(),
    onMoved: vi.fn(),
    ...overrides,
  }
}

describe('MoveStudentModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListClasses.mockResolvedValue({ classes })
  })

  it('groups target classes by Level and excludes the current class', async () => {
    render(<MoveStudentModal {...baseProps()} />)

    await waitFor(() => {
      expect(screen.getByText('Geometry')).toBeInTheDocument()
    })
    // Current class (Algebra I · Mon, id 1) is excluded; the other Algebra I class remains.
    expect(screen.queryByTestId('move-student-target-1')).not.toBeInTheDocument()
    expect(screen.getByTestId('move-student-target-2')).toBeInTheDocument()
    expect(screen.getByTestId('move-student-target-3')).toBeInTheDocument()
    // Level prefix stripped from the row label.
    expect(screen.getByTestId('move-student-target-2')).toHaveTextContent('Wed')
    expect(screen.getByTestId('move-student-target-2')).not.toHaveTextContent('Algebra I ·')
  })

  it('moves directly to a same-Level class without a confirm step', async () => {
    mockMoveStudent.mockResolvedValueOnce({ droppedAliases: [] })
    const onMoved = vi.fn()
    render(<MoveStudentModal {...baseProps({ onMoved })} />)

    await waitFor(() => screen.getByTestId('move-student-target-2'))
    await userEvent.click(screen.getByTestId('move-student-target-2'))

    await waitFor(() => {
      expect(mockMoveStudent).toHaveBeenCalledWith(42, 2, expect.any(Function))
    })
    expect(screen.queryByTestId('move-student-confirm')).not.toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByTestId('move-student-result')).toBeInTheDocument()
    })
    expect(onMoved).toHaveBeenCalledWith({ classId: 2, className: 'Algebra I · Wed', levelId: 10, levelName: 'Algebra I', droppedAliases: [] })
  })

  it('requires an explicit confirm with a Report Instructions warning for a cross-Level move', async () => {
    render(<MoveStudentModal {...baseProps()} />)

    await waitFor(() => screen.getByTestId('move-student-target-3'))
    await userEvent.click(screen.getByTestId('move-student-target-3'))

    expect(screen.getByTestId('move-student-confirm')).toBeInTheDocument()
    expect(screen.getByText(/Report Instructions/)).toBeInTheDocument()
    expect(mockMoveStudent).not.toHaveBeenCalled()

    mockMoveStudent.mockResolvedValueOnce({ droppedAliases: [] })
    await userEvent.click(screen.getByTestId('move-student-confirm-btn'))

    await waitFor(() => {
      expect(mockMoveStudent).toHaveBeenCalledWith(42, 3, expect.any(Function))
    })
  })

  it('shows a 409 conflict naming the colliding student and does not move', async () => {
    mockMoveStudent.mockRejectedValueOnce(new MoveConflictError('A student named "Alexander" already exists in the target class.', 'Alexander'))
    render(<MoveStudentModal {...baseProps()} />)

    await waitFor(() => screen.getByTestId('move-student-target-2'))
    await userEvent.click(screen.getByTestId('move-student-target-2'))

    await waitFor(() => {
      expect(screen.getByText('Alexander')).toBeInTheDocument()
    })
    expect(screen.getByText(/already has a student with this name/)).toBeInTheDocument()
    expect(screen.queryByTestId('move-student-result')).not.toBeInTheDocument()
  })

  it('lists dropped aliases in the result step but still succeeds', async () => {
    mockMoveStudent.mockResolvedValueOnce({ droppedAliases: ['Em', 'Emmy'] })
    render(<MoveStudentModal {...baseProps()} />)

    await waitFor(() => screen.getByTestId('move-student-target-2'))
    await userEvent.click(screen.getByTestId('move-student-target-2'))

    await waitFor(() => {
      expect(screen.getByTestId('move-student-result')).toBeInTheDocument()
    })
    expect(screen.getByText(/Em, Emmy/)).toBeInTheDocument()
  })

  it('dismisses on the close button', async () => {
    const onClose = vi.fn()
    render(<MoveStudentModal {...baseProps({ onClose })} />)
    await waitFor(() => screen.getByTestId('move-student-pick'))
    await userEvent.click(screen.getByLabelText('Close'))
    expect(onClose).toHaveBeenCalled()
  })

  it('dismisses on Escape', async () => {
    const onClose = vi.fn()
    render(<MoveStudentModal {...baseProps({ onClose })} />)
    await waitFor(() => screen.getByTestId('move-student-pick'))
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalled()
  })

  it('does not dismiss on overlay click', async () => {
    const onClose = vi.fn()
    render(<MoveStudentModal {...baseProps({ onClose })} />)
    await waitFor(() => screen.getByTestId('move-student-overlay'))
    await userEvent.click(screen.getByTestId('move-student-overlay'))
    expect(onClose).not.toHaveBeenCalled()
  })
})
