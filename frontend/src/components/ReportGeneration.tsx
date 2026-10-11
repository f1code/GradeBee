import { useState, useEffect, useCallback, useMemo, useRef, type ReactNode } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import {
  listClasses,
  listStudents,
  generateReports,
  listReportBatches,
  retryReportBatch,
  getReport,
  listLevels,
  ApiError,
  type LevelItem,
  type ClassItem,
  type StudentItem,
  type ReportResult,
  type ReportBatch,
  type ReportBatchStudent,
} from '../api'
import {
  ReportBatchFinished,
  ReportBatchRunning,
  ReportStudentDone,
  ReportStudentFailed,
  ReportStudentGenerating,
} from '../api-types.gen'
import ReportViewer from './ReportViewer'
import { formatDateRange } from '../lib/date'

interface ClassWithStudents extends ClassItem {
  students: StudentItem[]
}

const POLL_MS = 3000

function batchSummary(batch: ReportBatch, deletedReportIds: Set<number>): string {
  const rows = batch.students
  const doneRows = rows.filter(s => s.status === ReportStudentDone)
  const deleted = doneRows.filter(s => s.reportId !== undefined && deletedReportIds.has(s.reportId)).length
  const done = doneRows.length - deleted
  const failed = rows.filter(s => s.status === ReportStudentFailed).length
  const pending = rows.length - doneRows.length - failed
  const n = rows.length
  const parts = [`${n} student${n !== 1 ? 's' : ''}`]
  if (done) parts.push(`${done} done`)
  if (deleted) parts.push(`${deleted} deleted`)
  if (failed) parts.push(`${failed} failed`)
  if (pending) parts.push(`${pending} in progress`)
  return parts.join(' · ')
}

function hasRetryableFailures(b: ReportBatch): boolean {
  return b.status === ReportBatchFinished && b.students.some(s => s.status === ReportStudentFailed)
}

// What a row that can't expand shows: deleted, failed, or still pending.
function staticStatus(r: ReportBatchStudent, deleted: boolean) {
  if (deleted) {
    return { testId: 'report-result-deleted', className: 'report-result-status', text: 'deleted' }
  }
  if (r.status === ReportStudentFailed) {
    return { testId: 'report-result-failed', className: 'report-result-error', text: r.error || 'No report' }
  }
  const text = r.status === ReportStudentGenerating ? 'generating…' : 'queued'
  return { testId: 'report-result-pending', className: 'report-result-status', text }
}

