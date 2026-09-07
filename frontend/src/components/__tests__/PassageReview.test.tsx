import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { JobPassage } from '../../api-types.gen'
import type { AssignPassagesRequest, AssignPassagesResponse } from '../../api'
import PassageReview from '../PassageReview'
import { unattributed } from '../../lib/passages'

const mockListStudents = vi.fn()
vi.mock('../../api', () => ({
  listStudents: (...args: unknown[]) => mockListStudents(...args),
}))

const mockGetToken = vi.fn()
vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: mockGetToken }),
}))

beforeEach(() => {
  vi.clearAllMocks()
  mockGetToken.mockResolvedValue('tok')
  mockListStudents.mockResolvedValue({ students: [
    { id: 21, classId: 3, name: 'Lévy', createdAt: '', aliases: [] },
    { id: 22, classId: 3, name: 'Eleonore', createdAt: '', aliases: [] },
  ] })
})

// Note 694's shape (#131): the spoken header comes back `none`, two blocks
// name nobody, and Lévy resolves. Two rows to show; the rest is filed or
// dropped. `none` is on the fixture even though assembly never puts one on
// the wire — a row for a header would be a bug worth pinning against.
const note694: JobPassage[] = [
  { kind: 'none', summary: 'Tuesday, period one.' },
  { kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' },
  { kind: 'unknown', summary: "Polly wasn't speaking much today." },
  { kind: 'child', spokenLabels: ['Lévy'], student: 'Lévy', summary: 'Lévy finished the puzzle alone.' },
]

describe('unattributed', () => {
  it('keeps the passages that reached nobody, in order', () => {
    expect(unattributed(note694).map(p => p.summary)).toEqual([
      'She was helping the younger ones with their blocks.',
      "Polly wasn't speaking much today.",
    ])
  })

  // A child passage whose student the pipeline could not pin is as lost as an
  // unknown one, and reads the same to the teacher.
  it('counts a child passage with no student', () => {
    const rows = unattributed([{ kind: 'child', spokenLabels: ['Sam'], summary: 'Sam shared.' }])
    expect(rows).toHaveLength(1)
  })

  // A class-wide remark joins every note this recording made; it is not
  // something to file to one child.
  it('never lists a group passage', () => {
    expect(unattributed([{ kind: 'group', summary: 'Everyone worked hard.' }])).toEqual([])
  })
})

describe('PassageReview', () => {
  it('shows one row per passage that reached nobody', () => {
    render(<PassageReview passages={note694} />)
    expect(screen.getByText("AI couldn't find the student for these:")).toBeInTheDocument()
    const rows = screen.getAllByTestId('passage-review-row')
    expect(rows.map(r => r.textContent)).toEqual([
      'She was helping the younger ones with their blocks.',
      "Polly wasn't speaking much today.",
    ])
    expect(screen.queryByText(/Lévy/)).not.toBeInTheDocument()
    expect(screen.queryByText('Tuesday, period one.')).not.toBeInTheDocument()
  })

  it('renders nothing when every passage reached somebody', () => {
    const { container } = render(
      <PassageReview passages={[
        { kind: 'child', student: 'Alice', summary: 'Alice did great.' },
        { kind: 'group', summary: 'Everyone worked hard.' },
      ]} />,
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing for an empty array', () => {
    const { container } = render(<PassageReview passages={[]} />)
    expect(container).toBeEmptyDOMElement()
  })
})

// Filing rows to a child (#134). The component knows nothing about jobs: it
// gets rows, a class id and a callback, and the callback gets a body.
describe('PassageReview filing', () => {
  // The recording's class-wide remark, on the card but never a row: it rides
  // along on every assignment, as it joins every note the pipeline makes.
  const withGroup: JobPassage[] = [...note694, { kind: 'group', summary: 'Everyone worked hard.' }]
  const filed: AssignPassagesResponse = { noteId: 60, studentId: 22, name: 'Eleonore', className: 'Tuesday', appended: false }

  // A job done before the field existed, or nothing pinned: nothing to file
  // to, so the rows are read-only and no roster is fetched.
  it('shows no controls without a class', () => {
    render(<PassageReview passages={note694} onAssign={vi.fn()} />)
    expect(screen.getAllByTestId('passage-review-row')).toHaveLength(2)
    expect(screen.queryByTestId('passage-review-check')).not.toBeInTheDocument()
    expect(screen.queryByTestId('passage-review-student')).not.toBeInTheDocument()
    expect(mockListStudents).not.toHaveBeenCalled()
  })

  // The pick is the confirm: choosing a child sends the ticked rows at once.
  it('lists the pinned class, and assigns the ticked rows plus the group passage on the pick', async () => {
    const user = userEvent.setup()
    const onAssign = vi.fn().mockResolvedValue(filed)
    render(<PassageReview passages={withGroup} classId={3} onAssign={onAssign} />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument()
    })
    expect(mockListStudents).toHaveBeenCalledWith(3, expect.anything())

    const eleonore = screen.getByRole('button', { name: 'Eleonore' })
    expect(eleonore).toBeDisabled()

    await user.click(screen.getAllByTestId('passage-review-check')[0])
    expect(eleonore).toBeEnabled()
    await user.click(eleonore)

    expect(onAssign).toHaveBeenCalledWith({
      classId: 3,
      studentId: 22,
      passages: [
        { kind: 'unknown', summary: 'She was helping the younger ones with their blocks.' },
        { kind: 'group', summary: 'Everyone worked hard.' },
      ],
    } satisfies AssignPassagesRequest)

    // The filed row is marked and can no longer be ticked; the other stays open.
    await waitFor(() => {
      expect(screen.getByTestId('passage-review-filed')).toHaveTextContent('Assigned to Eleonore')
    })
    const checks = screen.getAllByTestId('passage-review-check')
    expect(checks[0]).toBeDisabled()
    expect(checks[1]).toBeEnabled()
    expect(checks[1]).not.toBeChecked()
  })

  // Two ticked rows go in transcript order whatever order they were ticked
  // in, and both are marked filed to the same child.
  it('files several rows as one call, in transcript order', async () => {
    const user = userEvent.setup()
    const onAssign = vi.fn().mockResolvedValue(filed)
    render(<PassageReview passages={note694} classId={3} onAssign={onAssign} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())

    const checks = screen.getAllByTestId('passage-review-check')
    await user.click(checks[1])
    await user.click(checks[0])
    await user.click(screen.getByRole('button', { name: 'Eleonore' }))

    expect(onAssign.mock.calls[0][0].passages.map((p: { summary: string }) => p.summary)).toEqual([
      'She was helping the younger ones with their blocks.',
      "Polly wasn't speaking much today.",
    ])
    await waitFor(() => expect(screen.getAllByTestId('passage-review-filed')).toHaveLength(2))
    // Every row assigned: nothing left to pick, so the picker goes.
    expect(screen.queryByTestId('passage-review-student')).not.toBeInTheDocument()
  })

  // A failed call leaves the row ticked and says why; the teacher tries again.
  it('keeps a failed row selectable and shows the error', async () => {
    const user = userEvent.setup()
    const onAssign = vi.fn().mockRejectedValue(new Error('Already filing this recording, try again.'))
    render(<PassageReview passages={note694} classId={3} onAssign={onAssign} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())

    await user.click(screen.getAllByTestId('passage-review-check')[0])
    await user.click(screen.getByRole('button', { name: 'Eleonore' }))

    await waitFor(() => {
      expect(screen.getByTestId('passage-review-error')).toHaveTextContent('Already filing this recording, try again.')
    })
    expect(screen.queryByTestId('passage-review-filed')).not.toBeInTheDocument()
    expect(screen.getAllByTestId('passage-review-check')[0]).toBeChecked()
    expect(screen.getAllByTestId('passage-review-check')[0]).toBeEnabled()
    // The picker is back on its prompt, ready for the next pick.
    expect(screen.getByRole('button', { name: 'Eleonore' })).toBeEnabled()
    expect(screen.getByTestId('passage-review-prompt')).toHaveTextContent('Assign this row to…')
  })

  // The honest mistake (#138): the wrong child picked, undone from the row.
  // Both rows went to Eleonore — a create, then an append onto that note — so
  // they sit on one note and come back together, open and unticked.
  it('undoes an assignment from the row, reopening every row filed to that child', async () => {
    const user = userEvent.setup()
    const onAssign = vi.fn()
      .mockResolvedValueOnce(filed)
      .mockResolvedValueOnce({ ...filed, appended: true })
    let release: () => void = () => {}
    const onUndo = vi.fn().mockImplementation(() => new Promise<void>(resolve => { release = resolve }))
    render(<PassageReview passages={note694} classId={3} onAssign={onAssign} onUndo={onUndo} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())

    await user.click(screen.getAllByTestId('passage-review-check')[0])
    await user.click(screen.getByRole('button', { name: 'Eleonore' }))
    await waitFor(() => expect(screen.getAllByTestId('passage-review-filed')).toHaveLength(1))
    await user.click(screen.getAllByTestId('passage-review-check')[1])
    await user.click(screen.getByRole('button', { name: 'Eleonore' }))
    await waitFor(() => expect(screen.getAllByTestId('passage-review-filed')).toHaveLength(2))
    expect(screen.getAllByTestId('passage-review-undo')).toHaveLength(2)

    await user.click(screen.getAllByTestId('passage-review-undo')[1])
    expect(onUndo).toHaveBeenCalledWith(22)
    // Dead for the round trip, and it says so; the picker is dead too, but
    // must not claim to be assigning.
    expect(screen.getAllByTestId('passage-review-undo')[0]).toBeDisabled()
    expect(screen.getAllByTestId('passage-review-undo')[1]).toHaveTextContent('Undoing…')
    expect(screen.getAllByRole('button', { name: 'Undo the assignment to Eleonore' })).toHaveLength(2)

    release()
    await waitFor(() => expect(screen.queryByTestId('passage-review-filed')).not.toBeInTheDocument())
    const checks = screen.getAllByTestId('passage-review-check')
    expect(checks).toHaveLength(2)
    for (const check of checks) {
      expect(check).toBeEnabled()
      expect(check).not.toBeChecked()
    }
    expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument()
  })

  // A row that joined a note the card already held — the pipeline's — is not
  // this tab's to take back: the server would say 404. No control, and the
  // teacher edits that note instead.
  it('offers no undo on a row appended to a note this tab did not make', async () => {
    const user = userEvent.setup()
    const onAssign = vi.fn().mockResolvedValue({ noteId: 50, studentId: 21, name: 'Lévy', className: 'Tuesday', appended: true })
    render(<PassageReview passages={note694} classId={3} onAssign={onAssign} onUndo={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Lévy' })).toBeInTheDocument())

    await user.click(screen.getAllByTestId('passage-review-check')[0])
    await user.click(screen.getByRole('button', { name: 'Lévy' }))
    await waitFor(() => expect(screen.getByTestId('passage-review-filed')).toHaveTextContent('Assigned to Lévy'))
    expect(screen.queryByTestId('passage-review-undo')).not.toBeInTheDocument()
  })

  it('shows no undo without an undo callback', async () => {
    const user = userEvent.setup()
    render(<PassageReview passages={note694} classId={3} onAssign={vi.fn().mockResolvedValue(filed)} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())
    await user.click(screen.getAllByTestId('passage-review-check')[0])
    await user.click(screen.getByRole('button', { name: 'Eleonore' }))
    await waitFor(() => expect(screen.getByTestId('passage-review-filed')).toBeInTheDocument())
    expect(screen.queryByTestId('passage-review-undo')).not.toBeInTheDocument()
  })

  // A failed undo leaves the row assigned and says why.
  it('keeps the row assigned and shows the error when the undo fails', async () => {
    const user = userEvent.setup()
    const onUndo = vi.fn().mockRejectedValue(new Error('Already filing this recording, try again.'))
    render(<PassageReview passages={note694} classId={3} onAssign={vi.fn().mockResolvedValue(filed)} onUndo={onUndo} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())
    await user.click(screen.getAllByTestId('passage-review-check')[0])
    await user.click(screen.getByRole('button', { name: 'Eleonore' }))
    await waitFor(() => expect(screen.getByTestId('passage-review-undo')).toBeInTheDocument())

    await user.click(screen.getByTestId('passage-review-undo'))
    await waitFor(() => {
      expect(screen.getByTestId('passage-review-error')).toHaveTextContent('Already filing this recording, try again.')
    })
    expect(screen.getByTestId('passage-review-filed')).toHaveTextContent('Assigned to Eleonore')
    expect(screen.getByTestId('passage-review-undo')).toBeEnabled()
    expect(screen.getAllByTestId('passage-review-check')[0]).toBeDisabled()
  })

  // The point of the split (#131): a row that lost its name usually belongs to
  // a child nobody has written about yet, so those come first. Lévy resolved
  // on this recording, so she is demoted — not dropped, because assigning to
  // her appends to the note she already has.
  it('puts the children who already have a note below the rest', async () => {
    render(<PassageReview passages={note694} classId={3} onAssign={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Lévy' })).toBeInTheDocument())

    const noted = screen.getByTestId('passage-review-noted')
    expect(noted).toHaveTextContent('Already has a note')
    expect(noted).toContainElement(screen.getByRole('button', { name: 'Lévy' }))
    expect(noted).not.toContainElement(screen.getByRole('button', { name: 'Eleonore' }))
  })

  // The common case: a recording that reached nobody. Every child is fresh,
  // so the band never appears and the roster reads as one list.
  it('shows no band when nobody has a note yet', async () => {
    const reachedNobody = note694.filter(p => !p.student)
    render(<PassageReview passages={reachedNobody} classId={3} onAssign={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Lévy' })).toBeInTheDocument())
    expect(screen.queryByTestId('passage-review-noted')).not.toBeInTheDocument()
    expect(screen.queryByText('Already has a note')).not.toBeInTheDocument()
  })

  // A child this tab filed to has a note now, and moves down for the next
  // pick — the teacher's remaining rows are most likely someone else's.
  it('demotes a child once this tab has filed to them', async () => {
    const user = userEvent.setup()
    render(<PassageReview passages={note694} classId={3} onAssign={vi.fn().mockResolvedValue(filed)} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())

    await user.click(screen.getAllByTestId('passage-review-check')[0])
    await user.click(screen.getByRole('button', { name: 'Eleonore' }))

    await waitFor(() => {
      expect(screen.getByTestId('passage-review-noted'))
        .toContainElement(screen.getByRole('button', { name: 'Eleonore' }))
    })
  })

  // Undo is only offered where this tab made the note, so an undone child had
  // no note before this tab and has none after: back up with the rest.
  it('brings a child back up when the assignment is undone', async () => {
    const user = userEvent.setup()
    render(
      <PassageReview
        passages={note694}
        classId={3}
        onAssign={vi.fn().mockResolvedValue(filed)}
        onUndo={vi.fn().mockResolvedValue(undefined)}
      />,
    )
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())
    await user.click(screen.getAllByTestId('passage-review-check')[0])
    await user.click(screen.getByRole('button', { name: 'Eleonore' }))
    await waitFor(() => expect(screen.getByTestId('passage-review-undo')).toBeInTheDocument())

    await user.click(screen.getByTestId('passage-review-undo'))
    await waitFor(() => {
      expect(screen.getByTestId('passage-review-noted'))
        .not.toContainElement(screen.getByRole('button', { name: 'Eleonore' }))
    })
  })

  // Every chip is dead until a row is ticked, and the prompt says what to do.
  it('holds the roster on screen, unpickable, until a row is ticked', async () => {
    const user = userEvent.setup()
    render(<PassageReview passages={note694} classId={3} onAssign={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())

    for (const chipEl of screen.getAllByTestId('passage-review-student')) {
      expect(chipEl).toBeDisabled()
    }
    expect(screen.getByTestId('passage-review-prompt')).toHaveTextContent('Tick a row, then pick a child')

    await user.click(screen.getAllByTestId('passage-review-check')[0])
    for (const chipEl of screen.getAllByTestId('passage-review-student')) {
      expect(chipEl).toBeEnabled()
    }
  })

  // The prompt names who the pick went to while the call runs, and the chip
  // stays lit so the teacher can see which one they hit.
  it('names the child on the prompt while the assignment runs', async () => {
    const user = userEvent.setup()
    let release: (v: AssignPassagesResponse) => void = () => {}
    const onAssign = vi.fn().mockImplementation(() => new Promise<AssignPassagesResponse>(res => { release = res }))
    render(<PassageReview passages={note694} classId={3} onAssign={onAssign} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())

    await user.click(screen.getAllByTestId('passage-review-check')[0])
    await user.click(screen.getByRole('button', { name: 'Eleonore' }))

    expect(screen.getByTestId('passage-review-prompt')).toHaveTextContent('Assigning to Eleonore…')
    expect(screen.getByRole('button', { name: 'Eleonore' })).toHaveClass('passage-review-student-going')
    expect(screen.getByRole('button', { name: 'Lévy' })).toBeDisabled()

    release(filed)
    await waitFor(() => expect(screen.getByTestId('passage-review-filed')).toBeInTheDocument())
  })

  // A pinned card that named nobody shows the class picker and these controls
  // together. Picking another class must swap the roster: a child of the old
  // class sent with the new class id is a 404.
  it('swaps the roster when the class changes', async () => {
    const user = userEvent.setup()
    mockListStudents.mockImplementation(async (classId: number) => ({
      students: classId === 3
        ? [{ id: 22, classId: 3, name: 'Eleonore', createdAt: '', aliases: [] }]
        : [{ id: 31, classId: 4, name: 'Ombeline', createdAt: '', aliases: [] }],
    }))
    const { rerender } = render(<PassageReview passages={note694} classId={3} onAssign={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())
    await user.click(screen.getAllByTestId('passage-review-check')[0])
    expect(screen.getByRole('button', { name: 'Eleonore' })).toBeEnabled()

    rerender(<PassageReview passages={note694} classId={4} onAssign={vi.fn()} />)
    expect(screen.queryByRole('button', { name: 'Eleonore' })).not.toBeInTheDocument()
    // Nobody to pick until the new roster lands, tick or no tick: the prompt
    // holds the line on its own.
    expect(screen.queryByTestId('passage-review-student')).not.toBeInTheDocument()
    expect(screen.getByTestId('passage-review-prompt')).toHaveTextContent('Loading the class…')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Ombeline' })).toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Ombeline' })).toBeEnabled()
    expect(mockListStudents).toHaveBeenLastCalledWith(4, expect.anything())
  })

  // A class pick replaces the rows, and pass 2 is non-deterministic: a tick
  // on the old list must not carry over to whatever text sits at that index
  // now. The same rows coming back from the poll keep their ticks.
  it('starts over when the rows change, and keeps its ticks when they do not', async () => {
    const user = userEvent.setup()
    const { rerender } = render(<PassageReview passages={note694} classId={3} onAssign={vi.fn()} />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Eleonore' })).toBeInTheDocument())
    await user.click(screen.getAllByTestId('passage-review-check')[0])

    rerender(<PassageReview passages={[...note694]} classId={3} onAssign={vi.fn()} />)
    expect(screen.getAllByTestId('passage-review-check')[0]).toBeChecked()

    rerender(<PassageReview passages={[
      { kind: 'unknown', summary: 'Someone tidied up without being asked.' },
      { kind: 'unknown', summary: "Polly wasn't speaking much today." },
    ]} classId={3} onAssign={vi.fn()} />)
    for (const check of screen.getAllByTestId('passage-review-check')) {
      expect(check).not.toBeChecked()
    }
  })
})
