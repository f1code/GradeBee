import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@clerk/react'
import { createClass, listLevels, WEEKDAYS, type ClassItem, type LevelItem } from '../api'
import InlineError from './InlineError'

interface AddClassFormProps {
  onCreated: (cls: ClassItem) => void
  onCancel?: () => void
}

type LevelsStatus = 'loading' | 'ready' | 'error'

export default function AddClassForm({ onCreated, onCancel }: AddClassFormProps) {
  const { getToken } = useAuth()
  const [levels, setLevels] = useState<LevelItem[]>([])
  const [levelsStatus, setLevelsStatus] = useState<LevelsStatus>('loading')
  const [levelId, setLevelId] = useState<number | ''>('')
  const [day, setDay] = useState('')
  const [timeSlot, setTimeSlot] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const selectRef = useRef<HTMLSelectElement>(null)

  useEffect(() => {
    listLevels(getToken)
      .then(({ levels: lvls }) => {
        setLevels(lvls || [])
        setLevelsStatus('ready')
      })
      .catch(() => setLevelsStatus('error'))
  }, [getToken])

  useEffect(() => {
    if (levelsStatus === 'ready' && levels.length > 0) {
      selectRef.current?.focus()
    }
  }, [levelsStatus, levels.length])

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!levelId || !day || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      const cls = await createClass(levelId, day, timeSlot.trim(), getToken)
      onCreated(cls)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create class')
    } finally {
      setSubmitting(false)
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      onCancel?.()
    }
  }

  if (levelsStatus === 'ready' && levels.length === 0) {
    return (
      <div
        className="info-box"
        data-testid="add-class-no-levels"
      >
        <h2>No Levels yet</h2>
        <p>Ask an Admin to add one before creating a class.</p>
        {onCancel && (
          <button type="button" className="btn-secondary" onClick={onCancel} data-testid="add-class-cancel">
            Cancel
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="add-class-form">
      <form onSubmit={handleSubmit} className="add-class-form-fields">
        <div className="add-class-field-group">
          <select
            ref={selectRef}
            value={levelId}
            onChange={e => setLevelId(e.target.value ? Number(e.target.value) : '')}
            onKeyDown={handleKeyDown}
            disabled={submitting || levelsStatus === 'loading'}
            className="add-class-input"
            data-testid="add-class-level-select"
          >
            <option value="" disabled>
              {levelsStatus === 'loading' ? 'Loading levels…' : 'Select a level'}
            </option>
            {levels.map(l => (
              <option key={l.id} value={l.id}>{l.name}</option>
            ))}
          </select>
          <select
            value={day}
            onChange={e => setDay(e.target.value)}
            onKeyDown={handleKeyDown}
            disabled={submitting}
            className="add-class-input"
            data-testid="add-class-day-select"
          >
            <option value="" disabled>Select a day</option>
            {WEEKDAYS.map(d => (
              <option key={d} value={d}>{d}</option>
            ))}
          </select>
          <input
            type="text"
            value={timeSlot}
            onChange={e => setTimeSlot(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Time slot (optional)"
            disabled={submitting}
            className="add-class-input"
            data-testid="add-class-time-slot-input"
          />
        </div>
        <p className="add-class-hint" data-testid="add-class-hint">
          Pick the <strong>day</strong> of the class's first meeting of the week if it
          meets more than once. <strong>Time slot</strong> is optional and distinguishes
          classes at the same level and day (e.g. "Period 1", "14:10"). The
          <strong> level</strong> identifies the class and drives its report style.
        </p>
        <div className="add-class-form-row">
          <button type="submit" disabled={submitting || !levelId || !day} data-testid="add-class-submit">
            {submitting ? 'Adding…' : 'Add'}
          </button>
          {onCancel && (
            <button type="button" className="btn-secondary" onClick={onCancel} data-testid="add-class-cancel">
              Cancel
            </button>
          )}
        </div>
      </form>
      {error && (
        <div data-testid="add-class-error">
          <InlineError onDismiss={() => setError(null)}>
            {error}
          </InlineError>
        </div>
      )}
    </div>
  )
}
