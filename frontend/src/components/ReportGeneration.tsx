import { useState, useEffect, useCallback } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import {
  generateReports,
  type ReportResult,
  type GenerateReportsResponse,
} from '../api'
import ReportExamples from './ReportExamples'

interface ClassGroup {
  name: string
  students: { name: string }[]
}

interface StudentsResponse {
  classes: ClassGroup[]
}

export default function ReportGeneration() {
  const { getToken } = useAuth()
  const apiUrl = import.meta.env.VITE_API_URL

  const [classes, setClasses] = useState<ClassGroup[]>([])
  const [loadingStudents, setLoadingStudents] = useState(true)
  const [selected, setSelected] = useState<Record<string, Set<string>>>({})
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [instructions, setInstructions] = useState('')
  const [generating, setGenerating] = useState(false)
  const [results, setResults] = useState<ReportResult[]>([])
  const [error, setError] = useState<string | null>(null)

  const loadStudents = useCallback(async () => {
    try {
      const token = await getToken()
      const resp = await fetch(`${apiUrl}/students`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const body: StudentsResponse = await resp.json()
      setClasses(body.classes || [])
      // Initialize selection state
      const sel: Record<string, Set<string>> = {}
      for (const c of body.classes || []) {
        sel[c.name] = new Set()
      }
      setSelected(sel)
    } catch {
      // silent
    } finally {
      setLoadingStudents(false)
    }
  }, [getToken, apiUrl])

  useEffect(() => { loadStudents() }, [loadStudents])

  // Default dates: start of current school term → today
  useEffect(() => {
    const now = new Date()
    setEndDate(now.toISOString().slice(0, 10))
    // Default start: 3 months ago
    const start = new Date(now)
    start.setMonth(start.getMonth() - 3)
    setStartDate(start.toISOString().slice(0, 10))
  }, [])

  function toggleStudent(className: string, studentName: string) {
    setSelected(prev => {
      const next = { ...prev }
      const set = new Set(next[className])
      if (set.has(studentName)) set.delete(studentName)
      else set.add(studentName)
      next[className] = set
      return next
    })
  }

  function toggleClass(className: string, students: { name: string }[]) {
    setSelected(prev => {
      const next = { ...prev }
      const set = new Set(next[className])
      const allSelected = students.every(s => set.has(s.name))
      if (allSelected) {
        next[className] = new Set()
      } else {
        next[className] = new Set(students.map(s => s.name))
      }
      return next
    })
  }

  const selectedStudents = Object.entries(selected).flatMap(([cls, names]) =>
    Array.from(names).map(name => ({ name, class: cls }))
  )

  async function handleGenerate() {
    if (selectedStudents.length === 0 || !startDate || !endDate) return
    setGenerating(true)
    setError(null)
    setResults([])
    try {
      const resp: GenerateReportsResponse = await generateReports(
        { students: selectedStudents, startDate, endDate, instructions: instructions || undefined },
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
          <div className="honeycomb-spinner" />
        ) : classes.length === 0 ? (
          <p className="report-empty">No students found. Set up your roster first.</p>
        ) : (
          <div className="report-class-groups">
            {classes.map(c => {
              const classSet = selected[c.name] || new Set()
              const allSelected = c.students.length > 0 && c.students.every(s => classSet.has(s.name))
              return (
                <motion.div key={c.name} className="report-class-card" initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }}>
                  <label className="report-class-header">
                    <input
                      type="checkbox"
                      checked={allSelected}
                      onChange={() => toggleClass(c.name, c.students)}
                    />
                    <strong>{c.name}</strong>
                    <span className="report-class-count">{classSet.size}/{c.students.length}</span>
                  </label>
                  <div className="report-student-list">
                    {c.students.map(s => (
                      <label key={s.name} className="report-student-item">
                        <input
                          type="checkbox"
                          checked={classSet.has(s.name)}
                          onChange={() => toggleStudent(c.name, s.name)}
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
        disabled={generating || selectedStudents.length === 0 || !startDate || !endDate}
      >
        {generating ? (
          <span className="btn-loading"><span className="honeycomb-spinner honeycomb-spinner-sm" /> Generating...</span>
        ) : (
          `Generate ${selectedStudents.length} Report${selectedStudents.length !== 1 ? 's' : ''}`
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
              {results.map((r, i) => (
                <motion.div
                  key={`${r.class}-${r.student}`}
                  className={`report-result-item ${r.skipped ? 'skipped' : ''}`}
                  initial={{ opacity: 0, x: -10 }}
                  animate={{ opacity: 1, x: 0 }}
                  transition={{ delay: i * 0.05 }}
                >
                  <span className="report-result-name">{r.student} <span className="report-result-class">({r.class})</span></span>
                  {r.skipped && <span className="report-result-badge">Already exists</span>}
                  <a href={r.docUrl} target="_blank" rel="noopener noreferrer" className="report-doc-link">
                    Open in Docs →
                  </a>
                </motion.div>
              ))}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
