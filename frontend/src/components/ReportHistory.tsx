import { useEffect, useState, useCallback, useRef } from 'react'
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
import { formatDateRange, formatNoteDate } from '../lib/date'

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

export default function ReportHistory({ studentId, studentName }: ReportHistoryProps) {
  const { getToken } = useAuth()
  const [reports, setReports] = useState<ReportSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [expandedReport, setExpandedReport] = useState<ReportResult | null>(null)
  // The paragraph the last Tweak rewrote, for the new report's viewer to highlight.
  const [tweaked, setTweaked] = useState<{ reportId: number; paragraph: number } | null>(null)
  const [loadingReport, setLoadingReport] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Terms whose Earlier drafts are shown.
  const [openTerms, setOpenTerms] = useState<Set<number>>(new Set())
  // Auto-open of Latest races a quick click on another row: only the last request may land.
  const requestedId = useRef<number | null>(null)

  const openReport = useCallback(async (id: number) => {
    requestedId.current = id
    setExpandedId(id)
    setExpandedReport(null)
    setLoadingReport(true)
    try {
      const report = await getReport(id, getToken)
      if (requestedId.current !== id) return
      setExpandedReport(report)
    } catch (err) {
      if (requestedId.current !== id) return
      setError(err instanceof Error ? err.message : 'Failed to load report')
      setExpandedId(null)
    } finally {
      if (requestedId.current === id) setLoadingReport(false)
    }
  }, [getToken])

  const fetchReports = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { reports: fetched } = await listStudentReports(studentId, getToken)
      setReports(fetched || [])
      // Newest first from the server: open Latest.
      if (fetched?.[0]) void openReport(fetched[0].id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load reports')
    } finally {
      setLoading(false)
    }
  }, [studentId, getToken, openReport])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchReports()
  }, [fetchReports])

  function handleExpand(id: number) {
    setTweaked(null)
    if (expandedId === id) {
      requestedId.current = null
      setLoadingReport(false)
      setExpandedId(null)
      setExpandedReport(null)
      return
    }
    openReport(id)
  }

  function toggleEarlier(termId: number) {
    setOpenTerms(prev => {
      const next = new Set(prev)
      if (!next.delete(termId)) next.add(termId)
      return next
    })
  }

  // Regenerate saves a new row in the source's Term, now its Latest; the source moves to Earlier
  // drafts. Expansion follows only if the old row is still open: the Teacher may have moved on.
  function handleRegenerate(source: ReportSummary, report: ReportResult, paragraph?: number) {
    const oldId = source.id
    const { id, startDate, endDate, createdAt } = report
    setReports(prev => [{ ...source, id, startDate, endDate, createdAt }, ...prev])
    setExpandedId(prev => (prev === oldId ? id : prev))
    setExpandedReport(prev => (prev?.id === oldId ? report : prev))
    setTweaked(paragraph === undefined ? null : { reportId: report.id, paragraph })
  }

  function handleDelete(reportId: number) {
    setReports(prev => prev.filter(r => r.id !== reportId))
    if (expandedId === reportId) {
      setExpandedId(null)
      setExpandedReport(null)
    }
  }

  function renderEntry(r: ReportSummary, isLatest: boolean) {
    const isExpanded = expandedId === r.id
    return (
      <motion.div
        key={r.id}
        className={`report-history-entry${isLatest ? '' : ' report-history-draft'}`}
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        layout
      >
        <div
          className={`report-history-item${isExpanded ? ' report-history-item-expanded' : ''}`}
          onClick={() => handleExpand(r.id)}
          role="button"
          tabIndex={0}
          onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); handleExpand(r.id) } }}
        >
          <span className="report-history-label" title={isLatest ? `Created ${relativeTime(r.createdAt)}` : undefined}>
            Notes up to {formatNoteDate(r.endDate)}
            {isLatest && r.termCurrent && <span className="report-latest-badge">Latest</span>}
          </span>
          {/* Tweaks keep the source's end date: creation time tells drafts apart. */}
          {!isLatest && <span className="report-history-created">{relativeTime(r.createdAt)}</span>}
          <ChevronIcon open={isExpanded} />
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
              {loadingReport ? (
                <div className="report-loading-inline">
                  <div className="honeycomb-spinner">
                    <div className="hex" /><div className="hex" /><div className="hex" />
                  </div>
                </div>
              ) : expandedReport?.id === r.id ? (
                <ReportViewer
                  key={expandedReport.id}
                  reportId={expandedReport.id}
                  html={expandedReport.html}
                  studentName={studentName}
                  // Earlier drafts are read-only: regenerating one drops the newer draft's work.
                  onRegenerate={isLatest ? (report, paragraph) => handleRegenerate(r, report, paragraph) : undefined}
                  highlightParagraph={tweaked?.reportId === expandedReport.id ? tweaked.paragraph : undefined}
                  onDelete={() => handleDelete(expandedReport.id)}
                />
              ) : null}
            </motion.div>
          )}
        </AnimatePresence>
      </motion.div>
    )
  }

  if (loading) {
    return (
      <div className="report-history-loading">
        <div className="honeycomb-spinner">
          <div className="hex" /><div className="hex" /><div className="hex" />
        </div>
      </div>
    )
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
          {groupByTerm(reports).map(([latest, ...earlier]) => {
            const showEarlier = openTerms.has(latest.termId)
            const draftsId = `report-drafts-${latest.termId}`
            return (
              <section key={latest.termId} className="report-term-group">
                <h4 className="report-term-heading">
                  {latest.termName}{latest.termArchived && ' (archived)'}
                  <span className="report-term-dates"> · {formatDateRange(latest.termStartDate, latest.termEndDate)}</span>
                </h4>
                <div className="report-history-card">
                  {renderEntry(latest, true)}
                  {earlier.length > 0 && (
                    <button
                      type="button"
                      className="text-link report-earlier-toggle"
                      aria-expanded={showEarlier}
                      aria-controls={draftsId}
                      onClick={() => toggleEarlier(latest.termId)}
                    >
                      <ChevronIcon open={showEarlier} />
                      {showEarlier
                        ? 'Hide earlier drafts'
                        : `${earlier.length} earlier draft${earlier.length === 1 ? '' : 's'}`}
                    </button>
                  )}
                  <AnimatePresence initial={false}>
                    {showEarlier && (
                      <motion.div
                        id={draftsId}
                        className="report-earlier-drafts"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: 'auto' }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.25 }}
                        style={{ overflow: 'hidden' }}
                      >
                        {earlier.map(r => renderEntry(r, false))}
                      </motion.div>
                    )}
                  </AnimatePresence>
                </div>
              </section>
            )
          })}
        </div>
      )}
    </div>
  )
}
