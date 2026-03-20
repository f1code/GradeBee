import { useState } from 'react'
import { motion } from 'motion/react'
import type { ExtractResult, NoteResult } from '../api'

interface StudentEdit {
  name: string
  class: string
  summary: string
  confidence: number
  candidates?: { name: string; class: string }[]
}

interface Props {
  extractResult: ExtractResult
  transcript: string
  onSave: (students: { name: string; class: string; summary: string }[], date: string) => void
  onCancel: () => void
  saving: boolean
  savedNotes: NoteResult[] | null
  onReset: () => void
}

function ConfidenceBadge({ confidence }: { confidence: number }) {
  const isHigh = confidence >= 0.7
  return (
    <span
      className="confidence-badge"
      data-level={isHigh ? 'high' : 'low'}
    >
      {isHigh ? '✓ Matched' : '⚠ Uncertain'}
      <span className="confidence-score">{Math.round(confidence * 100)}%</span>
    </span>
  )
}

function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg
      width="16" height="16" viewBox="0 0 16 16" fill="none"
      style={{ transform: open ? 'rotate(180deg)' : 'rotate(0deg)', transition: 'transform 0.2s' }}
    >
      <path d="M4 6L8 10L12 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function DocIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <path d="M4 2H10L13 5V14H4V2Z" stroke="currentColor" strokeWidth="1.2" fill="none" />
      <path d="M10 2V5H13" stroke="currentColor" strokeWidth="1.2" />
      <line x1="6" y1="8" x2="11" y2="8" stroke="currentColor" strokeWidth="1" />
      <line x1="6" y1="10.5" x2="11" y2="10.5" stroke="currentColor" strokeWidth="1" />
    </svg>
  )
}

export default function NoteConfirmation({
  extractResult,
  transcript,
  onSave,
  onCancel,
  saving,
  savedNotes,
  onReset,
}: Props) {
  const [students, setStudents] = useState<StudentEdit[]>(
    extractResult.students.map(s => ({ ...s }))
  )
  const [date, setDate] = useState(extractResult.date)
  const [showTranscript, setShowTranscript] = useState(false)

  function updateStudent(idx: number, field: keyof StudentEdit, value: string) {
    setStudents(prev => prev.map((s, i) => i === idx ? { ...s, [field]: value } : s))
  }

  function selectCandidate(idx: number, name: string, cls: string) {
    setStudents(prev => prev.map((s, i) =>
      i === idx ? { ...s, name, class: cls, confidence: 0.85 } : s
    ))
  }

  function removeStudent(idx: number) {
    setStudents(prev => prev.filter((_, i) => i !== idx))
  }

  function handleSave() {
    onSave(
      students.map(s => ({ name: s.name, class: s.class, summary: s.summary })),
      date
    )
  }

  // Success state
  if (savedNotes) {
    return (
      <motion.div
        className="note-confirmation"
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
      >
        <div className="note-success">
          <div className="note-success-icon">🎉</div>
          <h3>{savedNotes.length === 1 ? 'Note created!' : `${savedNotes.length} notes created!`}</h3>
          <div className="note-links">
            {savedNotes.map((note, i) => (
              <a
                key={i}
                href={note.docUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="note-doc-link"
              >
                <DocIcon />
                {note.student} — {note.class}
              </a>
            ))}
          </div>
          <button onClick={onReset} className="btn-secondary" style={{ marginTop: '1rem' }}>
            Upload another
          </button>
        </div>
      </motion.div>
    )
  }

  return (
    <motion.div
      className="note-confirmation"
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3 }}
    >
      <h3>Confirm Student Notes</h3>

      {/* Students */}
      <div className="note-students">
        {students.map((student, idx) => (
          <div key={idx} className="note-student-card">
            <div className="note-student-header">
              <div className="note-student-name">
                <strong>{student.name}</strong>
                <span className="note-student-class">{student.class}</span>
              </div>
              <div className="note-student-actions">
                <ConfidenceBadge confidence={student.confidence} />
                <button
                  className="note-remove-btn"
                  onClick={() => removeStudent(idx)}
                  title="Remove student"
                >
                  ×
                </button>
              </div>
            </div>

            {/* Low confidence: show candidate options */}
            {student.confidence < 0.7 && student.candidates && student.candidates.length > 0 && (
              <div className="note-candidates">
                <p className="note-candidates-label">Did you mean:</p>
                <div className="note-candidates-list">
                  {student.candidates.map((c, ci) => (
                    <button
                      key={ci}
                      className="btn-secondary note-candidate-btn"
                      onClick={() => selectCandidate(idx, c.name, c.class)}
                    >
                      {c.name} <span className="note-student-class">{c.class}</span>
                    </button>
                  ))}
                </div>
              </div>
            )}

            <textarea
              className="note-summary-input"
              value={student.summary}
              onChange={e => updateStudent(idx, 'summary', e.target.value)}
              rows={3}
              placeholder="Student summary..."
            />
          </div>
        ))}
      </div>

      {students.length === 0 && (
        <div className="note-empty-students">
          <p>No students matched. You can cancel and try again with a clearer recording.</p>
        </div>
      )}

      {/* Date */}
      <div className="note-meta-row">
        <div className="note-meta-field">
          <label htmlFor="note-date">Date</label>
          <input
            id="note-date"
            type="date"
            value={date}
            onChange={e => setDate(e.target.value)}
          />
        </div>
      </div>

      {/* Transcript collapsible */}
      <div className="note-transcript-section">
        <button
          className="note-transcript-toggle"
          onClick={() => setShowTranscript(!showTranscript)}
        >
          <ChevronIcon open={showTranscript} />
          Transcript
        </button>
        {showTranscript && (
          <motion.textarea
            className="transcript-text"
            readOnly
            value={transcript}
            rows={8}
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            transition={{ duration: 0.2 }}
          />
        )}
      </div>

      {/* Actions */}
      <div className="note-actions">
        <button
          onClick={handleSave}
          disabled={saving || students.length === 0 || !date}
        >
          {saving ? 'Creating note...' : students.length === 1 ? 'Save Note' : `Save ${students.length} Notes`}
        </button>
        <button className="btn-secondary" onClick={onCancel} disabled={saving}>
          Cancel
        </button>
      </div>
    </motion.div>
  )
}
