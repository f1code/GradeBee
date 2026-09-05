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

vi.mock('../../api', () => ({
  fetchJobs: (...args: unknown[]) => mockFetchJobs(...args),
  retryFailedJobs: (...args: unknown[]) => mockRetryFailedJobs(...args),
  dismissJobs: (...args: unknown[]) => mockDismissJobs(...args),
  listNotes: (...args: unknown[]) => mockListNotes(...args),
  assembleNotes: (...args: unknown[]) => mockAssembleNotes(...args),
  listClasses: (...args: unknown[]) => mockListClasses(...args),
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
    // The server files nothing on this outcome and its response says so — no
    // class, and the reason the card already had.
    it('keeps the picker after a pick that resolved nobody', async () => {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      mockFetchJobs.mockResolvedValue({ active: [], failed: [], done: [noNoteCard] })
      mockAssembleNotes.mockResolvedValue({
        className: '',
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
