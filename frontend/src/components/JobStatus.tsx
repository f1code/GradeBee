import { useAuth } from '@clerk/react'
import React, { useCallback, useEffect, useRef, useState } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { fetchJobs, retryFailedJobs, dismissJobs } from '../api'
import type { UploadJob, JobListResponse } from '../api'
import StudentDetail from './StudentDetail'
import TranscriptReview from './TranscriptReview'

/** Polling intervals in milliseconds. */
const POLL_ACTIVE_MS = 3_000
const POLL_IDLE_MS = 60_000

/** Stop polling entirely when there's nothing to show. */
const POLL_EMPTY_MS = 0 // 0 = don't schedule, wait for pollNow

/** Max recently-completed jobs to display. */
const MAX_DONE_SHOWN = 5

const STATUS_LABELS: Record<string, string> = {
  queued: 'Queued',
  transcribing: 'Transcribing',
  extracting: 'Analyzing transcript',
  creating_notes: 'Creating notes',
}

function HoneycombSpinner() {
  return (
    <div className="honeycomb-spinner honeycomb-spinner-sm">
      <div className="hex" />
      <div className="hex" />
      <div className="hex" />
    </div>
  )
}

function DocIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
      <path d="M4 2H10L13 5V14H4V2Z" stroke="currentColor" strokeWidth="1.2" fill="none" />
      <path d="M10 2V5H13" stroke="currentColor" strokeWidth="1.2" />
      <line x1="6" y1="8" x2="11" y2="8" stroke="currentColor" strokeWidth="1" />
      <line x1="6" y1="10.5" x2="11" y2="10.5" stroke="currentColor" strokeWidth="1" />
    </svg>
  )
}

export default function JobStatus({ pollNowRef }: { pollNowRef?: React.MutableRefObject<(() => void) | null> }) {
  const { getToken } = useAuth()
  const [jobs, setJobs] = useState<JobListResponse | null>(null)
  const [retrying, setRetrying] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [newDoneIds, setNewDoneIds] = useState<Set<number>>(new Set())
  const [modalStudent, setModalStudent] = useState<{ studentId: number; name: string; className: string } | null>(null)
  const prevDoneIdsRef = useRef<Set<number>>(new Set())
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const poll = useCallback(async () => {
    try {
      const data = await fetchJobs(getToken)
      setJobs(data)
      setError(null)

      // Detect newly completed jobs.
      const currentDoneIds = new Set(data.done.map(j => j.uploadId))
      const fresh = new Set<number>()
      for (const id of currentDoneIds) {
        if (!prevDoneIdsRef.current.has(id)) {
          fresh.add(id)
        }
      }
      if (fresh.size > 0) {
        setNewDoneIds(prev => new Set([...prev, ...fresh]))
      }
      prevDoneIdsRef.current = currentDoneIds

      // Schedule next poll – stop entirely when there's nothing to show.
      const hasAny = data.active.length > 0 || data.failed.length > 0 || data.done.length > 0
      const interval = data.active.length > 0
        ? POLL_ACTIVE_MS
        : hasAny
          ? POLL_IDLE_MS
          : POLL_EMPTY_MS
      if (interval > 0) {
        timerRef.current = setTimeout(poll, interval)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load jobs')
      timerRef.current = setTimeout(poll, POLL_IDLE_MS)
    }
  }, [getToken])

  // Pause polling when tab is hidden.
  useEffect(() => {
    poll()

    function handleVisibility() {
      if (document.hidden) {
        if (timerRef.current) { clearTimeout(timerRef.current); timerRef.current = null }
      } else {
        // Resume immediately when tab becomes visible.
        if (!timerRef.current) poll()
      }
    }
    document.addEventListener('visibilitychange', handleVisibility)

    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  }, [poll])

  // Expose an imperative "poll now" handle for parent components.
  useEffect(() => {
    if (pollNowRef) {
      pollNowRef.current = () => {
        if (timerRef.current) clearTimeout(timerRef.current)
        poll()
      }
      return () => { pollNowRef.current = null }
    }
  }, [pollNowRef, poll])

  async function handleRetry() {
    setRetrying(true)
    try {
      await retryFailedJobs(getToken)
      // Immediately re-poll to reflect the change.
      if (timerRef.current) clearTimeout(timerRef.current)
      await poll()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Retry failed')
    } finally {
      setRetrying(false)
    }
  }

  function dismissNewBadge(uploadId: number) {
    setNewDoneIds(prev => {
      const next = new Set(prev)
      next.delete(uploadId)
      return next
    })
  }

  async function dismissDoneJob(uploadId: number) {
    try {
      await dismissJobs(getToken, [uploadId])
      if (timerRef.current) clearTimeout(timerRef.current)
      await poll()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Dismiss failed')
    }
  }

  async function dismissAllDone() {
    const ids = jobs?.done.map(j => j.uploadId) ?? []
    if (ids.length === 0) return
    try {
      await dismissJobs(getToken, ids)
      if (timerRef.current) clearTimeout(timerRef.current)
      await poll()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Dismiss failed')
    }
  }

  // Don't render anything if there are no jobs at all.
  if (!jobs) return null
  const hasContent = jobs.active.length > 0 || jobs.failed.length > 0 || jobs.done.length > 0
  const doneSlice = jobs.done.slice(0, MAX_DONE_SHOWN)
  if (!hasContent) return null

  return (
    <motion.div
      className="job-status"
      data-testid="job-status"
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, delay: 0.1 }}
    >
      <h3>Processing</h3>

      {error && (
        <div className="job-error" data-testid="job-error">
          <p>{error}</p>
        </div>
      )}

      {/* Active jobs */}
      <AnimatePresence>
        {jobs.active.map(job => (
          <motion.div
            key={job.uploadId}
            className="job-card job-card-active"
            data-testid="job-active"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.25 }}
          >
            <div className="job-card-row">
              <HoneycombSpinner />
              <div className="job-card-info">
                <span className="job-file-name">{job.fileName}</span>
                <span className="job-status-label">{STATUS_LABELS[job.status] ?? job.status}</span>
              </div>
            </div>
          </motion.div>
        ))}
      </AnimatePresence>

      {/* Failed jobs */}
      {jobs.failed.length > 0 && (
        <div className="job-section-failed" data-testid="job-failed-section">
          {jobs.failed.map(job => (
            <div key={job.uploadId} className="job-card job-card-failed" data-testid="job-failed">
              <div className="job-card-row">
                <span className="job-failed-icon">✕</span>
                <div className="job-card-info">
                  <span className="job-file-name">{job.fileName}</span>
                  <span className="job-error-msg">{job.error}</span>
                </div>
              </div>
            </div>
          ))}
          <button
            className="btn-secondary job-retry-btn"
            onClick={handleRetry}
            disabled={retrying}
            data-testid="job-retry-btn"
          >
            {retrying ? 'Retrying…' : 'Retry All'}
          </button>
        </div>
      )}

      {/* Recently completed jobs */}
      {doneSlice.length > 0 && (
        <div className="job-section-done" data-testid="job-done-section">
          <div className="job-section-done-header">
            <button className="text-link" onClick={dismissAllDone} data-testid="job-clear-all">
              Clear all
            </button>
          </div>
          {doneSlice.map(job => (
            <DoneJobCard
              key={job.uploadId}
              job={job}
              isNew={newDoneIds.has(job.uploadId)}
              onDismissNew={() => dismissNewBadge(job.uploadId)}
              onDismiss={() => dismissDoneJob(job.uploadId)}
              onOpenStudent={(s) => setModalStudent(s)}
            />
          ))}
        </div>
      )}

      {/* Student detail modal */}
      <AnimatePresence>
        {modalStudent && (
          <motion.div
            className="student-modal-overlay"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={() => setModalStudent(null)}
            data-testid="student-modal-overlay"
          >
            <motion.div
              className="student-modal-card card"
              initial={{ opacity: 0, y: 30, scale: 0.97 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 20 }}
              transition={{ duration: 0.3, ease: 'easeOut' }}
              onClick={(e) => e.stopPropagation()}
            >
              <button className="student-modal-close" onClick={() => setModalStudent(null)} aria-label="Close">×</button>
              <StudentDetail
                studentId={modalStudent.studentId}
                studentName={modalStudent.name}
                className={modalStudent.className}
                onCollapse={() => setModalStudent(null)}
                modal
              />
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  )
}

