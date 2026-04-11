import { useState } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import type { Note } from '../api'
import { PencilIcon, TrashIcon } from './Icons'
import NoteEditor from './NoteEditor'

interface NotesListProps {
  notes: Note[]
  onEdit: (noteId: number) => void
  onDelete: (noteId: number) => void
  editingNoteId: number | null
  onSaveEdit: (noteId: number, summary: string) => Promise<void>
  onCancelEdit: () => void
}

function formatDate(dateStr: string): string {
  const [year, month, day] = dateStr.split('-').map(Number)
  const d = new Date(year, month - 1, day)
  return d.toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })
}

const containerVariants = {
  hidden: {},
  visible: { transition: { staggerChildren: 0.06 } },
}

const cardVariants = {
  hidden: { opacity: 0, y: 12 },
  visible: { opacity: 1, y: 0, transition: { duration: 0.3, ease: 'easeOut' as const } },
}

export default function NotesList({
  notes,
  onEdit,
  onDelete,
  editingNoteId,
  onSaveEdit,
  onCancelEdit,
}: NotesListProps) {
  if (notes.length === 0) {
    return (
      <div className="info-box notes-empty" data-testid="notes-empty">
        <p>No notes yet. Add one manually or upload audio to generate notes automatically.</p>
      </div>
    )
  }

  // Group notes by date
  const grouped: { date: string; notes: Note[] }[] = []
  for (const n of notes) {
    const last = grouped[grouped.length - 1]
    if (last && last.date === n.date) {
      last.notes.push(n)
    } else {
      grouped.push({ date: n.date, notes: [n] })
    }
  }

  return (
    <motion.div
      className="notes-list"
      variants={containerVariants}
      initial="hidden"
      animate="visible"
      data-testid="notes-list"
    >
      {grouped.map(group => (
        <div key={group.date} className="notes-date-group">
          <h4 className="notes-date-heading">{formatDate(group.date)}</h4>
          {group.notes.map(note => (
            <NoteCard
              key={note.id}
              note={note}
              isEditing={editingNoteId === note.id}
              onEdit={() => onEdit(note.id)}
              onDelete={() => onDelete(note.id)}
              onSaveEdit={summary => onSaveEdit(note.id, summary)}
              onCancelEdit={onCancelEdit}
            />
          ))}
        </div>
      ))}
    </motion.div>
  )
}

function NoteCard({
  note,
  isEditing,
  onEdit,
  onDelete,
  onSaveEdit,
  onCancelEdit,
}: {
  note: Note
  isEditing: boolean
  onEdit: () => void
  onDelete: () => void
  onSaveEdit: (summary: string) => Promise<void>
  onCancelEdit: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [savingEdit, setSavingEdit] = useState(false)

  async function handleSaveEdit(data: { summary: string }) {
    setSavingEdit(true)
    try {
      await onSaveEdit(data.summary)
    } finally {
      setSavingEdit(false)
    }
  }

  if (isEditing) {
    return (
      <motion.div className="note-card note-card-editing" variants={cardVariants}>
        <NoteEditor
          mode="edit"
          initialSummary={note.summary}
          initialDate={note.date}
          onSave={d => handleSaveEdit(d)}
          onCancel={onCancelEdit}
          saving={savingEdit}
        />
      </motion.div>
    )
  }

  return (
    <motion.div className="note-card" variants={cardVariants} data-testid={`note-${note.id}`}>
      <div className="note-card-header">
        <span className={`note-source-badge ${note.source === 'auto' ? 'note-source-auto' : 'note-source-manual'}`}>
          {note.source === 'auto' ? 'Auto' : 'Manual'}
        </span>
        <div className="note-card-actions">
          <button className="icon-btn" onClick={onEdit} aria-label="Edit note" data-testid={`edit-note-${note.id}`}>
            <PencilIcon />
          </button>
          <button className="icon-btn icon-btn-danger" onClick={() => setConfirmDelete(true)} aria-label="Delete note" data-testid={`delete-note-${note.id}`}>
            <TrashIcon />
          </button>
        </div>
      </div>

      <NoteSummary summary={note.summary} />

      {/* Transcript toggle (auto notes only) */}
      {note.source === 'auto' && note.transcript && (
        <div className="note-transcript-section">
          <button className="text-link" onClick={() => setExpanded(!expanded)}>
            {expanded ? 'Hide transcript' : 'Show transcript'}
          </button>
          <AnimatePresence>
            {expanded && (
              <motion.div
                className="note-transcript-block"
                initial={{ opacity: 0, height: 0 }}
                animate={{ opacity: 1, height: 'auto' }}
                exit={{ opacity: 0, height: 0 }}
                transition={{ duration: 0.2 }}
              >
                <pre className="note-transcript-text">{note.transcript}</pre>
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      )}

      {/* Delete confirmation */}
      <AnimatePresence>
        {confirmDelete && (
          <motion.div
            className="delete-confirm delete-confirm-inline"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.2 }}
          >
            <span>Are you sure?</span>
            <div className="delete-confirm-actions">
              <button className="btn-secondary btn-sm" onClick={() => setConfirmDelete(false)}>Cancel</button>
              <button className="btn-danger btn-sm" onClick={() => { setConfirmDelete(false); onDelete() }} data-testid={`confirm-delete-note-${note.id}`}>Delete</button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  )
}

function NoteSummary({ summary }: { summary: string }) {
  const [showFull, setShowFull] = useState(false)
  const needsTruncation = summary.length > 300 || summary.split('\n').length > 3

  if (!needsTruncation || showFull) {
    return (
      <div className="note-summary">
        <p style={{ whiteSpace: 'pre-wrap' }}>{summary}</p>
        {needsTruncation && (
          <button className="note-show-toggle" onClick={() => setShowFull(false)}>Show less</button>
        )}
      </div>
    )
  }

  const truncated = summary.slice(0, 300).split('\n').slice(0, 3).join('\n')

  return (
    <div className="note-summary note-summary-truncated">
      <p style={{ whiteSpace: 'pre-wrap' }}>{truncated}…</p>
      <button className="note-show-toggle" onClick={() => setShowFull(true)}>Show more</button>
    </div>
  )
}
