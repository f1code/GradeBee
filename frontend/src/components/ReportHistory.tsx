import { useEffect, useMemo, useState, useCallback, useRef } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import {
  listStudentReports,
  getReport,
  type ReportSummary,
  type ReportResult,
} from '../api'
import ReportViewer from './ReportViewer'
import { ChevronIcon } from './Icons'
import { formatNoteDate } from '../lib/date'

interface ReportHistoryProps {
  studentId: number
  studentName: string
}

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })

function relativeTime(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffSec = Math.round((date.getTime() - now.getTime()) / 1000)
  const diffMin = Math.round(diffSec / 60)
  const diffHour = Math.round(diffMin / 60)
  const diffDay = Math.round(diffHour / 24)
  const diffMonth = Math.round(diffDay / 30)

  if (Math.abs(diffMonth) >= 1) return rtf.format(diffMonth, 'month')
  if (Math.abs(diffDay) >= 1) return rtf.format(diffDay, 'day')
  if (Math.abs(diffHour) >= 1) return rtf.format(diffHour, 'hour')
  if (Math.abs(diffMin) >= 1) return rtf.format(diffMin, 'minute')
  return rtf.format(diffSec, 'second')
}

// Keeps the server's newest-first order inside each Term; Terms newest first by start date.
function groupByTerm(reports: ReportSummary[]): ReportSummary[][] {
  const groups = new Map<number, ReportSummary[]>()
  for (const r of reports) {
    const g = groups.get(r.termId)
    if (g) g.push(r)
    else groups.set(r.termId, [r])
  }
  return [...groups.values()].sort((a, b) => b[0].termStartDate.localeCompare(a[0].termStartDate))
}

function toggled<T>(prev: Set<T>, v: T): Set<T> {
  const next = new Set(prev)
  if (!next.delete(v)) next.add(v)
  return next
}

const fold = {
  initial: { opacity: 0, height: 0 },
  animate: { opacity: 1, height: 'auto' },
  exit: { opacity: 0, height: 0 },
  transition: { duration: 0.25 },
}

const spinner = (
  <div className="honeycomb-spinner">
    <div className="hex" /><div className="hex" /><div className="hex" />
  </div>
)

