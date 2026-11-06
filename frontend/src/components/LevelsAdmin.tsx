import { useState, useEffect, useCallback, useRef } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import {
  listLevels,
  createLevel,
  renameLevel,
  updateLevelReportInstructions,
  deleteLevel,
  createTerm,
  updateTerm,
  deleteTerm,
  rolloverTerms,
  type LevelItem,
  type Term,
  type TermFields,
} from '../api'
import InlineError from './InlineError'
import InlineEdit from './InlineEdit'
import ItemRow from './ItemRow'
import { HexBullet, TrashIcon } from './Icons'
import CollapsePresence from './CollapsePresence'
import { formatDateRange } from '../lib/date'

type Status = 'loading' | 'error' | 'ready'

export default function LevelsAdmin() {
  const { getToken } = useAuth()
  const [status, setStatus] = useState<Status>('loading')
  const [levels, setLevels] = useState<LevelItem[]>([])
  const [terms, setTerms] = useState<Term[]>([])
  const [termId, setTermId] = useState<number | null>(null)
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
      const { levels: lvls, term, terms: trms } = await listLevels(getToken)
      setLevels(lvls || [])
      setTerms(trms || [])
      // Current Term (contains today, else latest started), else the first upcoming.
      setTermId(term?.id ?? trms?.[0]?.id ?? null)
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

  // termId is the Term the text was typed under, not the one picked when the
  // save lands.
  async function handleSaveInstructions(id: number, termId: number, instructions: string) {
    try {
      await updateLevelReportInstructions(id, instructions, getToken, termId)
      setLevels(prev => prev.map(l => l.id === id
        ? { ...l, instructionsByTerm: { ...l.instructionsByTerm, [termId]: instructions } }
        : l))
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

  function byStart(list: Term[]) {
    return [...list].sort((a, b) => a.startDate.localeCompare(b.startDate))
  }

  function handleTermCreated(term: Term) {
    setTerms(prev => byStart([...prev, term]))
    setTermId(term.id)
  }

  function handleTermSaved(term: Term) {
    setTerms(prev => byStart(prev.map(t => t.id === term.id ? term : t)))
  }

  function handleTermDeleted(id: number) {
    const rest = terms.filter(t => t.id !== id)
    setTerms(rest)
    if (id !== termId) return
    // Same rule as the load: latest started, else the first upcoming.
    const today = new Date().toISOString().slice(0, 10)
    setTermId((rest.findLast(t => t.startDate <= today) ?? rest[0])?.id ?? null)
  }

  const picked = terms.find(t => t.id === termId)

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

      <TermStrip
        terms={terms}
        picked={picked}
        onPick={setTermId}
        onCreated={handleTermCreated}
        onSaved={handleTermSaved}
        onDeleted={handleTermDeleted}
      />

      <AnimatePresence mode="wait" initial={false}>
        {showAdd ? (
          <CollapsePresence key="add-level">
            <AddLevelForm
              existingNames={levels.map(l => l.name)}
              onCreated={handleCreated}
              onCancel={() => setShowAdd(false)}
            />
          </CollapsePresence>
        ) : levels.length === 0 ? (
          <CollapsePresence key="empty">
            <div className="info-box">
              <h2>No Levels yet</h2>
              <p>Add your Group's first Level to get started.</p>
            </div>
          </CollapsePresence>
        ) : null}
      </AnimatePresence>

      {levels.length > 0 && (
        <ul className="levels-admin-list">
          {levels.map(level => {
            const text = picked ? level.instructionsByTerm?.[picked.id] ?? '' : ''
            return (
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
                  icon={<HexBullet />}
                  expanded={expandedId === level.id}
                  onToggle={() => setExpandedId(prev => prev === level.id ? null : level.id)}
                  onDelete={() => handleDelete(level.id)}
                  badge={picked && !text.trim() && (
                    <span className="levels-admin-missing">⚠ No {picked.name} text</span>
                  )}
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
                  {picked ? (
                    <ReportInstructionsEditor
                      key={picked.id}
                      levelId={level.id}
                      text={text}
                      termName={picked.name}
                      onSave={instructions => handleSaveInstructions(level.id, picked.id, instructions)}
                    />
                  ) : (
                    <p className="levels-admin-instructions levels-admin-instructions-empty">
                      Add a term to write Report Instructions.
                    </p>
                  )}
                </ItemRow>
              )}
            </li>
            )
          })}
        </ul>
      )}

      {terms.length > 0 && <NewSchoolYear terms={terms} onDone={fetchLevels} />}

      {flashError && <div className="flash-error">{flashError}</div>}
    </div>
  )
}

