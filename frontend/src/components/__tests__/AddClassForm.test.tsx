import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createClass } from '../../api'

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../api', () => ({
  createClass: vi.fn(),
}))

const mockCreateClass = createClass as ReturnType<typeof vi.fn>

import AddClassForm from '../AddClassForm'

describe('AddClassForm', () => {
  const onCreated = vi.fn()
  const onCancel = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders input and buttons', () => {
    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)
    expect(screen.getByTestId('add-class-input')).toBeInTheDocument()
    expect(screen.getByTestId('add-class-submit')).toBeInTheDocument()
    expect(screen.getByTestId('add-class-cancel')).toBeInTheDocument()
  })

  it('hides Cancel button when onCancel is not provided', () => {
    render(<AddClassForm onCreated={onCreated} />)
    expect(screen.queryByTestId('add-class-cancel')).not.toBeInTheDocument()
  })

  it('disables submit when input is empty', () => {
    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)
    expect(screen.getByTestId('add-class-submit')).toBeDisabled()
  })

  it('calls createClass and fires onCreated on submit', async () => {
    const cls = { id: 1, name: 'Math', studentCount: 0 }
    mockCreateClass.mockResolvedValueOnce(cls)

    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)

    fireEvent.change(screen.getByTestId('add-class-input'), { target: { value: 'Math' } })
    fireEvent.click(screen.getByTestId('add-class-submit'))

    await waitFor(() => {
      expect(mockCreateClass).toHaveBeenCalledWith('Math', expect.any(Function))
    })
    expect(onCreated).toHaveBeenCalledWith(cls)
  })

  it('shows error on API failure', async () => {
    mockCreateClass.mockRejectedValueOnce(new Error('Server error'))

    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)

    fireEvent.change(screen.getByTestId('add-class-input'), { target: { value: 'Math' } })
    fireEvent.click(screen.getByTestId('add-class-submit'))

    await waitFor(() => {
      expect(screen.getByTestId('add-class-error')).toHaveTextContent('Server error')
    })
    expect(onCreated).not.toHaveBeenCalled()
  })

  it('calls onCancel on Escape key', () => {
    render(<AddClassForm onCreated={onCreated} onCancel={onCancel} />)

    fireEvent.keyDown(screen.getByTestId('add-class-input'), { key: 'Escape' })

    expect(onCancel).toHaveBeenCalled()
  })
})
