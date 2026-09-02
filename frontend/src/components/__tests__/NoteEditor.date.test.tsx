import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import NoteEditor from '../NoteEditor'

describe('NoteEditor date field', () => {
  // The date of an existing note is not editable — every layer below the editor
  // drops it. A readonly <input type="date"> is not enough: Chrome still opens the
  // calendar picker on one, so the teacher could change the date, save, and watch it
  // revert with no error.
  it('renders the date as static text when editing an existing note', () => {
    render(
      <NoteEditor
        mode="edit"
        initialSummary="Did well"
        initialDate="2026-02-10"
        onSave={vi.fn()}
        onCancel={vi.fn()}
        saving={false}
      />,
    )

    const date = screen.getByTestId('note-editor-date')
    expect(date.tagName).toBe('SPAN')
    expect(date).toHaveTextContent('February 10, 2026')
  })

  it('renders an editable date input when creating a note', () => {
    render(
      <NoteEditor
        mode="create"
        onSave={vi.fn()}
        onCancel={vi.fn()}
        saving={false}
      />,
    )

    const date = screen.getByTestId('note-editor-date')
    expect(date.tagName).toBe('INPUT')
    expect(date).not.toHaveAttribute('readonly')
  })
})