function StaticRow({ name, testId, className, text }: {
  name: ReactNode
  testId: string
  className: string
  text: string
}) {
  return (
    <div className="report-result-item report-result-item-static" data-testid={testId}>
      <span className="report-result-name">{name}</span>
      <span className={className}>{text}</span>
    </div>
  )
}

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
  const [posting, setPosting] = useState(false)
  const [retrying, setRetrying] = useState(false)
  const [batch, setBatch] = useState<ReportBatch | null>(null)
  // A running batch the server no longer knows: it restarted mid-batch.
  const [interrupted, setInterrupted] = useState(false)
  // Bumped when a poll fails, so the next one is still scheduled.
  const [pollRetry, setPollRetry] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [expandedReportId, setExpandedReportId] = useState<number | null>(null)
  const [expandedReport, setExpandedReport] = useState<ReportResult | null>(null)
  const [deletedReportIds, setDeletedReportIds] = useState<Set<number>>(new Set())
  // Last report the Teacher asked to expand; a slower earlier fetch must not land on it.
  const requestedReportId = useRef<number | null>(null)

  const [levels, setLevels] = useState<LevelItem[]>([])
  const [levelsError, setLevelsError] = useState<string | null>(null)

  useEffect(() => {
    listLevels(getToken)
      .then(({ levels }) => setLevels(levels))
      .catch((e: unknown) => setLevelsError(e instanceof Error ? e.message : 'Failed to load Levels'))
  }, [getToken])

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

  useEffect(() => {
    listReportBatches(getToken)
      .then(({ batches }) => setBatch(prev => prev ?? batches[0] ?? null))
      .catch(() => { /* no batch to show */ })
  }, [getToken])

  const running = batch?.status === ReportBatchRunning
  const canRetry = !!batch && hasRetryableFailures(batch)
  useEffect(() => {
    if (!batch || !running) return
    let cancelled = false
    const timer = setTimeout(async () => {
      try {
        const { batches } = await listReportBatches(getToken)
        if (cancelled) return
        // Gone with a newer batch present: another tab replaced it.
        const latest = batches.find(b => b.id === batch.id) ?? batches[0]
        if (latest) {
          setBatch(latest)
        } else {
          setBatch(null)
          setInterrupted(true)
        }
      } catch {
        if (!cancelled) setPollRetry(n => n + 1)
      }
    }, POLL_MS)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [batch, running, pollRetry, getToken])

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

  // Distinct Levels among currently-selected students, keyed by levelId.
  const selectedLevels = useMemo(() => {
    const byId = new Map<number, LevelItem>()
    for (const c of classes) {
      if (!c.students.some(s => selected.has(s.id))) continue
      const level = levels.find(l => l.id === c.levelId)
      if (level && !byId.has(level.id)) byId.set(level.id, level)
    }
    return Array.from(byId.values())
  }, [classes, selected, levels])

  // True while a selected student's Class references a levelId not present in
  // the loaded Levels list (still loading, or the load failed) — treat as
  // unresolved rather than silently passing the gate.
  const hasUnresolvedLevel = useMemo(() => {
    for (const c of classes) {
      if (!c.students.some(s => selected.has(s.id))) continue
      if (!levels.some(l => l.id === c.levelId)) return true
    }
    return false
  }, [classes, selected, levels])

  const unsetLevels = useMemo(
    () => selectedLevels.filter(l => l.reportInstructions.trim() === ''),
    [selectedLevels]
  )

  async function handleGenerate() {
    if (selectedCount === 0 || !startDate || !endDate || unsetLevels.length > 0 || !!levelsError || hasUnresolvedLevel) return
    setPosting(true)
    setError(null)
    setInterrupted(false)
    try {
      // Build students array with name+class for the backend
      const students = classes.flatMap(c =>
        c.students
          .filter(s => selected.has(s.id))
          .map(s => ({ studentId: s.id, name: s.name, className: c.name }))
      )
      const posted = await generateReports(
        { students, startDate, endDate, instructions: instructions || undefined },
        () => getToken()
      )
      requestedReportId.current = null
      setExpandedReportId(null)
      setExpandedReport(null)
      setDeletedReportIds(new Set())
      setBatch(posted)
    } catch (e: unknown) {
      // Another tab started a batch: show it, it explains the disabled button.
      // No running batch yet (the other POST still holds the lock): say why.
      if (e instanceof ApiError && e.status === 409) {
        const message = e.message
        listReportBatches(getToken)
          .then(({ batches }) => {
            const running = batches.find(b => b.status === ReportBatchRunning)
            if (running) setBatch(running)
            else setError(message)
          })
          .catch(() => setError(message))
      } else {
        setError(e instanceof Error ? e.message : 'Generation failed')
      }
    } finally {
      setPosting(false)
    }
  }

  async function handleRetry() {
    if (!batch) return
    setRetrying(true)
    setError(null)
    try {
      setBatch(await retryReportBatch(batch.id, getToken))
    } catch (e: unknown) {
      const message = e instanceof Error ? e.message : 'Retry failed'
      if (!(e instanceof ApiError && e.status === 409)) {
        setError(message)
        return
      }
      // Another tab retried or started a batch: show where it stands.
      try {
        const { batches } = await listReportBatches(getToken)
        const latest = batches.find(b => b.id === batch.id) ?? batches[0]
        if (latest) setBatch(latest)
        // Still finished with failures: the other request holds the lock, so say why nothing moved.
        if (!latest || hasRetryableFailures(latest)) setError(message)
      } catch {
        setError(message)
      }
    } finally {
      setRetrying(false)
    }
  }

  const classNameByStudent = useMemo(() => {
    const byId = new Map<number, string>()
    for (const c of classes) for (const s of c.students) byId.set(s.id, c.name)
    return byId
  }, [classes])

  async function handleExpand(reportId: number) {
    if (expandedReportId === reportId) {
      requestedReportId.current = null
      setExpandedReportId(null)
      setExpandedReport(null)
      return
    }
    requestedReportId.current = reportId
    setExpandedReportId(reportId)
    setError(null)
    try {
      const report = await getReport(reportId, getToken)
      if (requestedReportId.current === reportId) setExpandedReport(report)
    } catch (e: unknown) {
      if (requestedReportId.current !== reportId) return
      if (e instanceof ApiError && e.status === 404) {
        handleReportDelete(reportId)
        return
      }
      setError(e instanceof Error ? e.message : 'Failed to load report')
      setExpandedReportId(null)
    }
  }

  function handleReportRegenerate(updatedHtml: string) {
    setExpandedReport(prev => prev ? { ...prev, html: updatedHtml } : prev)
  }

  // Functional updates: the 404 path in handleExpand runs before its own
  // setExpandedReportId lands.
  function handleReportDelete(reportId: number) {
    setDeletedReportIds(prev => new Set(prev).add(reportId))
    setExpandedReportId(prev => (prev === reportId ? null : prev))
    setExpandedReport(prev => (prev?.id === reportId ? null : prev))
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

      {levelsError && (
        <div className="report-error" data-testid="levels-load-error">
          <p>⚠️ Could not load Levels: {levelsError}. Reload the page to try again.</p>
        </div>
      )}

      {/* Level report instructions — one read-only block per distinct selected Level */}
      {selectedLevels.length > 0 && (
        <div className="report-levels">
          {selectedLevels.map(level => {
            const isUnset = level.reportInstructions.trim() === ''
            return (
              <div
                key={level.id}
                className={`report-level-block${isUnset ? ' report-level-block-blocker' : ''}`}
                data-testid={isUnset ? 'level-instructions-blocker' : 'level-instructions-block'}
              >
                <h4 className="report-level-name">{level.name}</h4>
                {isUnset ? (
                  <p className="report-level-blocker-text">
                    No report instructions set. An admin must add report instructions for this
                    Level before reports can be generated.
                  </p>
                ) : (
                  <details className="report-level-instructions">
                    <summary>Report instructions</summary>
                    <p className="report-level-instructions-text">{level.reportInstructions}</p>
                  </details>
                )}
              </div>
            )
          })}
        </div>
      )}

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

      <button
        className="report-generate-btn"
        onClick={handleGenerate}
        disabled={posting || retrying || running || selectedCount === 0 || !startDate || !endDate || unsetLevels.length > 0 || !!levelsError || hasUnresolvedLevel}
      >
        {posting || retrying || running ? (
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

      {interrupted && (
        <div className="report-error" data-testid="report-batch-interrupted">
          <p>⚠️ The batch was interrupted. Check History.</p>
        </div>
      )}

      {/* Results */}
      <AnimatePresence>
        {batch && batch.students.length > 0 && (
          <motion.div
            className="report-results"
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
          >
            <h3>Reports for {formatDateRange(batch.startDate, batch.endDate)}</h3>
            <p className="report-results-summary" data-testid="report-batch-summary">{batchSummary(batch, deletedReportIds)}</p>
            {canRetry && (
              <button
                className="btn-sm btn-secondary report-batch-retry"
                onClick={handleRetry}
                disabled={retrying || posting}
                data-testid="report-batch-retry"
              >
                {retrying ? 'Retrying…' : 'Retry failed'}
              </button>
            )}
            <div className="report-results-list">
              {batch.students.map((r, i) => {
                const className = classNameByStudent.get(r.studentId)
                const name = (
                  <>
                    {r.name}{className && <span className="report-result-class"> ({className})</span>}
                  </>
                )
                const reportId = r.status === ReportStudentDone ? r.reportId : undefined
                const deleted = reportId !== undefined && deletedReportIds.has(reportId)
                const isExpanded = reportId !== undefined && expandedReportId === reportId
                return (
                  <motion.div
                    key={r.studentId}
                    className="report-result-card"
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ delay: i * 0.05 }}
                    layout
                  >
                    {reportId === undefined || deleted ? (
                      <StaticRow name={name} {...staticStatus(r, deleted)} />
                    ) : (
                      <div
                        className={`report-result-item${isExpanded ? ' report-result-item-expanded' : ''}`}
                        onClick={() => handleExpand(reportId)}
                        role="button"
                        tabIndex={0}
                        onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); handleExpand(reportId) } }}
                      >
                        <span className="report-result-name" data-testid="report-result-name">{name}</span>
                        <svg
                          width="16" height="16" viewBox="0 0 16 16" fill="none"
                          style={{ transform: isExpanded ? 'rotate(180deg)' : 'rotate(0deg)', transition: 'transform 0.2s', flexShrink: 0 }}
                        >
                          <path d="M4 6L8 10L12 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                      </div>
                    )}
                    <AnimatePresence>
                      {isExpanded && (
                        <motion.div
                          initial={{ opacity: 0, height: 0 }}
                          animate={{ opacity: 1, height: 'auto' }}
                          exit={{ opacity: 0, height: 0 }}
                          transition={{ duration: 0.25 }}
                          style={{ overflow: 'hidden' }}
                        >
                          {expandedReport?.id === reportId ? (
                            <ReportViewer
                              reportId={reportId}
                              html={expandedReport.html}
                              studentName={r.name}
                              onRegenerate={handleReportRegenerate}
                              onDelete={() => handleReportDelete(reportId)}
                            />
                          ) : (
                            <div className="report-loading-inline">
                              <div className="honeycomb-spinner">
                                <div className="hex" /><div className="hex" /><div className="hex" />
                              </div>
                            </div>
                          )}
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
