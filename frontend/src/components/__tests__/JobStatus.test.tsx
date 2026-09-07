import { render, screen, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { JobListResponse, UploadJob, AssembleNotesResponse } from '../../api'
import { NoNotesClassUnclear, NoNotesNoNameMatched, NoNotesNobodyNamed } from '../../api-types.gen'

const mockFetchJobs = vi.fn()
const mockRetryFailedJobs = vi.fn()
const mockDismissJobs = vi.fn()
const mockListNotes = vi.fn()
const mockAssembleNotes = vi.fn()
const mockListClasses = vi.fn()
const mockAssignPassages = vi.fn()
const mockUndoAssignment = vi.fn()
const mockListStudents = vi.fn()

vi.mock('../../api', () => ({
  fetchJobs: (...args: unknown[]) => mockFetchJobs(...args),
  retryFailedJobs: (...args: unknown[]) => mockRetryFailedJobs(...args),
  dismissJobs: (...args: unknown[]) => mockDismissJobs(...args),
  listNotes: (...args: unknown[]) => mockListNotes(...args),
  assembleNotes: (...args: unknown[]) => mockAssembleNotes(...args),
  listClasses: (...args: unknown[]) => mockListClasses(...args),
  assignPassages: (...args: unknown[]) => mockAssignPassages(...args),
  undoAssignment: (...args: unknown[]) => mockUndoAssignment(...args),
  listStudents: (...args: unknown[]) => mockListStudents(...args),
}))

// One stable getToken, as Clerk hands out: a fresh function per render would
// change `poll`'s identity, re-run its effect and re-poll on every state change.
const mockGetToken = vi.fn()

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: mockGetToken }),
}))

const emptyJobs: JobListResponse = { active: [], failed: [], done: [] }

