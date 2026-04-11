import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@clerk/react'
import { createStudent, type StudentItem } from '../api'

interface AddStudentFormProps {
  classId: number
  onCreated: (student: StudentItem) => void
}

export default function AddStudentForm({ classId, onCreated }: AddStudentFormProps) {
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
      const student = await createStudent(classId, trimmed, getToken)
      setName('')
      setError(null)
      onCreated(student)
      // Keep focus for rapid entry
      requestAnimationFrame(() => inputRef.current?.focus())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add student')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="add-student-form">
      <form onSubmit={handleSubmit} className="add-student-form-row">
        <input
          ref={inputRef}
          type="text"
          value={name}
          onChange={e => setName(e.target.value)}
          placeholder="Student name"
          disabled={submitting}
          className="add-student-input"
          data-testid="add-student-input"
        />
        <button type="submit" disabled={submitting || !name.trim()} className="btn-primary btn-sm" data-testid="add-student-submit">
          {submitting ? '…' : 'Add'}
        </button>
      </form>
      {error && <p className="add-form-error" data-testid="add-student-error">{error}</p>}
    </div>
  )
}
