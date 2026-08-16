import { useState, useEffect, useCallback, useRef } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import {
  listLevels,
  createLevel,
  renameLevel,
  updateLevelReportInstructions,
  deleteLevel,
  type LevelItem,
} from '../api'
import InlineError from './InlineError'
import InlineEdit from './InlineEdit'
import ItemRow from './ItemRow'
import { HexBullet } from './Icons'

type Status = 'loading' | 'error' | 'ready'

export default function LevelsAdmin() {
  const { getToken } = useAuth()
  const [status, setStatus] = useState<Status>('loading')
  const [levels, setLevels] = useState<LevelItem[]>([])
  const [error, setError] = useState<string | null>(null)
  const [showAdd, setShowAdd] = useState(false)
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [flashError, setFlashError] = useState<string | null>(null)
  const flashTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const fetchLevels = useCallback(async () => {
    setStatus('loading')
    setError(null)
    try {
      const { levels: lvls } = await listLevels(getToken)
      setLevels(lvls || [])
      setStatus('ready')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Levels')
      setStatus('error')
    }
  }, [getToken])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchLevels()
  }, [fetchLevels])

  useEffect(() => {
    return () => clearTimeout(flashTimer.current)
  }, [])

  function showFlash(msg: string) {
    setFlashError(msg)
    clearTimeout(flashTimer.current)
    flashTimer.current = setTimeout(() => setFlashError(null), 3000)
  }

  function handleCreated(level: LevelItem) {
    setLevels(prev => [...prev, level].sort((a, b) => a.name.localeCompare(b.name)))
    setShowAdd(false)
  }

  async function handleRename(id: number, newName: string) {
    const old = levels.find(l => l.id === id)
    if (!old || newName === old.name) {
      setEditingId(null)
      return
    }
    setLevels(prev => prev.map(l => l.id === id ? { ...l, name: newName } : l).sort((a, b) => a.name.localeCompare(b.name)))
    setEditingId(null)
    try {
      await renameLevel(id, newName, getToken)
    } catch (err) {
      setLevels(prev => prev.map(l => l.id === id ? { ...l, name: old.name } : l).sort((a, b) => a.name.localeCompare(b.name)))
      showFlash(err instanceof Error ? err.message : 'Failed to rename Level')
    }
  }

  async function handleSaveInstructions(id: number, instructions: string) {
    try {
      const updated = await updateLevelReportInstructions(id, instructions, getToken)
      setLevels(prev => prev.map(l => l.id === id ? updated : l))
    } catch (err) {
      showFlash(err instanceof Error ? err.message : 'Failed to save Report Instructions')
      throw err
    }
  }

  async function handleDelete(id: number) {
    try {
      await deleteLevel(id, getToken)
      setLevels(prev => prev.filter(l => l.id !== id))
      if (expandedId === id) setExpandedId(null)
    } catch (err) {
      showFlash(err instanceof Error ? err.message : 'Failed to delete Level')
    }
  }

  if (status === 'loading') {
    return (
      <div className="levels-admin" data-testid="levels-admin-loading">
        <div className="honeycomb-spinner">
          <div className="hex" /><div className="hex" /><div className="hex" />
        </div>
      </div>
    )
  }

  if (status === 'error') {
    return (
      <motion.div
        className="levels-admin student-list-error"
        data-testid="levels-admin-error"
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.35 }}
      >
        <h2>Error</h2>
        <p>{error}</p>
        <button onClick={fetchLevels}>Retry</button>
      </motion.div>
    )
  }

  return (
    <div className="levels-admin" data-testid="levels-admin">
      <div className="levels-admin-header">
        <h2>Levels</h2>
        {!showAdd && (
          <button className="btn-sm" onClick={() => setShowAdd(true)}>+ Add Level</button>
        )}
      </div>
      <p className="levels-admin-help">
        Levels are shared across your Group. Report Instructions guide how GradeBee writes report cards for each Level.
      </p>

      <AnimatePresence>
        {showAdd && (
          <AddLevelForm
            existingNames={levels.map(l => l.name)}
            onCreated={handleCreated}
            onCancel={() => setShowAdd(false)}
          />
        )}
      </AnimatePresence>

      {levels.length === 0 && !showAdd ? (
        <div className="info-box">
          <h2>No Levels yet</h2>
          <p>Add your Group's first Level to get started.</p>
        </div>
      ) : (
        <ul className="levels-admin-list">
          {levels.map(level => (
            <li key={level.id}>
              {editingId === level.id ? (
                <div className="inline-edit-group inline-class-edit">
                  <InlineEdit
                    value={level.name}
                    onSave={newName => handleRename(level.id, newName)}
                    onCancel={() => setEditingId(null)}
                  />
                </div>
              ) : (
                <ItemRow
                  name={level.name}
                  badge={<HexBullet />}
                  expanded={expandedId === level.id}
                  onToggle={() => setExpandedId(prev => prev === level.id ? null : level.id)}
                  onDelete={() => handleDelete(level.id)}
                  actions={
                    <button
                      className="icon-btn"
                      onClick={e => { e.stopPropagation(); setEditingId(level.id) }}
                      aria-label={`Rename ${level.name}`}
                    >
                      ✎
                    </button>
                  }
                >
                  <ReportInstructionsEditor
                    level={level}
                    onSave={instructions => handleSaveInstructions(level.id, instructions)}
                  />
                </ItemRow>
              )}
            </li>
          ))}
        </ul>
      )}

      {flashError && <div className="flash-error">{flashError}</div>}
    </div>
  )
}

