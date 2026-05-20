import { useState } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import { addAlias, removeAlias, AliasConflictError, type AliasResponse } from '../api'
import InlineError from './InlineError'

interface StudentAliasesProps {
  studentId: number
  /** Initial alias list — {id, alias} pairs from the student response */
  initialAliases: AliasResponse[]
}

export default function StudentAliases({ studentId, initialAliases }: StudentAliasesProps) {
  const { getToken } = useAuth()
  const [aliases, setAliases] = useState<AliasResponse[]>(initialAliases)
  const [adding, setAdding] = useState(false)
  const [input, setInput] = useState('')
  const [saving, setSaving] = useState(false)
  const [removingId, setRemovingId] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [conflictStudentName, setConflictStudentName] = useState<string | null>(null)
  // Snapshot of the alias that triggered a conflict error (not live input)
  const [conflictAlias, setConflictAlias] = useState<string | null>(null)

  async function handleAdd() {
    const trimmed = input.trim()
    if (!trimmed) return
    setSaving(true)
    setError(null)
    setConflictStudentName(null)
    setConflictAlias(null)
    try {
      const a = await addAlias(studentId, trimmed, getToken)
      setAliases(prev => [...prev, a])
      setInput('')
      setAdding(false)
    } catch (err) {
      if (err instanceof AliasConflictError) {
        setConflictAlias(trimmed)
        setConflictStudentName(err.conflictStudentName || null)
        setError(err.message)
      } else {
        setError(err instanceof Error ? err.message : 'Failed to add alias')
      }
    } finally {
      setSaving(false)
    }
  }

  async function handleRemove(aliasId: number) {
    setRemovingId(aliasId)
    setError(null)
    setConflictStudentName(null)
    setConflictAlias(null)
    try {
      await removeAlias(studentId, aliasId, getToken)
      setAliases(prev => prev.filter(a => a.id !== aliasId))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove alias')
    } finally {
      setRemovingId(null)
    }
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') { e.preventDefault(); handleAdd() }
    if (e.key === 'Escape') { setAdding(false); setInput(''); setError(null); setConflictStudentName(null); setConflictAlias(null) }
  }

  function handleDismissError() {
    setError(null)
    setConflictStudentName(null)
    setConflictAlias(null)
  }

  return (
    <div className="student-aliases">
      <div className="student-aliases-chips">
        <AnimatePresence initial={false}>
          {aliases.map((a, i) => (
            <motion.span
              key={a.id}
              className="alias-chip"
              initial={{ opacity: 0, scale: 0.85 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.85 }}
              transition={{ duration: 0.15, delay: i * 0.04 }}
            >
              {removingId === a.id
                ? <span className="alias-chip-removing">…</span>
                : <>
                    <span className="alias-chip-text">{a.alias}</span>
                    <button
                      className="alias-chip-remove"
                      onClick={() => handleRemove(a.id)}
                      aria-label={`Remove alias ${a.alias}`}
                      disabled={removingId !== null}
                    >✕</button>
                  </>
              }
            </motion.span>
          ))}
        </AnimatePresence>

        {adding ? (
          <span className="alias-add-input-wrap">
            <input
              className="alias-add-input"
              value={input}
              onChange={e => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="e.g. Alex"
              autoFocus
              disabled={saving}
              maxLength={80}
            />
            <button
              className="alias-add-confirm"
              onClick={handleAdd}
              disabled={saving || !input.trim()}
              aria-label="Confirm alias"
            >{saving ? '…' : '✓'}</button>
            <button
              className="alias-add-cancel"
              onClick={() => { setAdding(false); setInput(''); handleDismissError() }}
              aria-label="Cancel"
            >✕</button>
          </span>
        ) : (
          <button
            className="alias-add-pill"
            onClick={() => { setAdding(true); handleDismissError() }}
            aria-label="Add alias"
          >+ alias</button>
        )}
      </div>

      <AnimatePresence>
        {error && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
          >
            {conflictStudentName ? (
              <InlineError title={`"${conflictAlias}"`} onDismiss={handleDismissError}>
                is already used by {conflictStudentName} in this class. Aliases must be unique per class (case-insensitive).
              </InlineError>
            ) : (
              <InlineError onDismiss={handleDismissError}>{error}</InlineError>
            )}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
