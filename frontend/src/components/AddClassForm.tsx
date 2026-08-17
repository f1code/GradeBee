import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@clerk/react'
import { motion } from 'motion/react'
import { createClass, listLevels, type ClassItem, type LevelItem } from '../api'
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
  const [scheduleName, setScheduleName] = useState('')
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
    if (!levelId || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      const cls = await createClass(levelId, scheduleName.trim(), getToken)
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
      <motion.div
        className="add-class-form"
        initial={{ opacity: 0, y: -8 }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: -8 }}
        transition={{ duration: 0.2 }}
      >
        <p className="add-class-hint" data-testid="add-class-no-levels">
          There are no Levels yet — ask an Admin to add one before creating a class.
        </p>
        {onCancel && (
          <button type="button" className="btn-secondary" onClick={onCancel} data-testid="add-class-cancel">
            Cancel
          </button>
        )}
      </motion.div>
    )
  }

  return (
    <motion.div
      className="add-class-form"
      initial={{ opacity: 0, y: -8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -8 }}
      transition={{ duration: 0.2 }}
    >
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
          <input
            type="text"
            value={scheduleName}
            onChange={e => setScheduleName(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Schedule (optional)"
            disabled={submitting}
            className="add-class-input"
            data-testid="add-class-schedule-input"
          />
        </div>
        <p className="add-class-hint" data-testid="add-class-hint">
          <strong>Schedule</strong> is optional and groups classes by schedule slot
          (e.g. "Period 1"). The <strong>level</strong> identifies the
          class and is used to match report-card examples.
        </p>
        <div className="add-class-form-row">
          <button type="submit" disabled={submitting || !levelId} data-testid="add-class-submit">
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
    </motion.div>
  )
}
