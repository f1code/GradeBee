import { useEffect, useState } from 'react'
import { useAuth } from '@clerk/react'
import { listStudents } from '../api'
import type { AssignPassagesRequest, AssignPassagesResponse, StudentItem } from '../api'
import type { JobPassage } from '../api-types.gen'
import { PassageGroup } from '../api-types.gen'
import { isUnattributed } from '../lib/passages'

interface PassageReviewProps {
  passages: JobPassage[]
  /**
   * The class in force on the card, whose roster the picker offers. Absent on
   * a job done before the field existed, or when nothing was pinned: the rows
   * then stay read-only. A class pick brings it with the assemble response.
   */
  classId?: number
  /** Files the body and resolves to the note link made. Rejects to show its message. */
  onAssign?: (body: AssignPassagesRequest) => Promise<AssignPassagesResponse>
  /**
   * Takes back everything this card assigned to one child (#138). Resolves
   * once the note is gone; rejects to show its message.
   */
  onUndo?: (studentId: number) => Promise<void>
}

/** Who a filed row went to, and the key the undo is by. */
interface Filing {
  studentId: number
  name: string
}

/**
 * Lists what a done recording said that no note holds, and lets the teacher
 * assign rows to a child of the pinned class (#134): tick rows, pick a child,
 * and the pick is the confirm — one call, one note; repeat for the next
 * child. No separate button: a wrong pick is undone from the row (#138), so
 * the extra click bought nothing. Group passages on the card ride along on
 * every assignment, as they join every note the pipeline makes.
 *
 * Undo is of the assignment, not the row. Every row filed to a child in this
 * tab sits on one note — the first pick made it, the rest joined it — and the
 * server deletes that note, so undoing any of them reopens them all. It shows
 * only where this tab made the note: a row that joined a note the card
 * already held (the pipeline's, or one from before a reload) is not the
 * server's to take back, and the teacher edits that note instead.
 *
 * Takes a passage array and a callback. It knows nothing about jobs, so a
 * later inbox can feed it rows from a table instead of the card.
 *
 * Selection and assigned marks are keyed on the row's position and text, not
 * its position alone: a class pick replaces the rows, and pass 2 is
 * non-deterministic, so an index into the old list would point at other
 * text. The poll brings the same rows back under the same keys, so a tick
 * survives it; a pick's new rows match nothing, so the review starts over.
 * Nothing is stored anywhere but here: a refresh ends the review, by design.
 */