export default function ReportHistory({ studentId, studentName }: ReportHistoryProps) {
  const { getToken } = useAuth()
  const [reports, setReports] = useState<ReportSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  // Fetched reports by id; null when the fetch failed.
  const [bodies, setBodies] = useState<Record<number, ReportResult | null>>({})
  // Fetch each report once: a failed fetch must not retry on every render.
  const requested = useRef(new Set<number>())
  const [openTerms, setOpenTerms] = useState<Set<number>>(new Set())
  const [showEarlier, setShowEarlier] = useState(false)
  const [openDrafts, setOpenDrafts] = useState<Set<number>>(new Set())
  // The paragraph the last Tweak rewrote, for the new report's viewer to highlight.
  const [tweaked, setTweaked] = useState<{ reportId: number; paragraph: number } | null>(null)

  const groups = useMemo(() => groupByTerm(reports), [reports])
  // An open Term always shows its newest report.
  const wanted = useMemo(
    () => [...groups.filter(g => openTerms.has(g[0].termId)).map(g => g[0].id), ...openDrafts],
    [groups, openTerms, openDrafts],
  )

  useEffect(() => {
    for (const id of wanted) {
      if (requested.current.has(id)) continue
      requested.current.add(id)
      getReport(id, getToken)
        .then(r => {
          setBodies(prev => ({ ...prev, [id]: r }))
          setError(null)
        })
        .catch(err => {
          setBodies(prev => ({ ...prev, [id]: null }))
          setError(err instanceof Error ? err.message : 'Failed to load report')
        })
    }
  }, [wanted, getToken])

  const fetchReports = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { reports: fetched } = await listStudentReports(studentId, getToken)
      setReports(fetched || [])
      const newest = groupByTerm(fetched || [])[0]
      setOpenTerms(new Set(newest ? [newest[0].termId] : []))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load reports')
    } finally {
      setLoading(false)
    }
  }, [studentId, getToken])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchReports()
  }, [fetchReports])

  // Only toggling retries failed loads, so a retry always starts from the spinner.
  function dropFailed() {
    for (const [id, b] of Object.entries(bodies)) if (b === null) requested.current.delete(Number(id))
    setBodies(prev => Object.fromEntries(Object.entries(prev).filter(([, b]) => b !== null)))
  }

  function toggleTerm(termId: number) {
    setTweaked(null)
    dropFailed()
    setOpenTerms(prev => toggled(prev, termId))
  }

  function toggleDraft(id: number) {
    setTweaked(null)
    dropFailed()
    setOpenDrafts(prev => toggled(prev, id))
  }

  // Regenerate saves a new row in the source's Term, which becomes the Term's shown report.
  function handleRegenerate(source: ReportSummary, report: ReportResult, paragraph?: number) {
    const { id, startDate, endDate, createdAt } = report
    requested.current.add(id)
    setBodies(prev => ({ ...prev, [id]: report }))
    setReports(prev => [{ ...source, id, startDate, endDate, createdAt }, ...prev])
    setTweaked(paragraph === undefined ? null : { reportId: id, paragraph })
  }

  function handleDelete(reportId: number) {
    setReports(prev => prev.filter(r => r.id !== reportId))
  }

  function renderViewer(r: ReportSummary, editable: boolean) {
    const body = bodies[r.id]
    if (body === undefined) return <div className="report-loading-inline">{spinner}</div>
    if (body === null) return <p className="report-history-failed">Could not load this report.</p>
    return (
      <ReportViewer
        key={body.id}
        reportId={body.id}
        html={body.html}
        studentName={studentName}
        // Drafts are read-only: regenerating one drops the newer report's work.
        onRegenerate={editable ? (report, paragraph) => handleRegenerate(r, report, paragraph) : undefined}
        highlightParagraph={tweaked?.reportId === body.id ? tweaked.paragraph : undefined}
        onDelete={() => handleDelete(body.id)}
      />
    )
  }

  function renderDraft(r: ReportSummary) {
    const isOpen = openDrafts.has(r.id)
    return (
      <div key={r.id} className="report-history-entry report-history-draft">
        <div
          className={`report-history-item${isOpen ? ' report-history-item-expanded' : ''}`}
          onClick={() => toggleDraft(r.id)}
          role="button"
          tabIndex={0}
          aria-expanded={isOpen}
          onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleDraft(r.id) } }}
        >
          <span className="report-history-label">Notes up to {formatNoteDate(r.endDate)}</span>
          {/* Tweaks keep the source's end date: creation time tells drafts apart. */}
          <span className="report-history-created">{relativeTime(r.createdAt)}</span>
          <ChevronIcon open={isOpen} />
        </div>
        <AnimatePresence>
          {isOpen && (
            <motion.div
              {...fold}
              style={{ overflow: 'hidden' }}
            >
              {renderViewer(r, false)}
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    )
  }

  if (loading) {
    return <div className="report-history-loading">{spinner}</div>
  }

  return (
    <div className="report-history">

      {error && (
        <motion.div
          className="report-viewer-error"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
        >
          <span>⚠️ {error}</span>
        </motion.div>
      )}

      {reports.length === 0 ? (
        <div className="info-box report-history-empty">
          <p>No reports generated yet for {studentName}.</p>
        </div>
      ) : (
        <div className="report-history-list">
          {groups.map(([shown, ...earlier]) => {
            const open = openTerms.has(shown.termId)
            const cardId = `report-term-${shown.termId}`
            // Past Terms show only their final report.
            const drafts = shown.termCurrent ? earlier : []
            return (
              <section key={shown.termId} className="report-term-group">
                <h4 className="report-term-heading">
                  <button
                    type="button"
                    className="text-link report-term-toggle"
                    aria-expanded={open}
                    aria-controls={cardId}
                    onClick={() => toggleTerm(shown.termId)}
                  >
                    <span>
                      {shown.termName}{shown.termArchived && ' (archived)'}
                      <span className="report-term-dates"> · {formatNoteDate(shown.termStartDate)} – {formatNoteDate(shown.termEndDate)}</span>
                    </span>
                    <ChevronIcon open={open} />
                  </button>
                </h4>
                <AnimatePresence initial={false}>
                  {open && (
                    <motion.div
                      id={cardId}
                      className="report-history-card"
                      {...fold}
                    >
                      {renderViewer(shown, true)}
                      {drafts.length > 0 && (
                        <button
                          type="button"
                          className="text-link report-earlier-toggle"
                          aria-expanded={showEarlier}
                          aria-controls="report-earlier-drafts"
                          onClick={() => setShowEarlier(v => !v)}
                        >
                          <ChevronIcon open={showEarlier} />
                          {showEarlier
                            ? 'Hide earlier drafts'
                            : `${drafts.length} earlier draft${drafts.length === 1 ? '' : 's'}`}
                        </button>
                      )}
                      <AnimatePresence initial={false}>
                        {drafts.length > 0 && showEarlier && (
                          <motion.div
                            id="report-earlier-drafts"
                            className="report-earlier-drafts"
                            {...fold}
                            style={{ overflow: 'hidden' }}
                          >
                            {drafts.map(renderDraft)}
                          </motion.div>
                        )}
                      </AnimatePresence>
                    </motion.div>
                  )}
                </AnimatePresence>
              </section>
            )
          })}
        </div>
      )}
    </div>
  )
}

