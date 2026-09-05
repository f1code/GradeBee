import { useEffect, useState } from 'react'
import { useAuth } from '@clerk/react'
import { listClasses } from '../api'
import type { ClassItem } from '../api'

interface ClassPickerProps {
  /** Called with the class the teacher picked. Rejects to show its message. */
  onPick: (className: string) => Promise<void>
}

/**
 * The way out of a recording that reached no child (#115). `(level, day)` does
 * not identify a class for every teacher, so a correctly spoken header can
 * still leave the extraction reading the recording against the wrong roster —
 * and then no name in it resolves to anybody.
 *
 * Every class the teacher owns is listed, not a guess at the likely ones: the
 * extraction has already chosen wrong once, and a shortlist built from the
 * same signal would be the guess this whole path exists to avoid.
 */
export default function ClassPicker({ onPick }: ClassPickerProps) {
  const { getToken } = useAuth()
  const [classes, setClasses] = useState<ClassItem[] | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    listClasses(getToken)
      .then(({ classes }) => { if (live) setClasses(classes) })
      .catch((err: unknown) => {
        if (live) setError(err instanceof Error ? err.message : 'Could not load your classes')
      })
    return () => { live = false }
  }, [getToken])

  async function pick(name: string) {
    setBusy(name)
    setError(null)
    try {
      await onPick(name)
    } catch (err) {
      // The picker stays: the teacher's next move is to try again or pick
      // another class, and removing it would leave them with the same card and
      // no way forward.
      setError(err instanceof Error ? err.message : 'Could not create the notes')
    } finally {
      // Always, not only on the error path. A pick that succeeds but resolves
      // nobody leaves this component mounted, and a busy flag left set there
      // disables every option — locking the teacher out of the retry that case
      // exists to allow.
      setBusy(null)
    }
  }

  return (
    <div className="class-picker" data-testid="class-picker">
      <p className="class-picker-prompt">Which class was this?</p>
      {error && <p className="class-picker-error" data-testid="class-picker-error">{error}</p>}
      {classes === null && !error && <p className="class-picker-prompt">Loading your classes…</p>}
      {classes !== null && classes.length === 0 && (
        <p className="class-picker-prompt">You have no classes yet.</p>
      )}
      <div className="class-picker-options">
        {classes?.map((c) => (
          <button
            key={c.id}
            className="btn-secondary class-picker-option"
            onClick={() => pick(c.name)}
            disabled={busy !== null}
            data-testid="class-picker-option"
          >
            {busy === c.name ? 'Creating notes…' : c.name}
          </button>
        ))}
      </div>
    </div>
  )
}
