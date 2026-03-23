import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import NoteConfirmation from '../NoteConfirmation'
import type { ExtractResult } from '../../api'

const baseExtract: ExtractResult = {
  students: [
    { name: 'Alice', class: 'Math 101', summary: 'Good work', confidence: 0.9 },
    { name: 'Bob', class: 'Math 101', summary: 'Needs improvement', confidence: 0.4, candidates: [{ name: 'Robert', class: 'Math 101' }] },
  ],
  date: '2026-03-20',
}

function renderComponent(overrides = {}) {
  const props = {
    extractResult: baseExtract,
    transcript: 'Alice did great. Bob needs work.',
    onSave: vi.fn(),
    onCancel: vi.fn(),
    saving: false,
    savedNotes: null,
    onReset: vi.fn(),
    ...overrides,
  }
  render(<NoteConfirmation {...props} />)
  return props
}

describe('NoteConfirmation', () => {
  // Tier 3 — smoke
  it('renders student list from extractResult', () => {
    renderComponent()
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
  })

  // Tier 4 — interactions
  it('shows confidence badges correctly', () => {
    renderComponent()
    expect(screen.getByText('✓ Matched')).toBeInTheDocument()
    expect(screen.getByText('⚠ Uncertain')).toBeInTheDocument()
  })

  it('shows candidates for low confidence', () => {
    renderComponent()
    expect(screen.getByText(/Robert/)).toBeInTheDocument()
  })

  it('selecting candidate updates name', async () => {
    const user = userEvent.setup()
    renderComponent()
    await user.click(screen.getByRole('button', { name: /Robert/ }))
    // After selecting, Bob should be replaced by Robert and show matched
    expect(screen.queryByText('Bob')).not.toBeInTheDocument()
    expect(screen.getByText('Robert')).toBeInTheDocument()
  })

  it('editing summary updates textarea', () => {
    renderComponent()
    const textareas = screen.getAllByPlaceholderText('Student summary...')
    fireEvent.change(textareas[0], { target: { value: 'Updated summary' } })
    expect(textareas[0]).toHaveValue('Updated summary')
  })

  it('save calls onSave with edited data', async () => {
    const user = userEvent.setup()
    const props = renderComponent()
    await user.click(screen.getByText('Save 2 Notes'))
    expect(props.onSave).toHaveBeenCalledWith(
      [
        { name: 'Alice', class: 'Math 101', summary: 'Good work' },
        { name: 'Bob', class: 'Math 101', summary: 'Needs improvement' },
      ],
      '2026-03-20',
    )
  })

  it('cancel calls onCancel', async () => {
    const user = userEvent.setup()
    const props = renderComponent()
    await user.click(screen.getByText('Cancel'))
    expect(props.onCancel).toHaveBeenCalled()
  })

  it('shows saved notes with doc links', () => {
    renderComponent({
      savedNotes: [
        { student: 'Alice', class: 'Math 101', docId: 'd1', docUrl: 'https://docs/d1' },
      ],
    })
    expect(screen.getByText('Note created!')).toBeInTheDocument()
    expect(screen.getByText('Alice — Math 101')).toBeInTheDocument()
  })

  it('remove button removes student', async () => {
    const user = userEvent.setup()
    renderComponent()
    const removeBtns = screen.getAllByTitle('Remove student')
    await user.click(removeBtns[0])
    expect(screen.queryByText('Alice')).not.toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
  })
})