function DoneJobCard({ job, isNew, onDismissNew, onDismiss, onOpenStudent }: { job: UploadJob; isNew: boolean; onDismissNew: () => void; onDismiss: () => void; onOpenStudent: (link: { studentId: number; name: string; className: string }) => void }) {
  const noteCount = job.noteLinks?.length ?? 0
  const [showTranscript, setShowTranscript] = useState(false)

  return (
    <motion.div
      className="job-card job-card-done"
      data-testid="job-done"
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, height: 0, marginTop: 0, marginBottom: 0, overflow: 'hidden' }}
      transition={{ duration: 0.2 }}
    >
      <div className="job-card-row">
        <span className="job-done-icon">✓</span>
        <div className="job-card-info">
          <span className="job-file-name">
            {job.fileName}
            {isNew && (
              <span className="job-new-badge" onClick={onDismissNew} data-testid="job-new-badge">
                new
              </span>
            )}
          </span>
          <span className="job-done-meta">
            {noteCount === 0 ? 'No notes created' : `${noteCount} note${noteCount !== 1 ? 's' : ''} created`}
          </span>
        </div>
        <button className="job-dismiss-btn" onClick={onDismiss} title="Dismiss" data-testid="job-dismiss">
          <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
            <line x1="2" y1="2" x2="8" y2="8" /><line x1="8" y1="2" x2="2" y2="8" />
          </svg>
        </button>
      </div>
      {job.noteLinks && job.noteLinks.length > 0 && (
        <div className="job-note-links">
          {job.noteLinks.map((link, i) => (
            <button key={i} className="job-note-link" onClick={() => onOpenStudent({ studentId: link.studentId, name: link.name, className: link.className })}>
              <DocIcon /> {link.name}
            </button>
          ))}
        </div>
      )}
      {job.transcript && (
        <>
          <button
            className="text-link"
            onClick={() => setShowTranscript(v => !v)}
          >
            {showTranscript ? 'Hide transcript' : 'View transcript'}
          </button>
          <AnimatePresence>
            {showTranscript && (
              <motion.div
                initial={{ opacity: 0, height: 0 }}
                animate={{ opacity: 1, height: 'auto' }}
                exit={{ opacity: 0, height: 0 }}
                transition={{ duration: 0.2 }}
              >
                <TranscriptReview
                  transcript={job.transcript}
                  noteLinks={job.noteLinks ?? []}
                />
              </motion.div>
            )}
          </AnimatePresence>
        </>
      )}
    </motion.div>
  )
}