// en-GB: "Term 1, Term 2 and Term 3", no Oxford comma.
const joinNames = new Intl.ListFormat('en-GB', { type: 'conjunction' })

function NewSchoolYear({ terms, onDone }: { terms: Term[]; onDone: () => void }) {
  const { getToken } = useAuth()
  const [confirming, setConfirming] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Set on Cancel so the trigger takes focus back, but not on first render.
  const refocus = useRef(false)

  async function handleConfirm() {
    setSubmitting(true)
    setError(null)
    try {
      await rolloverTerms(getToken)
      onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start new school year')
      setSubmitting(false)
    }
  }

  return (
    <div className="levels-new-year">
      {confirming ? (
        <div className="delete-confirm">
          <span id="new-year-warning">
            <strong>Start a new school year?</strong>{' '}
            {joinNames.format(terms.map(t => t.name))} {terms.length === 1 ? 'becomes' : 'become'} read-only.
            New copies get every Level's instructions and dates one year later. Check the new dates after.
          </span>
          <div className="delete-confirm-actions">
            <button
              className="btn-secondary btn-sm"
              autoFocus
              aria-describedby="new-year-warning"
              onClick={() => { refocus.current = true; setConfirming(false) }}
              disabled={submitting}
            >
              Cancel
            </button>
            <button
              className="btn-danger btn-sm"
              aria-describedby="new-year-warning"
              onClick={handleConfirm}
              disabled={submitting}
            >
              Start new year
            </button>
          </div>
        </div>
      ) : (
        <p>
          School year over?{' '}
          <button
            className="btn-secondary btn-sm"
            ref={el => { if (el && refocus.current) { refocus.current = false; el.focus() } }}
            onClick={() => setConfirming(true)}
          >
            Start new school year
          </button>
        </p>
      )}
      {error && <InlineError onDismiss={() => setError(null)}>{error}</InlineError>}
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
    <form
      className="add-class-form"
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
    </form>
  )
}

function ReportInstructionsEditor({
  levelId,
  text,
  termName,
  onSave,
}: {
  levelId: number
  text: string
  termName: string
  onSave: (instructions: string) => Promise<void>
}) {
  const [value, setValue] = useState(text)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const savedTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setValue(text)
  }, [text])

  useEffect(() => {
    return () => clearTimeout(savedTimer.current)
  }, [])

  async function handleSave() {
    if (value === text) return
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
      <label htmlFor={`level-instructions-${levelId}`}>
        Report Instructions · {termName}
      </label>
      <textarea
        id={`level-instructions-${levelId}`}
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
            No {termName} instructions yet — {termName} reports for this Level can't be generated until an Admin adds them.
          </span>
        )}
      </div>
    </div>
  )
}

