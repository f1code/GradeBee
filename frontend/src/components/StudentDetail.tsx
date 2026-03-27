import { useEffect, useState, useCallback } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import { listNotes, createNote, updateNote, deleteNote, type Note } from '../api'
import NotesList from './NotesList'
import NoteEditor from './NoteEditor'
import ReportHistory from './ReportHistory'

interface StudentDetailProps {
  studentId: number
  studentName: string
  className: string
  onCollapse: () => void
  modal?: boolean
}

type Status = 'loading' | 'error' | 'success'
type Tab = 'notes' | 'reports'

export default function StudentDetail({ studentId, studentName, className, onCollapse, modal }: StudentDetailProps) {
  const { getToken } = useAuth()
  const [activeTab, setActiveTab] = useState<Tab>('notes')
  const [notes, setNotes] = useState<Note[]>([])
  const [status, setStatus] = useState<Status>('loading')
  const [editingNoteId, setEditingNoteId] = useState<number | null>(null)
  const [addingNote, setAddingNote] = useState(false)
  const [savingNew, setSavingNew] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchNotes = useCallback(async () => {
    setStatus('loading')
    setError(null)
    try {
      const { notes: fetched } = await listNotes(studentId, getToken)
      setNotes(fetched || [])
      setStatus('success')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load notes')
      setStatus('error')
    }
  }, [studentId, getToken])

  useEffect(() => {
    fetchNotes()
  }, [fetchNotes])

  async function handleCreate(data: { date: string; summary: string }) {
    setSavingNew(true)
    try {
      await createNote(studentId, data, getToken)
      setAddingNote(false)
      await fetchNotes()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create note')
    } finally {
      setSavingNew(false)
    }
  }

  async function handleSaveEdit(noteId: number, summary: string) {
    try {
      await updateNote(noteId, { summary }, getToken)
      setEditingNoteId(null)
      await fetchNotes()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update note')
    }
  }

  async function handleDelete(noteId: number) {
    try {
      await deleteNote(noteId, getToken)
      await fetchNotes()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete note')
    }
  }

  return (
    <motion.div
      className="student-detail"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.25 }}
      data-testid={`student-detail-${studentId}`}
    >
      {/* Mobile back bar */}
      {!modal && (
        <button className="student-detail-back" onClick={onCollapse}>
          ← Back to list
        </button>
      )}

      {/* Header */}
      <div className="student-detail-header">
        <div className="student-detail-info">
          <h3 className="student-detail-name">{studentName}</h3>
          <span className="student-detail-class">{className}</span>
        </div>
        {activeTab === 'notes' && (
          <button
            className="btn-sm"
            onClick={() => { setAddingNote(true); setEditingNoteId(null) }}
            disabled={addingNote}
            data-testid="add-note-btn"
          >
            + Add Note
          </button>
        )}
      </div>

      {/* Tab toggle */}
      <div className="student-detail-tabs">
        <button
          className={`student-detail-tab${activeTab === 'notes' ? ' student-detail-tab-active' : ''}`}
          onClick={() => setActiveTab('notes')}
        >
          Notes
        </button>
        <button
          className={`student-detail-tab${activeTab === 'reports' ? ' student-detail-tab-active' : ''}`}
          onClick={() => setActiveTab('reports')}
        >
          Reports
        </button>
      </div>

      {/* Error flash */}
      <AnimatePresence>
        {error && (
          <motion.div
            className="student-detail-error"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.2 }}
          >
            <span>{error}</span>
            <button className="icon-btn" onClick={() => setError(null)} aria-label="Dismiss">✕</button>
          </motion.div>
        )}
      </AnimatePresence>

      {activeTab === 'notes' ? (
        <>
          {/* Add note form */}
          <AnimatePresence>
            {addingNote && (
              <NoteEditor
                mode="create"
                onSave={handleCreate}
                onCancel={() => setAddingNote(false)}
                saving={savingNew}
              />
            )}
          </AnimatePresence>

          {/* Notes content */}
          {status === 'loading' && (
            <div className="student-detail-loading">
              <div className="honeycomb-spinner">
                <div className="hex" /><div className="hex" /><div className="hex" />
              </div>
            </div>
          )}

          {status === 'error' && !notes.length && (
            <div className="student-detail-error-state">
              <p>Failed to load notes.</p>
              <button className="btn-sm btn-secondary" onClick={fetchNotes}>Retry</button>
            </div>
          )}

          {status === 'success' && (
            <NotesList
              notes={notes}
              onEdit={id => { setEditingNoteId(id); setAddingNote(false) }}
              onDelete={handleDelete}
              editingNoteId={editingNoteId}
              onSaveEdit={handleSaveEdit}
              onCancelEdit={() => setEditingNoteId(null)}
            />
          )}
        </>
      ) : (
        <ReportHistory studentId={studentId} studentName={studentName} />
      )}
    </motion.div>
  )
}
