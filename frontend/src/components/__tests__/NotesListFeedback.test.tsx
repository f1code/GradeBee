import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, vi } from 'vitest'
import type { Note } from '../../api'

const mockSubmitFeedback = vi.fn()

vi.mock('../../api', () => ({
  submitFeedback: (...args: unknown[]) => mockSubmitFeedback(...args),
}))

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

beforeEach(() => {
  vi.clearAllMocks()
  mockSubmitFeedback.mockResolvedValue({ id: 1, created_at: '2026-01-01' })
})

function autoNote(overrides: Partial<Note> = {}): Note {
  return {
    id: 7,
    studentId: 1,
    date: '2026-01-01',
    summary: 'Worked well in group tasks.',
    source: 'auto',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

async function renderNotes(note: Note = autoNote()) {
  const { default: NotesList } = await import('../NotesList')
  const user = userEvent.setup()
  render(
    <NotesList
      notes={[note]}
      onEdit={vi.fn()}
      onDelete={vi.fn()}
      editingNoteId={null}
      onSaveEdit={vi.fn()}
      onCancelEdit={vi.fn()}
    />
  )
  return user
}

describe('NotesList thumbs-down comment', () => {
  // Same reasoning as ReportViewer: the comment is forwarded to Sentry as
  // written, so the hint is the mitigation. See docs/adr/0003.
  it('warns against student names beside the comment box', async () => {
    const user = await renderNotes()
    expect(screen.queryByTestId('thumb-down-privacy-hint-note-7')).not.toBeInTheDocument()

    await user.click(screen.getByTestId('thumb-down-note-7'))

    const hint = screen.getByTestId('thumb-down-privacy-hint-note-7')
    expect(hint).toHaveTextContent(/student names/i)
    expect(screen.getByTestId('thumb-down-comment-note-7')).toHaveAttribute(
      'aria-describedby',
      hint.id
    )
  })

  it('shows no thumbs controls on a manual note', async () => {
    await renderNotes(autoNote({ source: 'manual' }))
    expect(screen.queryByTestId('thumb-up-note-7')).not.toBeInTheDocument()
    expect(screen.queryByTestId('thumb-down-note-7')).not.toBeInTheDocument()
  })
})
