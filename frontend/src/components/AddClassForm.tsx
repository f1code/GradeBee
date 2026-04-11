import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@clerk/react'
import { motion } from 'motion/react'
import { createClass, type ClassItem } from '../api'

interface AddClassFormProps {
  onCreated: (cls: ClassItem) => void
  onCancel?: () => void
}

export default function AddClassForm({ onCreated, onCancel }: AddClassFormProps) {
  const { getToken } = useAuth()
  const [name, setName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed || submitting) return

    setSubmitting(true)
    setError(null)
    try {
      const cls = await createClass(trimmed, getToken)
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

  return (
    <motion.div
      className="add-class-form"
      initial={{ opacity: 0, y: -8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -8 }}
      transition={{ duration: 0.2 }}
    >
      <form onSubmit={handleSubmit} className="add-class-form-row">
        <input
          ref={inputRef}
          type="text"
          value={name}
          onChange={e => setName(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Class name"
          disabled={submitting}
          className="add-class-input"
          data-testid="add-class-input"
        />
        <button type="submit" disabled={submitting || !name.trim()} className="btn-primary" data-testid="add-class-submit">
          {submitting ? 'Adding…' : 'Add'}
        </button>
        {onCancel && (
          <button type="button" className="btn-secondary" onClick={onCancel} data-testid="add-class-cancel">
            Cancel
          </button>
        )}
      </form>
      {error && <p className="add-form-error" data-testid="add-class-error">{error}</p>}
    </motion.div>
  )
}
