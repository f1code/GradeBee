import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createStudent } from '../../api'

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../api', () => ({
  createStudent: vi.fn(),
}))

const mockCreateStudent = createStudent as ReturnType<typeof vi.fn>

import AddStudentForm from '../AddStudentForm'

describe('AddStudentForm', () => {
  const onCreated = vi.fn()
  const classId = 5

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders input and submit button', () => {
    render(<AddStudentForm classId={classId} onCreated={onCreated} />)
    expect(screen.getByTestId('add-student-input')).toBeInTheDocument()
    expect(screen.getByTestId('add-student-submit')).toBeInTheDocument()
  })

  it('disables submit when input is empty', () => {
    render(<AddStudentForm classId={classId} onCreated={onCreated} />)
    expect(screen.getByTestId('add-student-submit')).toBeDisabled()
  })

  it('calls createStudent and fires onCreated on submit', async () => {
    const student = { id: 10, name: 'Alice', classId: 5 }
    mockCreateStudent.mockResolvedValueOnce(student)

    render(<AddStudentForm classId={classId} onCreated={onCreated} />)

    await userEvent.type(screen.getByTestId('add-student-input'), 'Alice')
    await userEvent.click(screen.getByTestId('add-student-submit'))

    await waitFor(() => {
      expect(mockCreateStudent).toHaveBeenCalledWith(5, 'Alice', expect.any(Function))
    })
    expect(onCreated).toHaveBeenCalledWith(student)
  })

  it('clears input on success', async () => {
    const student = { id: 10, name: 'Alice', classId: 5 }
    mockCreateStudent.mockResolvedValueOnce(student)

    render(<AddStudentForm classId={classId} onCreated={onCreated} />)

    const input = screen.getByTestId('add-student-input') as HTMLInputElement
    await userEvent.type(input, 'Alice')
    await userEvent.click(screen.getByTestId('add-student-submit'))

    await waitFor(() => {
      expect(input.value).toBe('')
    })
  })

  it('shows error on failure', async () => {
    mockCreateStudent.mockRejectedValueOnce(new Error('Duplicate name'))

    render(<AddStudentForm classId={classId} onCreated={onCreated} />)

    await userEvent.type(screen.getByTestId('add-student-input'), 'Alice')
    await userEvent.click(screen.getByTestId('add-student-submit'))

    await waitFor(() => {
      expect(screen.getByTestId('add-student-error')).toHaveTextContent('Duplicate name')
    })
    expect(onCreated).not.toHaveBeenCalled()
  })
})