function AddLevelForm({
  existingNames,
  onCreated,
  onCancel,
}: {
  existingNames: string[]
  onCreated: (level: LevelItem) => void
  onCancel: () => void
}) {
  const { getToken } = useAuth()
  const [name, setName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [duplicateError, setDuplicateError] = useState<string | null>(null)
  const [apiError, setApiError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    if (existingNames.some(n => n.toLowerCase() === trimmed.toLowerCase())) {
      setDuplicateError(trimmed)
      return
    }
    setSubmitting(true)
    setDuplicateError(null)
    setApiError(null)
    try {
      const level = await createLevel(trimmed, getToken)
      onCreated(level)
    } catch (err) {
      setApiError(err instanceof Error ? err.message : 'Failed to create Level')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <motion.form
      className="add-class-form"
      initial={{ opacity: 0, height: 0 }}
      animate={{ opacity: 1, height: 'auto' }}
      exit={{ opacity: 0, height: 0 }}
      onSubmit={handleSubmit}
    >
      <div className="add-class-form-row">
        <input
          ref={inputRef}
          className="add-class-input"
          type="text"
          placeholder="Level name (e.g. Marcia)"
          value={name}
          onChange={e => setName(e.target.value)}
          disabled={submitting}
        />
        <button type="submit" disabled={submitting || !name.trim()}>Add</button>
        <button type="button" className="btn-secondary" onClick={onCancel} disabled={submitting}>Cancel</button>
      </div>
      {duplicateError && (
        <InlineError title={`"${duplicateError}"`} onDismiss={() => setDuplicateError(null)}>
          already exists in this Group.
        </InlineError>
      )}
      {apiError && (
        <InlineError onDismiss={() => setApiError(null)}>
          {apiError}
        </InlineError>
      )}
    </motion.form>
  )
}

function ReportInstructionsEditor({
  level,
  onSave,
}: {
  level: LevelItem
  onSave: (instructions: string) => Promise<void>
}) {
  const [value, setValue] = useState(level.reportInstructions)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const savedTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setValue(level.reportInstructions)
  }, [level.reportInstructions])

  useEffect(() => {
    return () => clearTimeout(savedTimer.current)
  }, [])

  async function handleSave() {
    if (value === level.reportInstructions) return
    setSaving(true)
    setError(null)
    setSaved(false)
    try {
      await onSave(value)
      setSaved(true)
      clearTimeout(savedTimer.current)
      savedTimer.current = setTimeout(() => setSaved(false), 1500)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="report-instructions levels-admin-instructions">
      <label htmlFor={`level-instructions-${level.id}`}>Report Instructions</label>
      <textarea
        id={`level-instructions-${level.id}`}
        value={value}
        onChange={e => setValue(e.target.value)}
        onBlur={handleSave}
        placeholder="Describe how report cards should read for this Level — tone, sections, focus areas..."
        rows={5}
      />
      <div className="levels-admin-instructions-status">
        {saving && <span>Saving…</span>}
        {saved && !saving && <span className="levels-admin-instructions-saved">Saved</span>}
        {error && <InlineError severity="error" onDismiss={() => setError(null)}>{error}</InlineError>}
        {!value && !saving && (
          <span className="levels-admin-instructions-empty">
            No Report Instructions yet — reports for this Level can't be generated until an Admin adds them.
          </span>
        )}
      </div>
    </div>
  )
}
