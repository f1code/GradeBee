import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@clerk/react'
import { motion } from 'motion/react'
import { createClass, listClassNames, type ClassItem } from '../api'

interface AddClassFormProps {
  onCreated: (cls: ClassItem) => void
  onCancel?: () => void
}

export default function AddClassForm({ onCreated, onCancel }: AddClassFormProps) {
  const { getToken } = useAuth()
  const [className, setClassName] = useState('')
  const [group, setGroup] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [suggestions, setSuggestions] = useState<string[]>([])
  const [allClassNames, setAllClassNames] = useState<string[]>([])
  const [showSuggestions, setShowSuggestions] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
    listClassNames(getToken).then(({ classNames }) => setAllClassNames(classNames)).catch(() => {})
  }, [getToken])

  function handleClassNameChange(val: string) {
    setClassName(val)
    if (val.trim()) {
      const lower = val.toLowerCase()
      const filtered = allClassNames.filter(n => n.toLowerCase().includes(lower))
      setSuggestions(filtered)
      setShowSuggestions(filtered.length > 0)
    } else {
      setSuggestions([])
      setShowSuggestions(false)
    }
  }

  function pickSuggestion(name: string) {
    setClassName(name)
    setSuggestions([])
    setShowSuggestions(false)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = className.trim()
    if (!trimmed || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      const cls = await createClass(trimmed, group.trim(), getToken)
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
              value={className}
              onChange={e => handleClassNameChange(e.target.value)}
              onKeyDown={handleKeyDown}
              onFocus={() => {
                if (suggestions.length > 0) setShowSuggestions(true)
              }}
              onBlur={() => setTimeout(() => setShowSuggestions(false), 150)}
              placeholder="Class name"
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
            value={group}
            onChange={e => setGroup(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Group (optional)"
            disabled={submitting}
            className="add-class-input"
            data-testid="add-class-group-input"
          />
        </div>
        <div className="add-class-form-row">
          <button type="submit" disabled={submitting || !className.trim()} data-testid="add-class-submit">
            {submitting ? 'Adding…' : 'Add'}
          </button>
          {onCancel && (
            <button type="button" className="btn-secondary" onClick={onCancel} data-testid="add-class-cancel">
              Cancel
            </button>
          )}
        </div>
      </form>
      {error && <p className="add-form-error" data-testid="add-class-error">{error}</p>}
    </motion.div>
  )
}
