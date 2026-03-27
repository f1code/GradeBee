import { useState, useEffect, useCallback } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import {
  listClasses,
  listStudents,
  generateReports,
  type ClassItem,
  type StudentItem,
  type ReportResult,
  type GenerateReportsResponse,
} from '../api'
import ReportExamples from './ReportExamples'
import ReportViewer from './ReportViewer'

interface ClassWithStudents extends ClassItem {
  students: StudentItem[]
}

export default function ReportGeneration() {
  const { getToken } = useAuth()

  const [classes, setClasses] = useState<ClassWithStudents[]>([])
  const [loadingStudents, setLoadingStudents] = useState(true)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [instructions, setInstructions] = useState('')
  const [generating, setGenerating] = useState(false)
  const [results, setResults] = useState<ReportResult[]>([])
  const [error, setError] = useState<string | null>(null)
  const [expandedReportId, setExpandedReportId] = useState<number | null>(null)

  const loadStudents = useCallback(async () => {
    try {
      const { classes: cls } = await listClasses(getToken)
      // Fetch students for each class in parallel
      const withStudents = await Promise.all(
        (cls || []).map(async (c) => {
          try {
            const { students } = await listStudents(c.id, getToken)
            return { ...c, students: students || [] }
          } catch {
            return { ...c, students: [] }
          }
        })
      )
      setClasses(withStudents)
    } catch {
      // silent
    } finally {
      setLoadingStudents(false)
    }
  }, [getToken])

  useEffect(() => { loadStudents() }, [loadStudents])

  // Default dates: start of current school term → today
  useEffect(() => {
    const now = new Date()
    setEndDate(now.toISOString().slice(0, 10))
    const start = new Date(now)
    start.setMonth(start.getMonth() - 3)
    setStartDate(start.toISOString().slice(0, 10))
  }, [])

  function toggleStudent(studentId: number) {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(studentId)) next.delete(studentId)
      else next.add(studentId)
      return next
    })
  }

  function toggleClass(students: StudentItem[]) {
    setSelected(prev => {
      const next = new Set(prev)
      const allSelected = students.every(s => next.has(s.id))
      if (allSelected) {
        students.forEach(s => next.delete(s.id))
      } else {
        students.forEach(s => next.add(s.id))
      }
      return next
    })
  }

  const selectedCount = selected.size

  async function handleGenerate() {
    if (selectedCount === 0 || !startDate || !endDate) return
    setGenerating(true)
    setError(null)
    setResults([])
    setExpandedReportId(null)
    try {
      const resp: GenerateReportsResponse = await generateReports(
        { studentIds: Array.from(selected), startDate, endDate, instructions: instructions || undefined },
        () => getToken()
      )
      setResults(resp.reports || [])
      if (resp.error) setError(resp.error)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Generation failed')
    } finally {
      setGenerating(false)
    }
  }

  function handleReportRegenerate(reportId: number, updatedHtml: string) {
    setResults(prev => prev.map(r => r.id === reportId ? { ...r, html: updatedHtml } : r))
  }

  function handleReportDelete(reportId: number) {
    setResults(prev => prev.filter(r => r.id !== reportId))
    if (expandedReportId === reportId) setExpandedReportId(null)
  }

  return (
    <div className="report-generation">
      <h2 className="section-heading">Generate Report Cards</h2>

      {/* Period picker */}
      <div className="report-period">
        <label>
          <span>From</span>
          <input type="date" value={startDate} onChange={e => setStartDate(e.target.value)} />
        </label>
        <label>
          <span>To</span>
          <input type="date" value={endDate} onChange={e => setEndDate(e.target.value)} />
        </label>
      </div>

      {/* Student selector */}
      <div className="report-students">
        <h3>Select Students</h3>
        {loadingStudents ? (
          <div className="honeycomb-spinner">
            <div className="hex" /><div className="hex" /><div className="hex" />
          </div>
        ) : classes.length === 0 ? (
          <p className="report-empty">No students found. Set up your roster first.</p>
        ) : (
          <div className="report-class-groups">
            {classes.map(c => {
              const classSelected = c.students.filter(s => selected.has(s.id)).length
              const allSelected = c.students.length > 0 && classSelected === c.students.length
              return (
                <motion.div key={c.id} className="report-class-card" initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }}>
                  <label className="report-class-header">
                    <input
                      type="checkbox"
                      checked={allSelected}
                      onChange={() => toggleClass(c.students)}
                    />
                    <strong>{c.name}</strong>
                    <span className="report-class-count">{classSelected}/{c.students.length}</span>
                  </label>
                  <div className="report-student-list">
                    {c.students.map(s => (
                      <label key={s.id} className="report-student-item">
                        <input
                          type="checkbox"
                          checked={selected.has(s.id)}
                          onChange={() => toggleStudent(s.id)}
                        />
                        {s.name}
                      </label>
                    ))}
                  </div>
                </motion.div>
              )
            })}
          </div>
        )}
      </div>

      {/* Example report cards */}
      <ReportExamples />

      {/* Additional instructions */}
      <div className="report-instructions">
        <h3>Additional Instructions</h3>
        <textarea
          value={instructions}
          onChange={e => setInstructions(e.target.value)}
          placeholder="e.g. Focus on social skills, keep paragraphs short..."
          rows={3}
        />
      </div>

      {/* Generate button */}
      <button
        className="btn-primary report-generate-btn"
        onClick={handleGenerate}
        disabled={generating || selectedCount === 0 || !startDate || !endDate}
      >
        {generating ? (
          <span className="btn-loading"><span className="honeycomb-spinner honeycomb-spinner-sm" /> Generating...</span>
        ) : (
          `Generate ${selectedCount} Report${selectedCount !== 1 ? 's' : ''}`
        )}
      </button>

      {/* Error */}
      {error && (
        <motion.div className="report-error" initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
          <p>⚠️ {error}</p>
        </motion.div>
      )}

      {/* Results */}
      <AnimatePresence>
        {results.length > 0 && (
          <motion.div
            className="report-results"
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
          >
            <h3>Generated Reports</h3>
            <div className="report-results-list">
              {results.map((r, i) => {
                const isExpanded = expandedReportId === r.id
                return (
                  <motion.div
                    key={r.id}
                    className="report-result-card"
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ delay: i * 0.05 }}
                    layout
                  >
                    <div
                      className={`report-result-item${isExpanded ? ' report-result-item-expanded' : ''}`}
                      onClick={() => setExpandedReportId(isExpanded ? null : r.id)}
                      role="button"
                      tabIndex={0}
                      onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setExpandedReportId(isExpanded ? null : r.id) } }}
                    >
                      <span className="report-result-name">
                        {r.student} <span className="report-result-class">({r.class})</span>
                      </span>
                      <svg
                        width="16" height="16" viewBox="0 0 16 16" fill="none"
                        style={{ transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)', transition: 'transform 0.2s', flexShrink: 0 }}
                      >
                        <path d="M4 6L8 10L12 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                      </svg>
                    </div>
                    <AnimatePresence>
                      {isExpanded && (
                        <motion.div
                          initial={{ opacity: 0, height: 0 }}
                          animate={{ opacity: 1, height: 'auto' }}
                          exit={{ opacity: 0, height: 0 }}
                          transition={{ duration: 0.25 }}
                          style={{ overflow: 'hidden' }}
                        >
                          <ReportViewer
                            reportId={r.id}
                            html={r.html}
                            studentName={r.student}
                            onRegenerate={(updatedHtml) => handleReportRegenerate(r.id, updatedHtml)}
                            onDelete={() => handleReportDelete(r.id)}
                          />
                        </motion.div>
                      )}
                    </AnimatePresence>
                  </motion.div>
                )
              })}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
