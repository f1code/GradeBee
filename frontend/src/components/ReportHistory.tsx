import { useEffect, useState, useCallback } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import {
  listStudentReports,
  getReport,
  type ReportSummary,
  type ReportResult,
} from '../api'
import ReportViewer from './ReportViewer'

interface ReportHistoryProps {
  studentId: number
  studentName: string
}

function relativeTime(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)

  if (diffDay > 30) {
    const diffMonth = Math.floor(diffDay / 30)
    return diffMonth === 1 ? '1 month ago' : `${diffMonth} months ago`
  }
  if (diffDay > 0) return diffDay === 1 ? '1 day ago' : `${diffDay} days ago`
  if (diffHour > 0) return diffHour === 1 ? '1 hour ago' : `${diffHour} hours ago`
  if (diffMin > 0) return diffMin === 1 ? '1 minute ago' : `${diffMin} minutes ago`
  return 'just now'
}

function formatDateRange(startDate: string, endDate: string): string {
  const fmt = (d: string) => {
    const date = new Date(d + 'T00:00:00')
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
  }
  return `${fmt(startDate)} – ${fmt(endDate)}`
}

export default function ReportHistory({ studentId, studentName }: ReportHistoryProps) {
  const { getToken } = useAuth()
  const [reports, setReports] = useState<ReportSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [expandedReport, setExpandedReport] = useState<ReportResult | null>(null)
  const [loadingReport, setLoadingReport] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchReports = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { reports: fetched } = await listStudentReports(studentId, getToken)
      setReports(fetched || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load reports')
    } finally {
      setLoading(false)
    }
  }, [studentId, getToken])

  useEffect(() => {
    fetchReports()
  }, [fetchReports])

  async function handleExpand(id: number) {
    if (expandedId === id) {
      setExpandedId(null)
      setExpandedReport(null)
      return
    }
    setExpandedId(id)
    setExpandedReport(null)
    setLoadingReport(true)
    try {
      const report = await getReport(id, getToken)
      setExpandedReport(report)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load report')
      setExpandedId(null)
    } finally {
      setLoadingReport(false)
    }
  }

  function handleRegenerate(updatedHtml: string) {
    if (expandedReport) {
      setExpandedReport({ ...expandedReport, html: updatedHtml })
    }
  }

  function handleDelete(reportId: number) {
    setReports(prev => prev.filter(r => r.id !== reportId))
    if (expandedId === reportId) {
      setExpandedId(null)
      setExpandedReport(null)
    }
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
      <h4 className="report-history-heading">Past Reports</h4>

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
          <AnimatePresence initial={false}>
            {reports.map((r, i) => {
              const isExpanded = expandedId === r.id
              return (
                <motion.div
                  key={r.id}
                  className="report-history-card"
                  initial={{ opacity: 0, y: 8 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: i * 0.05 }}
                  layout
                >
                  <div
                    className={`report-history-item${isExpanded ? ' report-history-item-expanded' : ''}`}
                    onClick={() => handleExpand(r.id)}
                    role="button"
                    tabIndex={0}
                    onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); handleExpand(r.id) } }}
                  >
                    <span className="report-history-date-range">
                      {formatDateRange(r.startDate, r.endDate)}
                    </span>
                    <span className="report-history-created">
                      {relativeTime(r.createdAt)}
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
                        {loadingReport ? (
                          <div className="report-history-loading-inline">
                            <div className="honeycomb-spinner">
                              <div className="hex" /><div className="hex" /><div className="hex" />
                            </div>
                          </div>
                        ) : expandedReport ? (
                          <ReportViewer
                            reportId={expandedReport.id}
                            html={expandedReport.html}
                            studentName={studentName}
                            onRegenerate={handleRegenerate}
                            onDelete={() => handleDelete(expandedReport.id)}
                          />
                        ) : null}
                      </motion.div>
                    )}
                  </AnimatePresence>
                </motion.div>
              )
            })}
          </AnimatePresence>
        </div>
      )}
    </div>
  )
}
