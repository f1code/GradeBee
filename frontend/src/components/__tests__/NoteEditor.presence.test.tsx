import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import NoteEditor from '../NoteEditor'

describe('NoteEditor presence', () => {
  it('does not put the editor inside a permanently clipped wrapper', () => {
    render(
      <NoteEditor
        mode="create"
        onSave={vi.fn()}
        onCancel={vi.fn()}
        saving={false}
      />,
    )

    const editor = screen.getByTestId('note-editor-textarea').closest('.note-editor') as HTMLElement
    expect(editor).not.toBeNull()
    expect(editor.parentElement).not.toHaveStyle({ overflow: 'hidden' })
  })
})
