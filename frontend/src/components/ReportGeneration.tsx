import { useState, useEffect, useCallback, useMemo } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import {
  listClasses,
  listStudents,
  generateReports,
  listReportExamples,
  uploadReportExample,
  updateReportExample,
  deleteReportExample,
  importExampleFromDrive,
  getGoogleToken,
  listClassNames,
  type ClassItem,
  type StudentItem,
  type ReportResult,
  type GenerateReportsResponse,
  type ReportExampleItem,
} from '../api'
import { useDrivePicker } from '../hooks/useDrivePicker'
import ReportExamples from './ReportExamples'
import ReportViewer from './ReportViewer'

interface ClassWithStudents extends ClassItem {
  students: StudentItem[]
}

const REPORT_MIME_TYPES = [
  'application/pdf',
  'image/png',
  'image/jpeg',
  'image/webp',
  'text/plain',
  'text/markdown',
].join(',')

const EXAMPLE_POLL_INTERVAL = 3000

export default function ReportGeneration() {
  const { getToken } = useAuth()

  const [classes, setClasses] = useState<ClassWithStudents[]>([])
  const [loadingStudents, setLoadingStudents] = useState(true)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [startDate, setStartDate] = useState(() => {
    const d = new Date()
    d.setMonth(d.getMonth() - 3)
    return d.toISOString().slice(0, 10)
  })
  const [endDate, setEndDate] = useState(
    () => new Date().toISOString().slice(0, 10)
  )
  const [instructions, setInstructions] = useState('')
  const [generating, setGenerating] = useState(false)
  const [results, setResults] = useState<ReportResult[]>([])
  const [error, setError] = useState<string | null>(null)
  const [expandedReportId, setExpandedReportId] = useState<number | null>(null)

  // Example report cards state + lifecycle
  const [examples, setExamples] = useState<ReportExampleItem[]>([])
  const [examplesLoading, setExamplesLoading] = useState(true)
  const [examplesError, setExamplesError] = useState<string | null>(null)
  const [availableClassNames, setAvailableClassNames] = useState<string[]>([])
  const { openPicker } = useDrivePicker()

  const loadExamples = useCallback(async () => {
    try {
      const { examples } = await listReportExamples(() => getToken())
      setExamples(examples)
    } catch (e: unknown) {
      setExamplesError(e instanceof Error ? e.message : 'Failed to load examples')
    } finally {
      setExamplesLoading(false)
    }
  }, [getToken])

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { loadExamples() }, [loadExamples])

  useEffect(() => {
    listClassNames(getToken).then(({ classNames }) => setAvailableClassNames(classNames)).catch(() => {})
  }, [getToken])

  // Poll while any example is still processing.
  useEffect(() => {
    const hasProcessing = examples.some(e => e.status === 'processing')
    if (!hasProcessing) return
    const timer = setInterval(() => { loadExamples() }, EXAMPLE_POLL_INTERVAL)
    return () => clearInterval(timer)
  }, [examples, loadExamples])

  async function handleUploadExamples(files: File[], classNames: string[]) {
    setExamplesError(null)
    try {
      for (const file of files) {
        await uploadReportExample(file, classNames, () => getToken())
      }
      await loadExamples()
    } catch (e: unknown) {
      setExamplesError(e instanceof Error ? e.message : 'Upload failed')
    }
  }

  async function handleDriveImportExample() {
    setExamplesError(null)
    try {
      const { accessToken } = await getGoogleToken(getToken)
      const picked = await openPicker(accessToken, {
        mimeTypes: REPORT_MIME_TYPES,
        title: 'Select a report card',
      })
      if (!picked || picked.length === 0) return
      await importExampleFromDrive(picked[0].id, picked[0].name, getToken)
      await loadExamples()
    } catch (e: unknown) {
      setExamplesError(e instanceof Error ? e.message : 'Drive import failed')
    }
  }

  async function handleUpdateExample(id: number, name: string, content: string, classNames: string[]) {
    setExamplesError(null)
    try {
      await updateReportExample(id, name, content, classNames, () => getToken())
      await loadExamples()
    } catch (e: unknown) {
      setExamplesError(e instanceof Error ? e.message : 'Update failed')
      throw e
    }
  }

  async function handleDeleteExample(id: number) {
    try {
      await deleteReportExample(id, () => getToken())
      setExamples(prev => prev.filter(e => e.id !== id))
    } catch (e: unknown) {
      setExamplesError(e instanceof Error ? e.message : 'Delete failed')
    }
  }

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

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { loadStudents() }, [loadStudents])

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

  // Distinct class names of currently-selected students
  const selectedClassNames = useMemo(() => {
    const names = new Set<string>()
    for (const c of classes) {
      for (const s of c.students) {
        if (selected.has(s.id)) names.add(c.className)
      }
    }
    return Array.from(names)
  }, [classes, selected])

  // Block generation when a class has no matching ready example
  const blockerMessage = useMemo(() => {
    if (selected.size === 0) return null
    const readyExamples = examples.filter(e => e.status === 'ready')
    const classesWithExamples = new Set(readyExamples.flatMap(e => e.classNames ?? []))
    const parts: string[] = []
    // Students whose class name is empty
    for (const c of classes) {
      if (!c.name) {
        for (const s of c.students) {
          if (selected.has(s.id)) parts.push(`${s.name} (no class)`)
        }
      }
    }
    // Classes with selected students but no matching ready example
    const checked = new Set<string>()
    for (const c of classes) {
      if (c.className && !checked.has(c.className)) {
        const hasSelected = c.students.some(s => selected.has(s.id))
        if (hasSelected && !classesWithExamples.has(c.className)) {
          parts.push(`${c.name} (no examples)`)
          checked.add(c.className)
        }
      }
    }
    return parts.length > 0
      ? `${parts.join(', ')} — assign a class / add examples to continue.`
      : null
  }, [classes, selected, examples])

  async function handleGenerate() {
    if (selectedCount === 0 || !startDate || !endDate) return
    setGenerating(true)
    setError(null)
    setResults([])
    setExpandedReportId(null)
    try {
      // Build students array with name+class for the backend
      const students = classes.flatMap(c =>
        c.students
          .filter(s => selected.has(s.id))
          .map(s => ({ studentId: s.id, name: s.name, class: c.name }))
      )
      const resp: GenerateReportsResponse = await generateReports(
        { students, startDate, endDate, instructions: instructions || undefined },
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
      <ReportExamples
        examples={examples}
        loading={examplesLoading}
        error={examplesError}
        availableClassNames={availableClassNames}
        selectedClassNames={selectedClassNames}
        onUpload={handleUploadExamples}
        onDriveImport={handleDriveImportExample}
        onUpdate={handleUpdateExample}
        onDelete={handleDeleteExample}
      />

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
      {blockerMessage && (
        <p className="report-generate-blocker" data-testid="generate-blocker">{blockerMessage}</p>
      )}
      <button
        className="report-generate-btn"
        onClick={handleGenerate}
        disabled={generating || selectedCount === 0 || !startDate || !endDate || !!blockerMessage}
      >
        {generating ? (
          <span className="btn-loading"><span className="honeycomb-spinner honeycomb-spinner-inline"><span className="hex" /><span className="hex" /><span className="hex" /></span> Generating...</span>
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
                      <span className="report-result-name" data-testid="report-result-name">
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
