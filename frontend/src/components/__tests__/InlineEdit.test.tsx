import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import InlineEdit from '../InlineEdit'

describe('InlineEdit', () => {
  it('saves the trimmed value on Enter', () => {
    const onSave = vi.fn()
    const onCancel = vi.fn()
    render(<InlineEdit value="Marcia" onSave={onSave} onCancel={onCancel} />)

    const input = screen.getByTestId('inline-edit-input')
    fireEvent.change(input, { target: { value: '  Marcia Renamed  ' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onSave).toHaveBeenCalledTimes(1)
    expect(onSave).toHaveBeenCalledWith('Marcia Renamed')
  })

  it('does not fire onSave a second time when Enter unmounts the input before blur', () => {
    // Enter commits the rename and the parent typically re-renders without
    // this input (editingId reset to null), which fires a native blur on
    // unmount. That blur must not re-invoke onSave/onCancel.
    const onSave = vi.fn()
    const onCancel = vi.fn()
    const { unmount } = render(<InlineEdit value="Marcia" onSave={onSave} onCancel={onCancel} />)

    const input = screen.getByTestId('inline-edit-input')
    fireEvent.change(input, { target: { value: 'Marcia Renamed' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onSave).toHaveBeenCalledTimes(1)

    unmount()

    expect(onSave).toHaveBeenCalledTimes(1)
    expect(onCancel).not.toHaveBeenCalled()
  })

  it('cancels on Escape without saving', () => {
    const onSave = vi.fn()
    const onCancel = vi.fn()
    render(<InlineEdit value="Marcia" onSave={onSave} onCancel={onCancel} />)

    fireEvent.keyDown(screen.getByTestId('inline-edit-input'), { key: 'Escape' })

    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onSave).not.toHaveBeenCalled()
  })

  it('cancels on blur when value is unchanged', () => {
    const onSave = vi.fn()
    const onCancel = vi.fn()
    render(<InlineEdit value="Marcia" onSave={onSave} onCancel={onCancel} />)

    fireEvent.blur(screen.getByTestId('inline-edit-input'))

    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onSave).not.toHaveBeenCalled()
  })

  it('saves on blur when the value changed', () => {
    const onSave = vi.fn()
    const onCancel = vi.fn()
    render(<InlineEdit value="Marcia" onSave={onSave} onCancel={onCancel} />)

    const input = screen.getByTestId('inline-edit-input')
    fireEvent.change(input, { target: { value: 'Marcia Renamed' } })
    fireEvent.blur(input)

    expect(onSave).toHaveBeenCalledWith('Marcia Renamed')
    expect(onCancel).not.toHaveBeenCalled()
  })
})
