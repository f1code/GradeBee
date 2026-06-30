import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@clerk/react'
import { motion } from 'motion/react'
import { createClass, listLevelNames, type ClassItem } from '../api'
import InlineError from './InlineError'

interface AddClassFormProps {
  onCreated: (cls: ClassItem) => void
  onCancel?: () => void
}

export default function AddClassForm({ onCreated, onCancel }: AddClassFormProps) {
  const { getToken } = useAuth()
  const [levelName, setLevelName] = useState('')
  const [scheduleName, setScheduleName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [suggestions, setSuggestions] = useState<string[]>([])
  const [allLevelNames, setAllLevelNames] = useState<string[]>([])
  const [showSuggestions, setShowSuggestions] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
    listLevelNames(getToken).then(({ levelNames }) => setAllLevelNames(levelNames)).catch(() => {})
  }, [getToken])

  function handleLevelNameChange(val: string) {
    setLevelName(val)
    if (val.trim()) {
      const lower = val.toLowerCase()
      const filtered = allLevelNames.filter(n => n.toLowerCase().includes(lower))
      setSuggestions(filtered)
      setShowSuggestions(filtered.length > 0)
    } else {
      setSuggestions([])
      setShowSuggestions(false)
    }
  }

  function pickSuggestion(name: string) {
    setLevelName(name)
    setSuggestions([])
    setShowSuggestions(false)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = levelName.trim()
    if (!trimmed || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      const cls = await createClass(trimmed, scheduleName.trim(), getToken)
      onCreated(cls)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create class')
    } finally {
      setSubmitting(false)
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      if (showSuggestions) {
        setShowSuggestions(false)
      } else {
        onCancel?.()
      }
    }
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
          <div className="add-class-autocomplete-wrapper">
            <input
              ref={inputRef}
              type="text"
              value={levelName}
              onChange={e => handleLevelNameChange(e.target.value)}
              onKeyDown={handleKeyDown}
              onFocus={() => {
                if (suggestions.length > 0) setShowSuggestions(true)
              }}
              onBlur={() => setTimeout(() => setShowSuggestions(false), 150)}
              placeholder="Level"
              disabled={submitting}
              className="add-class-input"
              data-testid="add-class-input"
              autoComplete="off"
            />
            {showSuggestions && (
              <ul className="add-class-suggestions">
                {suggestions.map(s => (
                  <li key={s} onMouseDown={() => pickSuggestion(s)} className="add-class-suggestion-item">
                    {s}
                  </li>
                ))}
              </ul>
            )}
          </div>
          <input
            type="text"
            value={scheduleName}
            onChange={e => setScheduleName(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Schedule (optional)"
            disabled={submitting}
            className="add-class-input"
            data-testid="add-class-group-input"
          />
        </div>
        <p className="add-class-hint" data-testid="add-class-hint">
          <strong>Schedule</strong> is optional and groups classes by schedule slot
          (e.g. "Period 1"). The <strong>level</strong> identifies the
          class and is used to match report-card examples.
        </p>
        <div className="add-class-form-row">
          <button type="submit" disabled={submitting || !levelName.trim()} data-testid="add-class-submit">
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
