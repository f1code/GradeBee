import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createClass, listLevels } from '../../api'

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../api', () => ({
  createClass: vi.fn(),
  listLevels: vi.fn(),
  WEEKDAYS: ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'],
}))

const mockCreateClass = createClass as ReturnType<typeof vi.fn>
const mockListLevels = listLevels as ReturnType<typeof vi.fn>

import AddClassForm from '../AddClassForm'

describe('AddClassForm', () => {
  const onCreated = vi.fn()
  const onCancel = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    mockListLevels.mockResolvedValue({ levels: [{ id: 1, name: 'Math', groupId: 'g1', reportInstructions: '', createdAt: '' }] })
  })

  it('renders select and buttons once levels load', async () => {
    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)
    await waitFor(() => {
      expect(screen.getByTestId('add-class-level-select')).toBeInTheDocument()
    })
    expect(screen.getByTestId('add-class-submit')).toBeInTheDocument()
    expect(screen.getByTestId('add-class-cancel')).toBeInTheDocument()
  })

  it('hides Cancel button when onCancel is not provided', async () => {
    render(<AddClassForm onCreated={onCreated} />)
    await waitFor(() => {
      expect(screen.getByTestId('add-class-level-select')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('add-class-cancel')).not.toBeInTheDocument()
  })

  it('disables submit until a level and day are chosen', async () => {
    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)
    await waitFor(() => {
      expect(screen.getByTestId('add-class-level-select')).toBeInTheDocument()
    })
    expect(screen.getByTestId('add-class-submit')).toBeDisabled()
    fireEvent.change(screen.getByTestId('add-class-level-select'), { target: { value: '1' } })
    expect(screen.getByTestId('add-class-submit')).toBeDisabled()
    fireEvent.change(screen.getByTestId('add-class-day-select'), { target: { value: 'Wednesday' } })
    expect(screen.getByTestId('add-class-submit')).not.toBeDisabled()
  })

  it('shows an ask-admin message when there are no levels', async () => {
    mockListLevels.mockResolvedValue({ levels: [] })
    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)
    await waitFor(() => {
      expect(screen.getByTestId('add-class-no-levels')).toBeInTheDocument()
    })
  })

  it('calls createClass and fires onCreated on submit', async () => {
    const cls = { id: 1, name: 'Math · Wed', studentCount: 0 }
    mockCreateClass.mockResolvedValueOnce(cls)

    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)

    await waitFor(() => {
      expect(screen.getByTestId('add-class-level-select')).toBeInTheDocument()
    })
    fireEvent.change(screen.getByTestId('add-class-level-select'), { target: { value: '1' } })
    fireEvent.change(screen.getByTestId('add-class-day-select'), { target: { value: 'Wednesday' } })
    fireEvent.click(screen.getByTestId('add-class-submit'))

    await waitFor(() => {
      expect(mockCreateClass).toHaveBeenCalledWith(1, 'Wednesday', '', expect.any(Function))
    })
    expect(onCreated).toHaveBeenCalledWith(cls)
  })

  it('shows error on API failure', async () => {
    mockCreateClass.mockRejectedValueOnce(new Error('Server error'))

    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)

    await waitFor(() => {
      expect(screen.getByTestId('add-class-level-select')).toBeInTheDocument()
    })
    fireEvent.change(screen.getByTestId('add-class-level-select'), { target: { value: '1' } })
    fireEvent.change(screen.getByTestId('add-class-day-select'), { target: { value: 'Wednesday' } })
    fireEvent.click(screen.getByTestId('add-class-submit'))

    await waitFor(() => {
      expect(screen.getByTestId('add-class-error')).toHaveTextContent('Server error')
    })
    expect(onCreated).not.toHaveBeenCalled()
  })

  it('calls onCancel on Escape key', async () => {
    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)

    await waitFor(() => {
      expect(screen.getByTestId('add-class-level-select')).toBeInTheDocument()
    })
    fireEvent.keyDown(screen.getByTestId('add-class-level-select'), { key: 'Escape' })

    expect(onCancel).toHaveBeenCalled()
  })
})
