import { useState, useRef, useEffect, useCallback } from 'react'
import { motion } from 'motion/react'

interface NoteEditorProps {
  mode: 'create' | 'edit'
  initialSummary?: string
  initialDate?: string
  onSave: (data: { date: string; summary: string }) => Promise<void>
  onCancel: () => void
  saving: boolean
}

function todayISO() {
  const d = new Date()
  return d.toISOString().slice(0, 10)
}

export default function NoteEditor({
  mode,
  initialSummary = '',
  initialDate,
  onSave,
  onCancel,
  saving,
}: NoteEditorProps) {
  const [summary, setSummary] = useState(initialSummary)
  const [date, setDate] = useState(initialDate || todayISO())
  const [dirty, setDirty] = useState(false)
  const [showDiscard, setShowDiscard] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    textareaRef.current?.focus()
  }, [])

  // Auto-grow textarea
  useEffect(() => {
    const ta = textareaRef.current
    if (ta) {
      ta.style.height = 'auto'
      ta.style.height = ta.scrollHeight + 'px'
    }
  }, [summary])

  function handleSummaryChange(value: string) {
    setSummary(value)
    if (!dirty && value !== initialSummary) setDirty(true)
  }

  const handleSave = useCallback(() => {
    if (!summary.trim() || saving) return
    onSave({ date, summary: summary.trim() })
  }, [summary, date, saving, onSave])

  function handleCancel() {
    if (dirty) {
      setShowDiscard(true)
    } else {
      onCancel()
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault()
      handleSave()
    }
  }

  const canSave = summary.trim().length > 0 && !saving

  return (
    <motion.div
      className="note-editor"
      initial={{ opacity: 0, y: -8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -8 }}
      transition={{ duration: 0.2 }}
    >
      <div className="note-editor-fields">
        <div className="note-editor-date-row">
          <label className="note-editor-label" htmlFor="note-date">Date</label>
          <input
            id="note-date"
            type="date"
            value={date}
            onChange={e => setDate(e.target.value)}
            readOnly={mode === 'edit'}
            className="note-editor-date"
            data-testid="note-editor-date"
          />
        </div>
        <div className="note-editor-summary-row">
          <label className="note-editor-label" htmlFor="note-summary">Observation</label>
          <textarea
            ref={textareaRef}
            id="note-summary"
            value={summary}
            onChange={e => handleSummaryChange(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Write your observation..."
            rows={4}
            className="note-editor-textarea"
            data-testid="note-editor-textarea"
          />
        </div>
      </div>

      {showDiscard ? (
        <div className="note-editor-discard">
          <span>Discard changes?</span>
          <div className="note-editor-discard-actions">
            <button className="btn-secondary btn-sm" onClick={() => setShowDiscard(false)}>Keep editing</button>
            <button className="btn-danger btn-sm" onClick={onCancel}>Discard</button>
          </div>
        </div>
      ) : (
        <div className="note-editor-actions">
          <button
            className={`btn-primary btn-sm${saving ? ' btn-loading' : ''}`}
            onClick={handleSave}
            disabled={!canSave}
            data-testid="note-editor-save"
          >
            {saving ? (
              <>
                <span className="honeycomb-spinner honeycomb-spinner-inline"><span className="hex" /><span className="hex" /><span className="hex" /></span>
                Saving…
              </>
            ) : (
              'Save'
            )}
          </button>
          <button className="btn-secondary btn-sm" onClick={handleCancel} data-testid="note-editor-cancel">
            Cancel
          </button>
        </div>
      )}
    </motion.div>
  )
}