describe('JobStatus', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers({ shouldAdvanceTime: true })
    mockGetToken.mockResolvedValue('tok')
    mockListNotes.mockResolvedValue({ notes: [] })
    mockListClasses.mockResolvedValue({ classes: [{ id: 1, name: 'Monday' }, { id: 2, name: 'Tuesday' }] })
    mockListStudents.mockResolvedValue({ students: [{ id: 22, classId: 2, name: 'Eleonore', createdAt: '', aliases: [] }] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders nothing when no jobs exist', async () => {
    mockFetchJobs.mockResolvedValue(emptyJobs)
    const { default: JobStatus } = await import('../JobStatus')

    const { container } = render(<JobStatus />)
    await waitFor(() => {
      expect(mockFetchJobs).toHaveBeenCalled()
    })
    expect(container.querySelector('[data-testid="job-status"]')).not.toBeInTheDocument()
  })

  it('renders active jobs with spinner and status label', async () => {
    mockFetchJobs.mockResolvedValue({
      active: [{ uploadId: 1, fileName: 'lesson.m4a', status: 'transcribing', createdAt: '2026-03-26T10:00:00Z' }],
      failed: [],
      done: [],
    })
    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-active')).toBeInTheDocument()
    })
    expect(screen.getByText('lesson.m4a')).toBeInTheDocument()
    expect(screen.getByText('Transcribing')).toBeInTheDocument()
  })

  it('renders failed jobs with retry button', async () => {
    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [{ uploadId: 2, fileName: 'bad.mp3', status: 'failed', error: 'Whisper down', createdAt: '2026-03-26T09:00:00Z' }],
      done: [],
    })
    mockRetryFailedJobs.mockResolvedValue(undefined)
    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-failed')).toBeInTheDocument()
    })
    expect(screen.getByText('bad.mp3')).toBeInTheDocument()
    expect(screen.getByText('Whisper down')).toBeInTheDocument()
    expect(screen.getByTestId('job-retry-btn')).toBeInTheDocument()
  })

  it('retry button calls retryFailedJobs and re-polls', async () => {
    vi.useRealTimers()
    const user = userEvent.setup()

    // All polls return failed jobs until retry resolves
    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [{ uploadId: 2, fileName: 'bad.mp3', status: 'failed', error: 'err', createdAt: '2026-03-26T09:00:00Z' }],
      done: [],
    })
    mockRetryFailedJobs.mockResolvedValue(undefined)

    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-retry-btn')).toBeInTheDocument()
    })

    await user.click(screen.getByTestId('job-retry-btn'))

    await waitFor(() => {
      expect(mockRetryFailedJobs).toHaveBeenCalled()
    })
  })

  it('renders done jobs with note count', async () => {
    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [],
      done: [{
        uploadId: 3,
       
        fileName: 'complete.m4a',
        status: 'done' as const,
        noteLinks: [
          { name: 'Alice', noteId: 10, studentId: 1, className: 'Math' },
          { name: 'Bob', noteId: 11, studentId: 2, className: 'Math' },
        ],
        createdAt: '2026-03-26T08:00:00Z',
      }],
    })
    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-done')).toBeInTheDocument()
    })
    expect(screen.getByText('complete.m4a')).toBeInTheDocument()
    expect(screen.getByText('2 notes created')).toBeInTheDocument()
    const links = screen.getAllByText(/Alice|Bob/)
    expect(links).toHaveLength(2)
  })

  // A job that produced nothing can mean three different things and the teacher
  // can act on two of them, so the card must not say the same words for all
  // three. The messages are asserted by value: a card that renders the wrong
  // one sends the teacher to do the wrong thing next.
  //
  // class_unclear is here even though nothing on this branch sets it — the
  // extraction schema forces a class from an enum of the teacher's own classes.
  // #125's two-pass contract sets it, and this is the test that says the card
  // is already ready for it.
  it.each([
    [NoNotesClassUnclear, "No notes — the class wasn't clear. Say the class and time at the start."],
    [NoNotesNoNameMatched, 'No notes — no names matched your roster.'],
    [NoNotesNobodyNamed, 'No notes — nobody was named.'],
  ])('explains a done job with no notes: %s', async (reason, message) => {
    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [],
      done: [{
        uploadId: 4,
        fileName: 'quiet.m4a',
        status: 'done' as const,
        noteLinks: [],
        noNotesReason: reason,
        createdAt: '2026-03-26T08:00:00Z',
      }],
    })
    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-done')).toBeInTheDocument()
    })
    expect(screen.getByText(message)).toBeInTheDocument()
  })

  // An older job, or one from a server that does not send the field yet.
  it('falls back to the bare line when a no-note job carries no reason', async () => {
    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [],
      done: [{
        uploadId: 5,
        fileName: 'quiet.m4a',
        status: 'done' as const,
        noteLinks: [],
        createdAt: '2026-03-26T08:00:00Z',
      }],
    })
    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-done')).toBeInTheDocument()
    })
    expect(screen.getByText('No notes created')).toBeInTheDocument()
  })

  // Two recordings a class can rescue, and the server says which. Gated on the
  // reason and not on a passage count: the passage contract (#125) returns
  // passages that named nobody — a pronoun-only block, a class-wide remark —
  // which no picked class can resolve, and a declined recording (#127) carries
  // no passages at all, so a count would hide the picker on the card that needs
  // it most.
  describe('class picker', () => {
    // Typed like doneCard above, and for the same reason: a rename in the Go
    // types must break `tsc -b` here rather than pass against a dead shape.
    const noNoteCard: UploadJob = {
      userId: 'user_1',
      uploadId: 9,
      filePath: 'uploads/tuesday.m4a',
      mimeType: 'audio/m4a',
      source: 'audio',
      fileName: 'tuesday.m4a',
      status: 'done' as const,
      noteLinks: [],
      noNotesReason: NoNotesNoNameMatched,
      canPickClass: true,
      passages: [
        { kind: 'child', spokenLabels: ['Alice'], summary: 'Alice did great.' },
      ],
      createdAt: '2026-03-26T08:00:00Z',
    }

    it('offers every class, and the pick replaces the card with its notes', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [noNoteCard] })
      mockAssembleNotes.mockResolvedValue({
        className: 'Tuesday',
        noteLinks: [{ name: 'Alice', noteId: 40, studentId: 11, className: 'Tuesday' }],
        passages: [{ kind: 'child', spokenLabels: ['Alice'], student: 'Alice', summary: 'Alice did great.' }],
      } satisfies AssembleNotesResponse)

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('class-picker')).toBeInTheDocument()
      })
      await waitFor(() => {
        expect(screen.getAllByTestId('class-picker-option')).toHaveLength(2)
      })

      await user.click(screen.getByText('Tuesday'))

      await waitFor(() => {
        expect(screen.getByText('1 note created')).toBeInTheDocument()
      })
      // The class is the whole request. The server reads the transcript from
      // its own row and runs pass 2 against the picked class, so the words in
      // the notes are the model's own (#127).
      expect(mockAssembleNotes).toHaveBeenCalledWith(9, 'Tuesday', expect.anything())
      expect(screen.queryByTestId('class-picker')).not.toBeInTheDocument()
      expect(screen.getByText('Alice')).toBeInTheDocument()
    })

    // Picking the wrong one of two sibling classes is the mistake this path
    // exists to undo, so a pick that made no note must leave the picker up.
    //
    // The server files nothing on this outcome. Its response reports the pick
    // (the class and the run's passages, so the rows can be assigned by hand
    // against that roster) and keeps the reason the card already had.
    it('keeps the picker after a pick that resolved nobody', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [noNoteCard] })
      mockAssembleNotes.mockResolvedValue({
        className: 'Monday',
        classId: 1,
        noteLinks: [],
        passages: noNoteCard.passages ?? [],
        noNotesReason: NoNotesNoNameMatched,
        canPickClass: true,
      } satisfies AssembleNotesResponse)

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('class-picker')).toBeInTheDocument()
      })
      await user.click(screen.getByText('Monday'))

      await waitFor(() => {
        expect(mockAssembleNotes).toHaveBeenCalled()
      })
      expect(screen.getByTestId('class-picker')).toBeInTheDocument()
      expect(screen.getByText('No notes — no names matched your roster.')).toBeInTheDocument()
    })

    // The decline (#127): pass 1 could not pin a class, so pass 2 never ran and
    // the card holds no passages at all. The picker is the whole way out, and
    // the pick is that pass's deferred first run.
    it('offers the picker on a recording whose class was never pinned', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      const declined: UploadJob = { ...noNoteCard, passages: [], noNotesReason: NoNotesClassUnclear, canPickClass: true }
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [declined] })
      mockAssembleNotes.mockResolvedValue({
        className: 'Tuesday',
        noteLinks: [{ name: 'Alice', noteId: 40, studentId: 11, className: 'Tuesday' }],
        passages: [{ kind: 'child', spokenLabels: ['Alice'], student: 'Alice', summary: 'Alice did great.' }],
      } satisfies AssembleNotesResponse)

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('class-picker')).toBeInTheDocument()
      })
      expect(screen.getByText("No notes — the class wasn't clear. Say the class and time at the start.")).toBeInTheDocument()

      await user.click(screen.getByText('Tuesday'))

      await waitFor(() => {
        expect(screen.getByText('1 note created')).toBeInTheDocument()
      })
      expect(mockAssembleNotes).toHaveBeenCalledWith(9, 'Tuesday', expect.anything())
      expect(screen.queryByTestId('class-picker')).not.toBeInTheDocument()
    })

    // A card that made notes has nothing to pick, and neither has one that
    // heard no name at all.
    it.each([
      ['notes were made', { ...noNoteCard, noteLinks: [{ name: 'Alice', noteId: 1, studentId: 11, className: 'Tuesday' }], noNotesReason: undefined, canPickClass: false }],
      ['nobody was named', { ...noNoteCard, passages: [], noNotesReason: NoNotesNobodyNamed, canPickClass: false }],
      // The case the passage contract adds: the recording produced passages,
      // and not one of them speaks a name. A class picked here has nothing to
      // resolve, so the picker would be a button that cannot work.
      [
        'the passages name nobody',
        {
          ...noNoteCard,
          passages: [
            { kind: 'unknown' as const, summary: 'She knocked on the boxes.' },
            { kind: 'group' as const, summary: 'Everyone worked hard.' },
          ],
          noNotesReason: NoNotesNobodyNamed,
          canPickClass: false,
        },
      ],
      // The gate is the server's flag, not this component's reading of the
      // reason: a card the server says cannot be rescued gets no picker even
      // when its reason is one the picker usually appears on.
      ['the server says a class cannot rescue it', { ...noNoteCard, canPickClass: false }],
    ])('offers no picker when %s', async (_name, card) => {
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [card] })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('job-done')).toBeInTheDocument()
      })
      expect(screen.queryByTestId('class-picker')).not.toBeInTheDocument()
    })
  })

  // What the recording said that no note holds (#133). Note 694's shape: the
  // header is `none`, two blocks name nobody, Lévy resolves. The card lists
  // the two and nothing else — read-only here; filing them is #134.
  describe('passages that reached nobody', () => {
    const doneCard: UploadJob = {
      userId: 'user_1',
      uploadId: 12,
      filePath: 'uploads/694.m4a',
      mimeType: 'audio/m4a',
      source: 'audio',
      fileName: '694.m4a',
      status: 'done' as const,
      className: 'Tuesday',
      noteLinks: [{ name: 'Lévy', noteId: 50, studentId: 21, className: 'Tuesday' }],
      passages: [
        { kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' },
        { kind: 'unknown', summary: "Polly wasn't speaking much today." },
        { kind: 'child', spokenLabels: ['Lévy'], student: 'Lévy', summary: 'Lévy finished the puzzle alone.' },
      ],
      createdAt: '2026-03-26T08:00:00Z',
    }

    it('lists each unattributed passage on the done card', async () => {
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [doneCard] })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('passage-review')).toBeInTheDocument()
      })
      const rows = screen.getAllByTestId('passage-review-row')
      expect(rows.map(r => r.textContent)).toEqual([
        'She was helping the younger ones with their blocks.',
        "Polly wasn't speaking much today.",
      ])
      expect(screen.queryByText('Lévy finished the puzzle alone.')).not.toBeInTheDocument()
      // The note link is still the only place Lévy appears.
      expect(screen.getByText('Lévy')).toBeInTheDocument()
    })

    // A card where every passage reached a child, and a card from before the
    // field existed, both look exactly as they did.
    it.each([
      ['every passage reached a child', { ...doneCard, passages: [{ kind: 'child', student: 'Lévy', summary: 'Lévy finished the puzzle alone.' }] }],
      ['the job carries no passages', { ...doneCard, passages: undefined }],
    ])('shows nothing new when %s', async (_name, card) => {
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [card] })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('job-done')).toBeInTheDocument()
      })
      expect(screen.queryByTestId('passage-review')).not.toBeInTheDocument()
    })

    // The rows follow the card's view of the job, so a class pick that re-ran
    // pass 2 replaces them with what that run left unfiled.
    it('replaces the rows after a class pick', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      const noNote: UploadJob = { ...doneCard, noteLinks: [], noNotesReason: NoNotesNoNameMatched, canPickClass: true }
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [noNote] })
      mockAssembleNotes.mockResolvedValue({
        className: 'Tuesday',
        noteLinks: [{ name: 'Polly', noteId: 51, studentId: 22, className: 'Tuesday' }],
        passages: [
          { kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' },
          { kind: 'child', spokenLabels: ['Polly'], student: 'Polly', summary: "Polly wasn't speaking much today." },
        ],
      } satisfies AssembleNotesResponse)

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getAllByTestId('passage-review-row')).toHaveLength(2)
      })
      await user.click(screen.getByText('Tuesday'))

      await waitFor(() => {
        expect(screen.getAllByTestId('passage-review-row')).toHaveLength(1)
      })
      expect(screen.getByText('She was helping the younger ones with their blocks.')).toBeInTheDocument()
      expect(screen.queryByText("Polly wasn't speaking much today.")).not.toBeInTheDocument()
    })
  })

  describe('filing a passage to a child', () => {
    // A child's name is on the card twice once they have a note: the picker
    // chip and the note link. Every query here says which one it means.
    function chipFor(name: string): HTMLElement {
      const chip = screen.getAllByTestId('passage-review-student').find(b => b.textContent === name)
      if (chip === undefined) throw new Error(`no picker chip for ${name}`)
      return chip
    }
    const noteLinks = () => screen.queryAllByTestId('job-note-link').map(b => b.textContent?.trim())

    const card: UploadJob = {
      userId: 'user_1',
      uploadId: 12,
      filePath: 'uploads/694.m4a',
      mimeType: 'audio/m4a',
      source: 'audio',
      fileName: '694.m4a',
      status: 'done' as const,
      className: 'Tuesday',
      classId: 2,
      noteLinks: [{ name: 'Lévy', noteId: 50, studentId: 21, className: 'Tuesday' }],
      passages: [
        { kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' },
        { kind: 'child', spokenLabels: ['Lévy'], student: 'Lévy', summary: 'Lévy finished the puzzle alone.' },
      ],
      createdAt: '2026-03-26T08:00:00Z',
    }

    // The response is a note link, and it joins the card's links the way an
    // assemble result does: the count moves, the child's name is a link.
    it('merges the new note into the card, once', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [card] })
      mockAssignPassages.mockResolvedValue({ noteId: 60, studentId: 22, name: 'Eleonore', className: 'Tuesday', appended: false })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => chipFor('Eleonore'))
      expect(mockListStudents).toHaveBeenCalledWith(2, expect.anything())
      expect(screen.getByText('1 note created')).toBeInTheDocument()

      await user.click(screen.getByTestId('passage-review-check'))
      await user.click(chipFor('Eleonore'))

      await waitFor(() => {
        expect(screen.getByText('2 notes created')).toBeInTheDocument()
      })
      expect(mockAssignPassages).toHaveBeenCalledWith(12, {
        classId: 2,
        studentId: 22,
        passages: [{ kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' }],
      }, expect.anything())
      expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument()

      // The poll brings back what assign wrote to the job. Keyed on note id,
      // the link the card already holds replaces nothing and doubles nothing.
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [{
        ...card,
        noteLinks: [...card.noteLinks!, { name: 'Eleonore', noteId: 60, studentId: 22, className: 'Tuesday' }],
      }] })
      await act(async () => { vi.advanceTimersByTime(60_000) })
      await waitFor(() => {
        expect(mockFetchJobs).toHaveBeenCalledTimes(2)
      })
      expect(screen.getByText('2 notes created')).toBeInTheDocument()
      expect(noteLinks().filter(n => n === 'Eleonore')).toHaveLength(1)
    })

    // Lévy already has a note from this recording, so the card sends its id
    // and the row lands on that note (#135). The response names a note the
    // card holds: the count stays, and Lévy is one link, not two.
    it('appends to the note the card already holds for the picked child', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [card] })
      mockListStudents.mockResolvedValue({ students: [
        { id: 21, classId: 2, name: 'Lévy', createdAt: '', aliases: [] },
        { id: 22, classId: 2, name: 'Eleonore', createdAt: '', aliases: [] },
      ] })
      mockAssignPassages.mockResolvedValue({ noteId: 50, studentId: 21, name: 'Lévy', className: 'Tuesday', appended: true })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => chipFor('Lévy'))
      await user.click(screen.getByTestId('passage-review-check'))
      await user.click(chipFor('Lévy'))

      await waitFor(() => {
        expect(screen.getByTestId('passage-review-filed')).toHaveTextContent('Assigned to Lévy')
      })
      expect(mockAssignPassages).toHaveBeenCalledWith(12, {
        classId: 2,
        studentId: 21,
        passages: [{ kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' }],
        appendToNoteId: 50,
      }, expect.anything())
      expect(screen.getByText('1 note created')).toBeInTheDocument()
      expect(screen.getAllByRole('button', { name: 'Lévy' })).toHaveLength(1)
    })

    // Two confirms to Eleonore in one tab: the first creates, and the link it
    // made is what the second sends back, so the second row joins that note.
    it('sends the note an earlier assign made when the same child is picked again', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [{
        ...card,
        passages: [
          { kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' },
          { kind: 'unknown', summary: "Polly wasn't speaking much today." },
        ],
      }] })
      mockAssignPassages
        .mockResolvedValueOnce({ noteId: 60, studentId: 22, name: 'Eleonore', className: 'Tuesday', appended: false })
        .mockResolvedValueOnce({ noteId: 60, studentId: 22, name: 'Eleonore', className: 'Tuesday', appended: true })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => chipFor('Eleonore'))
      await user.click(screen.getAllByTestId('passage-review-check')[0])
      await user.click(chipFor('Eleonore'))
      await waitFor(() => {
        expect(screen.getByText('2 notes created')).toBeInTheDocument()
      })

      await user.click(screen.getAllByTestId('passage-review-check')[1])
      await user.click(chipFor('Eleonore'))
      await waitFor(() => {
        expect(screen.getAllByTestId('passage-review-filed')).toHaveLength(2)
      })

      expect(mockAssignPassages.mock.calls[0][1]).not.toHaveProperty('appendToNoteId')
      expect(mockAssignPassages.mock.calls[1][1]).toMatchObject({ studentId: 22, appendToNoteId: 60 })
      expect(screen.getByText('2 notes created')).toBeInTheDocument()
      expect(noteLinks().filter(n => n === 'Eleonore')).toHaveLength(1)
    })

    // The honest mistake, taken back (#138): the note the server deleted
    // leaves the card at once — count down, name gone — and the row is open
    // again. Picking the same child then creates, not appends: the card no
    // longer holds a link for them to send back. The new note comes back
    // under the deleted one's id, as SQLite hands out after a delete of the
    // newest row, and the card must show it all the same.
    it('drops the note an undo deleted, and the next pick of that child creates', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [card] })
      mockAssignPassages
        .mockResolvedValueOnce({ noteId: 60, studentId: 22, name: 'Eleonore', className: 'Tuesday', appended: false })
        .mockResolvedValueOnce({ noteId: 60, studentId: 22, name: 'Eleonore', className: 'Tuesday', appended: false })
      mockUndoAssignment.mockResolvedValue({ noteIds: [60] })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => chipFor('Eleonore'))
      await user.click(screen.getByTestId('passage-review-check'))
      await user.click(chipFor('Eleonore'))
      await waitFor(() => {
        expect(screen.getByText('2 notes created')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('passage-review-undo'))
      await waitFor(() => {
        expect(screen.getByText('1 note created')).toBeInTheDocument()
      })
      expect(mockUndoAssignment).toHaveBeenCalledWith(12, 22, expect.anything())
      expect(noteLinks()).not.toContain('Eleonore')
      expect(screen.queryByTestId('passage-review-filed')).not.toBeInTheDocument()
      expect(screen.getByTestId('passage-review-check')).toBeEnabled()

      await user.click(screen.getByTestId('passage-review-check'))
      await user.click(chipFor('Eleonore'))
      await waitFor(() => {
        expect(screen.getByText('2 notes created')).toBeInTheDocument()
      })
      expect(mockAssignPassages.mock.calls[1][1]).not.toHaveProperty('appendToNoteId')
      expect(noteLinks()).toContain('Eleonore')
    })

    // A card from before the field existed has no roster to offer.
    it('keeps the rows read-only when the job carries no class id', async () => {
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [{ ...card, classId: undefined }] })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('passage-review-row')).toBeInTheDocument()
      })
      expect(screen.queryByTestId('passage-review-check')).not.toBeInTheDocument()
      expect(mockListStudents).not.toHaveBeenCalled()
    })
  })

  it('shows "new" badge for newly completed jobs', async () => {
    // First poll: no done jobs
    mockFetchJobs
      .mockResolvedValueOnce({ active: [{ uploadId: 1, fileName: 'a.m4a', status: 'transcribing', createdAt: '2026-03-26T10:00:00Z' }], failed: [], done: [] })
      // Second poll: job is done
      .mockResolvedValue({ active: [], failed: [], done: [{ uploadId: 1, fileName: 'a.m4a', status: 'done' as const, noteLinks: [{ name: 'Student', noteId: 5, studentId: 3, className: 'Science' }], createdAt: '2026-03-26T10:00:00Z' }] })

    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-active')).toBeInTheDocument()
    })

    // Advance timer to trigger second poll
    await act(async () => {
      vi.advanceTimersByTime(3_500)
    })

    await waitFor(() => {
      expect(screen.getByTestId('job-new-badge')).toBeInTheDocument()
    })
  })

  it('shows View transcript button when job has transcript', async () => {
    vi.useRealTimers()
    const user = userEvent.setup()

    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [],
      done: [{
        uploadId: 5,
        fileName: 'with-transcript.m4a',
        status: 'done' as const,
        transcript: 'Emma did great on math. Jacob struggled with reading.',
        noteLinks: [
          { name: 'Emma', noteId: 30, studentId: 10, className: 'Class A' },
        ],
        createdAt: '2026-03-27T10:00:00Z',
      }],
    })

    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByText('View transcript')).toBeInTheDocument()
    })

    // Click to expand
    await user.click(screen.getByText('View transcript'))

    await waitFor(() => {
      expect(screen.getByText(/Emma did great on math/)).toBeInTheDocument()
      expect(screen.getByText('Hide transcript')).toBeInTheDocument()
    })
  })

  it('does not show View transcript button when transcript is empty', async () => {
    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [],
      done: [{
        uploadId: 6,
        fileName: 'no-transcript.m4a',
        status: 'done' as const,
        noteLinks: [],
        createdAt: '2026-03-27T10:00:00Z',
      }],
    })

    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByTestId('job-done')).toBeInTheDocument()
    })
    expect(screen.queryByText('View transcript')).not.toBeInTheDocument()
  })

  // The job queue is in memory and production restarts about twice a day. A
  // card that vanishes on its own is the failure the review flow refuses: only
  // a dismiss, or the teacher's own refresh, ends a review.
  describe('retained done cards', () => {
    // Typed: the fixture is what the server sends, so a change to the Go
    // types breaks `tsc -b` here instead of passing against a dead shape.
    const doneCard: UploadJob = {
      userId: 'user_1',
      uploadId: 7,
      filePath: 'uploads/restart.m4a',
      fileName: 'restart.m4a',
      mimeType: 'audio/m4a',
      source: 'audio',
      status: 'done',
      noteLinks: [{ name: 'Emma', noteId: 30, studentId: 10, className: 'Class A' }],
      transcript: 'Emma did great on math. Jacob struggled with reading.',
      passages: [
        { kind: 'child', spokenLabels: ['Emma'], student: 'Emma', summary: 'Emma did great on math.' },
        { kind: 'child', spokenLabels: ['Jacob'], summary: 'Jacob struggled with reading.' },
      ],
      createdAt: '2026-03-27T10:00:00Z',
    }

    it('keeps a card the server has forgotten until dismissed', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      mockFetchJobs
        .mockResolvedValueOnce({ active: [], failed: [], done: [doneCard] })
        .mockResolvedValue(emptyJobs)
      mockDismissJobs.mockResolvedValue({ dismissed: 0 })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('job-done')).toBeInTheDocument()
      })

      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      await waitFor(() => {
        expect(mockFetchJobs).toHaveBeenCalledTimes(2)
      })
      expect(screen.getByTestId('job-done')).toBeInTheDocument()

      await user.click(screen.getByTestId('job-dismiss'))
      await waitFor(() => {
        expect(screen.queryByTestId('job-done')).not.toBeInTheDocument()
      })
      expect(mockDismissJobs).toHaveBeenCalledWith(expect.anything(), [7])
    })

    it('dismisses one card and keeps the other through an empty poll', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      const other = { ...doneCard, uploadId: 8, fileName: 'kept.m4a' }
      mockFetchJobs
        .mockResolvedValueOnce({ active: [], failed: [], done: [doneCard, other] })
        .mockResolvedValue(emptyJobs)
      mockDismissJobs.mockResolvedValue({ dismissed: 1 })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getAllByTestId('job-done')).toHaveLength(2)
      })

      await user.click(screen.getAllByTestId('job-dismiss')[0])

      await waitFor(() => {
        expect(screen.getAllByTestId('job-done')).toHaveLength(1)
      })
      expect(screen.getByText('kept.m4a')).toBeInTheDocument()
      expect(screen.queryByText('restart.m4a')).not.toBeInTheDocument()

      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      expect(screen.getByText('kept.m4a')).toBeInTheDocument()
    })

    // The third fetch is the one that matters: an empty poll used to stop
    // polling for good, which would strand a retained card on stale spans.
    it('keeps polling at the idle interval while a card is retained', async () => {
      mockFetchJobs
        .mockResolvedValueOnce({ active: [], failed: [], done: [doneCard] })
        .mockResolvedValue(emptyJobs)

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('job-done')).toBeInTheDocument()
      })

      await act(async () => {
        vi.advanceTimersByTime(3_500)
      })
      expect(mockFetchJobs).toHaveBeenCalledTimes(1)

      await act(async () => {
        vi.advanceTimersByTime(60_000)
      })
      await waitFor(() => {
        expect(mockFetchJobs).toHaveBeenCalledTimes(2)
      })

      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      await waitFor(() => {
        expect(mockFetchJobs).toHaveBeenCalledTimes(3)
      })
    })

    // Clear all reads the retained cards, not the poll: after a restart the
    // server lists nothing, and a button that clears nothing is worse than none.
    it('clears every retained card, including ones the server has forgotten', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      const other = { ...doneCard, uploadId: 8, fileName: 'kept.m4a' }
      mockFetchJobs
        .mockResolvedValueOnce({ active: [], failed: [], done: [doneCard, other] })
        .mockResolvedValue(emptyJobs)
      mockDismissJobs.mockResolvedValue({ dismissed: 0 })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getAllByTestId('job-done')).toHaveLength(2)
      })

      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      await waitFor(() => {
        expect(mockFetchJobs).toHaveBeenCalledTimes(2)
      })
      expect(screen.getAllByTestId('job-done')).toHaveLength(2)

      await user.click(screen.getByTestId('job-clear-all'))

      await waitFor(() => {
        expect(screen.queryByTestId('job-done')).not.toBeInTheDocument()
      })
      expect(mockDismissJobs).toHaveBeenCalledWith(expect.anything(), [7, 8])

      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      expect(screen.queryByTestId('job-done')).not.toBeInTheDocument()
    })

    // Five is the whole retention, not just the view: the oldest card expires
    // when a sixth arrives. The server keeps listing the expired one, so the
    // merge must place it by its own timestamp — putting a re-seen card first
    // would push a newer card off the bottom, every poll.
    it('expires the oldest card once six exist, and clears them all', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      const cards = [1, 2, 3, 4, 5, 6].map((n) => ({
        ...doneCard,
        uploadId: 100 + n,
        fileName: `lesson-${n}.m4a`,
        createdAt: `2026-03-27T0${9 - n}:00:00Z`,
      }))
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: cards })
      mockDismissJobs.mockResolvedValue({ dismissed: 6 })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      const shown = () => screen.getAllByTestId('job-done').map((el) => el.querySelector('.job-file-name')?.textContent)
      await waitFor(() => {
        expect(screen.getAllByTestId('job-done')).toHaveLength(5)
      })
      const first = shown()
      expect(screen.queryByText('lesson-6.m4a')).not.toBeInTheDocument()

      // The expired card is still on the server; it must not climb back in.
      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      await waitFor(() => {
        expect(mockFetchJobs).toHaveBeenCalledTimes(2)
      })
      expect(shown()).toEqual(first)
      expect(screen.queryByText('lesson-6.m4a')).not.toBeInTheDocument()

      // Clear all still reaches the expired card, so nothing pops back.
      await user.click(screen.getByTestId('job-clear-all'))
      await waitFor(() => {
        expect(screen.queryByTestId('job-done')).not.toBeInTheDocument()
      })
      expect(mockDismissJobs).toHaveBeenCalledWith(expect.anything(), [101, 102, 103, 104, 105, 106])
    })

    // The other side of the cap: a recording that finishes while five are
    // retained has to take the top slot, not fall off the bottom.
    it('takes in a newly finished card and expires the oldest', async () => {
      const cards = [1, 2, 3, 4, 5].map((n) => ({
        ...doneCard,
        uploadId: 100 + n,
        fileName: `lesson-${n}.m4a`,
        createdAt: `2026-03-27T0${9 - n}:00:00Z`,
      }))
      const fresh = { ...doneCard, uploadId: 200, fileName: 'just-finished.m4a', createdAt: '2026-03-27T12:00:00Z' }
      mockFetchJobs
        .mockResolvedValueOnce({ active: [], failed: [], done: cards })
        .mockResolvedValue({ active: [], failed: [], done: [fresh, ...cards] })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getAllByTestId('job-done')).toHaveLength(5)
      })

      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      await waitFor(() => {
        expect(screen.getByText('just-finished.m4a')).toBeInTheDocument()
      })
      expect(screen.getAllByTestId('job-done')).toHaveLength(5)
      expect(screen.queryByText('lesson-5.m4a')).not.toBeInTheDocument()
    })

    // Go trims trailing zeros from the fractional seconds, so timestamps
    // arrive at mixed widths and text order is not time order: ".5Z" sorts
    // before "Z" as a string, and the newer card would render last.
    it('orders cards by time, not by timestamp text', async () => {
      const newer = { ...doneCard, uploadId: 301, fileName: 'newer.m4a', createdAt: '2026-03-27T10:00:00.5Z' }
      const older = { ...doneCard, uploadId: 302, fileName: 'older.m4a', createdAt: '2026-03-27T10:00:00Z' }
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [newer, older] })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getAllByTestId('job-done')).toHaveLength(2)
      })
      const cards = screen.getAllByTestId('job-done')
      expect(cards[0]).toHaveTextContent('newer.m4a')
      expect(cards[1]).toHaveTextContent('older.m4a')
    })

    // A dismiss that lands while a poll is in flight used to leave two timer
    // chains running: the clearTimeout hits a handle that already fired. An
    // empty list once killed the spare chain; a retained card would now feed
    // it for the life of the tab.
    it('does not double the poll rate when a dismiss overlaps a poll', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      const other = { ...doneCard, uploadId: 8, fileName: 'kept.m4a' }
      let releaseSecond = () => {}
      mockFetchJobs
        .mockResolvedValueOnce({ active: [], failed: [], done: [doneCard, other] })
        .mockImplementationOnce(
          () => new Promise((resolve) => { releaseSecond = () => resolve({ active: [], failed: [], done: [doneCard, other] }) }),
        )
        .mockResolvedValue({ active: [], failed: [], done: [other] })
      mockDismissJobs.mockResolvedValue({ dismissed: 1 })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getAllByTestId('job-done')).toHaveLength(2)
      })

      // The idle poll fires and hangs.
      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      expect(mockFetchJobs).toHaveBeenCalledTimes(2)

      // Dismiss re-polls while that one is still out.
      await user.click(screen.getAllByTestId('job-dismiss')[0])
      await waitFor(() => {
        expect(mockFetchJobs).toHaveBeenCalledTimes(3)
      })

      await act(async () => {
        releaseSecond()
        await Promise.resolve()
      })

      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      await waitFor(() => {
        expect(mockFetchJobs).toHaveBeenCalledTimes(4)
      })
    })

    // A live server still owns a card it remembers: the poll's copy wins, so
    // work that landed elsewhere reaches the open card.
    it('takes fresh state from the poll for a card still queued', async () => {
      const pending = { ...doneCard, noteLinks: [], noNotesReason: NoNotesNoNameMatched }
      mockFetchJobs
        .mockResolvedValueOnce({ active: [], failed: [], done: [pending] })
        .mockResolvedValue({ active: [], failed: [], done: [doneCard] })

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByText('No notes — no names matched your roster.')).toBeInTheDocument()
      })

      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      await waitFor(() => {
        expect(screen.getByText('1 note created')).toBeInTheDocument()
      })
      // And the picker goes with it: the notes exist now.
      expect(screen.queryByTestId('class-picker')).not.toBeInTheDocument()
    })

    // Local removal is authoritative: a dismiss the server refused must not
    // resurrect the card on the next poll.
    it('does not bring back a dismissed card when the server call fails', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [doneCard] })
      mockDismissJobs.mockRejectedValue(new Error('nope'))

      const { default: JobStatus } = await import('../JobStatus')
      render(<JobStatus />)

      await waitFor(() => {
        expect(screen.getByTestId('job-done')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('job-dismiss'))
      await waitFor(() => {
        expect(screen.queryByTestId('job-done')).not.toBeInTheDocument()
      })
      expect(screen.getByTestId('job-error')).toBeInTheDocument()

      await act(async () => {
        vi.advanceTimersByTime(61_000)
      })
      await waitFor(() => {
        expect(mockFetchJobs.mock.calls.length).toBeGreaterThan(1)
      })
      expect(screen.queryByTestId('job-done')).not.toBeInTheDocument()
    })
  })

  it('opens StudentDetail modal when clicking a note link', async () => {
    vi.useRealTimers()
    const user = userEvent.setup()

    mockFetchJobs.mockResolvedValue({
      active: [],
      failed: [],
      done: [{
        uploadId: 4,
       
        fileName: 'recording.m4a',
        status: 'done' as const,
        noteLinks: [{ name: 'Maxence', noteId: 20, studentId: 7, className: 'CE2' }],
        createdAt: '2026-03-27T10:00:00Z',
      }],
    })

    const { default: JobStatus } = await import('../JobStatus')
    render(<JobStatus />)

    await waitFor(() => {
      expect(screen.getByText('Maxence')).toBeInTheDocument()
    })

    await user.click(screen.getByText('Maxence'))

    await waitFor(() => {
      expect(screen.getByTestId('student-modal-overlay')).toBeInTheDocument()
      expect(screen.getByTestId('student-detail-7')).toBeInTheDocument()
    })
  })
})