export default function PassageReview({ passages, classId, onAssign, onUndo }: PassageReviewProps) {
  const { getToken } = useAuth()
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [filed, setFiled] = useState<Map<string, Filing>>(new Map())
  // Children whose note this tab created, by a pick that said appended:false.
  // Only their assignments are undoable; see the component comment.
  const [created, setCreated] = useState<Set<number>>(new Set())
  // The roster remembers which class it belongs to, and counts only while
  // that is the class on the card. A class pick replaces the class (a pinned
  // card that named nobody shows the class picker and these controls
  // together), and a child of the old class sent with the new class id is a
  // 404 the teacher cannot read.
  const [roster, setRoster] = useState<{ classId: number; students: StudentItem[] } | null>(null)
  // The child an assignment is in flight to, so the prompt names them while
  // it runs and the chip they picked stays lit.
  const [assigning, setAssigning] = useState<number | null>(null)
  const [undoing, setUndoing] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  const canFile = classId !== undefined && onAssign !== undefined
  const students = roster !== null && roster.classId === classId ? roster.students : null
  const busy = assigning !== null || undoing !== null

  useEffect(() => {
    if (classId === undefined) return
    let live = true
    listStudents(classId, getToken)
      .then(({ students }) => {
        if (!live) return
        setRoster({ classId, students })
        // A roster that failed to load for the previous class said so here;
        // this one loaded, and the line must not outlive the failure.
        setError(null)
      })
      .catch((err: unknown) => {
        if (live) setError(err instanceof Error ? err.message : 'Could not load the class')
      })
    return () => { live = false }
  }, [classId, getToken])

  const rows = passages
    .map((p, i) => ({ p, key: `${i}:${p.summary}` }))
    .filter(({ p }) => isUnattributed(p))
  if (rows.length === 0) return null

  const open = rows.filter(r => !filed.has(r.key))
  const chosen = open.filter(r => selected.has(r.key))

  function toggle(key: string) {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  async function assignTo(studentId: number) {
    if (!canFile || chosen.length === 0 || busy) return
    // The ticked rows in transcript order, then every group passage on the
    // card. The server orders group text last whatever order it arrives in.
    const body: AssignPassagesRequest = {
      classId,
      studentId,
      passages: [
        ...chosen.map(r => ({ kind: r.p.kind, summary: r.p.summary })),
        ...passages.filter(p => p.kind === PassageGroup).map(p => ({ kind: p.kind, summary: p.summary })),
      ],
    }
    setAssigning(studentId)
    setError(null)
    try {
      const link = await onAssign(body)
      const filing: Filing = { studentId: link.studentId, name: link.name }
      setFiled(prev => new Map([...prev, ...chosen.map(r => [r.key, filing] as const)]))
      if (!link.appended) setCreated(prev => new Set(prev).add(link.studentId))
      setSelected(new Set())
    } catch (err) {
      // The rows stay ticked: the teacher's next move is to pick again.
      setError(err instanceof Error ? err.message : 'Could not assign the passage')
    } finally {
      setAssigning(null)
    }
  }

  async function undo(studentId: number) {
    if (onUndo === undefined || busy) return
    setUndoing(studentId)
    setError(null)
    try {
      await onUndo(studentId)
      setFiled(prev => new Map([...prev].filter(([, f]) => f.studentId !== studentId)))
      setCreated(prev => {
        const next = new Set(prev)
        next.delete(studentId)
        return next
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not undo the assignment')
    } finally {
      setUndoing(null)
    }
  }

  // Children this recording already wrote to: the ones a passage reached, and
  // the ones this tab filed. They stay pickable — a row that lost its name is
  // often about a child already named, and the pick appends to that note —
  // but they sit below the children nobody has written to yet, which is where
  // a lost row usually belongs.
  const filedIds = new Set([...filed.values()].map(f => f.studentId))
  const noteNames = new Set(passages.map(p => p.student).filter(n => n))
  const hasNote = (s: StudentItem) => filedIds.has(s.id) || noteNames.has(s.name)
  const fresh = (students ?? []).filter(s => !hasNote(s))
  const noted = (students ?? []).filter(hasNote)

  const going = assigning === null ? undefined : students?.find(s => s.id === assigning)
  const prompt = students === null ? 'Loading the class…'
    // The class picker offers every class the teacher owns, so a class with
    // no children on its roster is one pick away.
    : students.length === 0 ? 'This class has no children yet.'
      : going ? `Assigning to ${going.name}…`
        : chosen.length === 0 ? 'Tick a row, then pick a child'
          : chosen.length === 1 ? 'Assign this row to…'
            : `Assign ${chosen.length} rows to…`

  function chip(s: StudentItem) {
    return (
      <button
        key={s.id}
        type="button"
        className={`btn-secondary passage-review-student${assigning === s.id ? ' passage-review-student-going' : ''}`}
        onClick={() => assignTo(s.id)}
        disabled={busy || chosen.length === 0}
        data-testid="passage-review-student"
      >
        {s.name}
      </button>
    )
  }

  return (
    <div className="passage-review" data-testid="passage-review">
      <p className="passage-review-prompt">AI couldn't find the student for these:</p>
      <ul className="passage-review-list">
        {rows.map(({ p, key }) => {
          const to = filed.get(key)
          return (
            <li key={key} className={`passage-review-row${to ? ' passage-review-row-filed' : ''}`} data-testid="passage-review-row">
              {canFile ? (
                <label className="passage-review-pick">
                  <input
                    type="checkbox"
                    checked={selected.has(key)}
                    disabled={busy || to !== undefined}
                    onChange={() => toggle(key)}
                    data-testid="passage-review-check"
                  />
                  <span className="passage-review-text">{p.summary}</span>
                </label>
              ) : (
                <span className="passage-review-text">{p.summary}</span>
              )}
              {to && (
                <span className="passage-review-filed" data-testid="passage-review-filed">
                  Assigned to {to.name}
                  {onUndo !== undefined && created.has(to.studentId) && (
                    <button
                      type="button"
                      className="passage-review-undo"
                      onClick={() => undo(to.studentId)}
                      disabled={busy}
                      aria-label={`Undo the assignment to ${to.name}`}
                      data-testid="passage-review-undo"
                    >
                      {undoing === to.studentId ? 'Undoing…' : 'Undo'}
                    </button>
                  )}
                </span>
              )}
            </li>
          )
        })}
      </ul>
      {canFile && open.length > 0 && (
        <div className="passage-review-controls">
          <p className="passage-review-prompt" data-testid="passage-review-prompt">{prompt}</p>
          {fresh.length > 0 && (
            <div className="passage-review-students" role="group" aria-label="Assign the ticked rows to">
              {fresh.map(chip)}
            </div>
          )}
          {noted.length > 0 && (
            <div className="passage-review-noted" data-testid="passage-review-noted">
              <p className="passage-review-noted-label">Already has a note</p>
              <div className="passage-review-students" role="group" aria-label="Assign the ticked rows to a child who already has a note">
                {noted.map(chip)}
              </div>
            </div>
          )}
        </div>
      )}
      {error && <p className="passage-review-error" data-testid="passage-review-error">{error}</p>}
    </div>
  )
}