function TermStrip({
  terms,
  picked,
  onPick,
  onCreated,
  onSaved,
  onDeleted,
}: {
  terms: Term[]
  picked?: Term
  onPick: (id: number) => void
  onCreated: (term: Term) => void
  onSaved: (term: Term) => void
  onDeleted: (id: number) => void
}) {
  const { getToken } = useAuth()
  const [mode, setMode] = useState<'view' | 'add' | 'edit'>('view')
  const [error, setError] = useState<{ term: string; message: string } | null>(null)

  function show(next: typeof mode) {
    setMode(next)
    setError(null)
  }

  async function handleAdd(fields: TermFields) {
    onCreated(await createTerm(fields, getToken))
    show('view')
  }

  async function handleEdit(id: number, fields: TermFields) {
    onSaved(await updateTerm(id, fields, getToken))
    show('view')
  }

  async function handleDelete(term: Term) {
    // Close any open form: the error shows in view mode only.
    show('view')
    try {
      await deleteTerm(term.id, getToken)
      onDeleted(term.id)
    } catch (err) {
      // With a trash on every chip, the error names its Term.
      setError({ term: term.name, message: err instanceof Error ? err.message : 'Failed to delete term' })
    }
  }

  return (
    <div className="levels-terms">
      <div className="levels-terms-picker" role="group" aria-label="Term">
        {terms.map(t => (
          // Buttons can't nest, so each chip is a box around its own buttons.
          // A click anywhere on the box picks; the name button serves the keyboard.
          <div
            key={t.id}
            className={`levels-term-chip${t.id === picked?.id ? ' levels-term-chip-active' : ''}`}
            onClick={() => { onPick(t.id); show('view') }}
          >
            <button type="button" className="levels-term-chip-name" aria-pressed={t.id === picked?.id}>
              {t.name}
            </button>
            <span className="levels-term-chip-dates">{formatDateRange(t.startDate, t.endDate)}</span>
            <button
              type="button"
              className="icon-btn"
              onClick={e => { e.stopPropagation(); onPick(t.id); show('edit') }}
              aria-label={`Edit ${t.name}`}
              title={`Edit ${t.name}`}
            >
              ✎
            </button>
            <button
              type="button"
              className="icon-btn icon-btn-danger"
              onClick={e => { e.stopPropagation(); handleDelete(t) }}
              aria-label={`Delete ${t.name}`}
              title={`Delete ${t.name}`}
            >
              <TrashIcon />
            </button>
          </div>
        ))}
        {mode !== 'add' && (terms.length > 0 ? (
          <button type="button" className="icon-btn" onClick={() => show('add')} aria-label="Add term" title="Add term">
            +
          </button>
        ) : (
          <button type="button" className="btn-secondary levels-term-chip" onClick={() => show('add')}>
            + Add term
          </button>
        ))}
      </div>
      {mode === 'add' ? (
        <TermForm submitLabel="Add" onSubmit={handleAdd} onCancel={() => show('view')} />
      ) : picked && mode === 'edit' ? (
        <TermForm
          key={picked.id}
          initial={picked}
          submitLabel="Save"
          onSubmit={fields => handleEdit(picked.id, fields)}
          onCancel={() => show('view')}
        />
      ) : picked && (
        <div className="levels-term-summary">
          <p className="levels-term-note">Report Instructions below are for {picked.name}.</p>
          {error && <InlineError title={error.term} onDismiss={() => setError(null)}>{error.message}</InlineError>}
        </div>
      )}
    </div>
  )
}

function TermForm({
  initial,
  submitLabel,
  onSubmit,
  onCancel,
}: {
  initial?: Term
  submitLabel: string
  onSubmit: (fields: TermFields) => Promise<void>
  onCancel: () => void
}) {
  const [fields, setFields] = useState<TermFields>({
    name: initial?.name ?? '', startDate: initial?.startDate ?? '', endDate: initial?.endDate ?? '',
  })
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      await onSubmit({ ...fields, name: fields.name.trim() })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save term')
      setSubmitting(false)
    }
  }

  const input = (label: string, key: keyof TermFields, type: 'text' | 'date') => (
    <label>
      {label}
      <input
        className="add-class-input"
        type={type}
        value={fields[key]}
        autoFocus={key === 'name'}
        onChange={e => setFields({ ...fields, [key]: e.target.value })}
        disabled={submitting}
      />
    </label>
  )

  const ready = fields.name.trim() && fields.startDate && fields.endDate
  return (
    <form className="levels-term-fields" onSubmit={handleSubmit}>
      <div className="levels-term-fields-row">
        {input('Name', 'name', 'text')}
        {input('From', 'startDate', 'date')}
        {input('To', 'endDate', 'date')}
        <button type="submit" className="btn-sm" disabled={submitting || !ready}>{submitLabel}</button>
        <button type="button" className="btn-secondary btn-sm" onClick={onCancel} disabled={submitting}>Cancel</button>
      </div>
      <p className="levels-term-note">Dates apply to every Level in your Group.</p>
      {error && <InlineError onDismiss={() => setError(null)}>{error}</InlineError>}
    </form>
  )
}
