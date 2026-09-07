import { useAuth } from '@clerk/react'
import React, { useCallback, useEffect, useRef, useState } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { fetchJobs, retryFailedJobs, dismissJobs, assembleNotes, assignPassages, undoAssignment } from '../api'
import type { UploadJob, JobListResponse, AssembleNotesResponse, AssignPassagesRequest, NoteLink, JobPassage } from '../api'
import { NoNotesClassUnclear, NoNotesNoNameMatched, NoNotesNobodyNamed } from '../api-types.gen'
import ClassPicker from './ClassPicker'
import PassageReview from './PassageReview'
import StudentDetail from './StudentDetail'
import TranscriptReview from './TranscriptReview'

/** Polling intervals in milliseconds. */
const POLL_ACTIVE_MS = 3_000
const POLL_IDLE_MS = 60_000

/** Stop polling entirely when there's nothing to show. */
const POLL_EMPTY_MS = 0 // 0 = don't schedule, wait for pollNow

/** Max recently-completed jobs to display. */
const MAX_DONE_SHOWN = 5

/**
 * Fold a poll's done list into the cards this tab already shows: a card seen
 * once is kept even after the server forgets it, and the polled copy wins for
 * a card still in the queue, so a retained card keeps picking up server state.
 * Newest first, capped at MAX_DONE_SHOWN — the oldest expires when a sixth
 * arrives.
 */
function mergeDone(retained: UploadJob[], incoming: UploadJob[]): UploadJob[] {
  const byId = new Map([...retained, ...incoming].map(j => [j.uploadId, j]))
  return [...byId.values()]
    .sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))
    .slice(0, MAX_DONE_SHOWN)
}

/**
 * Fold note links this tab made into the ones the card holds, keyed on note
 * id: a link already held replaces nothing. An assign response, and the poll
 * bringing back what assign wrote to the job, both land here.
 */
function mergeLinks(held: NoteLink[], incoming: NoteLink[]): NoteLink[] {
  const out = [...held]
  for (const link of incoming) {
    if (!out.some(l => l.noteId === link.noteId)) out.push(link)
  }
  return out
}

/**
 * One empty array for every card with no passages. A fresh `[]` per render
 * would hand PassageReview a new prop on every poll.
 */
const NO_PASSAGES: JobPassage[] = []

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
  const [retainedDone, setRetainedDone] = useState<UploadJob[]>([])
  const [retrying, setRetrying] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [newDoneIds, setNewDoneIds] = useState<Set<number>>(new Set())
  const [modalStudent, setModalStudent] = useState<{ studentId: number; name: string; className: string } | null>(null)
  const prevDoneIdsRef = useRef<Set<number>>(new Set())
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // `poll` reads the retained cards from a ref, not from state: state in its
  // closure would land in the useCallback deps, churn poll's identity and make
  // the mount effect re-run and schedule a second timer.
  const retainedRef = useRef<UploadJob[]>([])
  const dismissedRef = useRef<Set<number>>(new Set())
  // Each poll takes a generation and only the newest arms the next timer. A
  // dismiss or a retry starts a poll while an older one is in flight, and
  // clearing a timer that has already fired does nothing — so both chains
  // would re-arm, and a retained card would keep the spare one alive for the
  // life of the tab.
  const pollGenRef = useRef(0)

  const setRetained = useCallback((next: UploadJob[]) => {
    retainedRef.current = next
    setRetainedDone(next)
  }, [])

  const poll = useCallback(async () => {
    const gen = ++pollGenRef.current
    try {
      const data = await fetchJobs(getToken)
      setJobs(data)
      setError(null)

      // The job queue is in memory, so a restart empties the done list. Keep
      // every card this tab has shown: only a dismiss or the teacher's own
      // refresh ends a review.
      const merged = mergeDone(
        retainedRef.current,
        data.done.filter(j => !dismissedRef.current.has(j.uploadId)),
      )
      setRetained(merged)

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

      // Schedule next poll – stop entirely when there's nothing to show. A
      // retained card counts: on a live server the idle poll is what brings it
      // fresh state.
      const hasAny = data.active.length > 0 || data.failed.length > 0 || merged.length > 0
      const interval = data.active.length > 0
        ? POLL_ACTIVE_MS
        : hasAny
          ? POLL_IDLE_MS
          : POLL_EMPTY_MS
      if (interval > 0 && gen === pollGenRef.current) {
        // eslint-disable-next-line react-hooks/immutability
        timerRef.current = setTimeout(poll, interval)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load jobs')
      if (gen === pollGenRef.current) {
        timerRef.current = setTimeout(poll, POLL_IDLE_MS)
      }
    }
  }, [getToken, setRetained])

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

  // Removal is local and final, before any await: the server may have
  // forgotten the job (a `dismissed: 0` reply is fine) or refuse the call, and
  // either way a card the teacher dismissed must not come back on the next poll.
  function forget(ids: number[]) {
    for (const id of ids) dismissedRef.current.add(id)
    setRetained(retainedRef.current.filter(j => !ids.includes(j.uploadId)))
  }

  async function dismissDoneJob(uploadId: number) {
    forget([uploadId])
    try {
      await dismissJobs(getToken, [uploadId])
      if (timerRef.current) clearTimeout(timerRef.current)
      await poll()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Dismiss failed')
    }
  }

  async function dismissAllDone() {
    // Everything the teacher could reach: the cards on screen, plus any the
    // server still lists behind the cap. "Clear all" leaving cards to pop
    // back on the next poll would read as a broken button.
    const ids = [...new Set([...retainedRef.current.map(j => j.uploadId), ...(jobs?.done.map(j => j.uploadId) ?? [])])]
    if (ids.length === 0) return
    forget(ids)
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
  // The error counts as content: dismissing the last card empties the panel,
  // and a refused dismiss would otherwise have nowhere to say so. The cost is
  // that a failed poll now shows its line on an empty board, where nothing
  // used to render; the next good poll clears it.
  const hasContent = jobs.active.length > 0 || jobs.failed.length > 0 || retainedDone.length > 0 || error !== null
  // mergeDone owns the cap, so every retained card renders.
  const doneSlice = retainedDone
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
            style={{ overflow: 'hidden' }}
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

// A job can finish with no note for three different reasons and the teacher can
// act on two of them, so one line for all three sends them the wrong way — or
// nowhere. The cases come from api-types.gen, not from string literals: rename
// a constant in Go and this stops compiling, where a literal would fall through
// to the generic line and show every teacher nothing.
//
function noNotesMessage(reason: string | undefined): string {
  switch (reason) {
    case NoNotesClassUnclear:
      return "No notes — the class wasn't clear. Say the class and time at the start."
    case NoNotesNoNameMatched:
      return 'No notes — no names matched your roster.'
    case NoNotesNobodyNamed:
      return 'No notes — nobody was named.'
    default:
      return 'No notes created'
  }
}

function DoneJobCard({ job, isNew, onDismissNew, onDismiss, onOpenStudent }: { job: UploadJob; isNew: boolean; onDismissNew: () => void; onDismiss: () => void; onOpenStudent: (link: { studentId: number; name: string; className: string }) => void }) {
  const [showTranscript, setShowTranscript] = useState(false)
  // What the assemble call returned, if the teacher has picked a class on this
  // card. It wins over the polled job for the life of the card: the poll only
  // agrees while the job is still in the queue, and the card must not flip back
  // to the picker over notes that now exist.
  const [assembled, setAssembled] = useState<AssembleNotesResponse | null>(null)
  // Note links the teacher made from this card by filing passages by hand.
  // Component state only: review lives in the tab that saw the card, and a
  // refresh ends it, by design.
  const [assignedLinks, setAssignedLinks] = useState<NoteLink[]>([])
  // Notes an undo on this card deleted. The server drops them from the job,
  // but the poll that shows it may be a minute off, and an assembled result
  // is never polled again, so the card hides them itself. An id leaves the
  // set the moment a later call hands it back: the notes table has no
  // AUTOINCREMENT, so the next note made after a delete can take the id the
  // deleted one had, and that note is real.
  const [undoneNoteIds, setUndoneNoteIds] = useState<Set<number>>(new Set())
  const { getToken } = useAuth()

  const base: UploadJob = assembled
    ? { ...job, className: assembled.className, classId: assembled.classId, noteLinks: assembled.noteLinks, passages: assembled.passages, noNotesReason: assembled.noNotesReason, canPickClass: assembled.canPickClass }
    : job
  const view: UploadJob = { ...base, noteLinks: mergeLinks(base.noteLinks ?? [], assignedLinks).filter(l => !undoneNoteIds.has(l.noteId)) }
  const noteCount = view.noteLinks?.length ?? 0

  // The server decides this, and the card obeys. Not a list of the reasons this
  // component happens to know about: that made an affordance out of a cause, so
  // a reason added later silently took the picker off a card that could still
  // be rescued, and it made the server name a cause it could not know in order
  // to keep the picker up.
  //
  // Not a passage count either. A recording can come back with passages that
  // named nobody — a block the teacher only ever said "she" about, a class-wide
  // remark — which no picked class can resolve, and a declined recording
  // carries no passages at all, so counting them would hide the button on the
  // card that needs it most.
  //
  // It stays up after a pick that made no note. Picking the wrong one of two
  // sibling classes is the mistake this path exists to undo; without a local
  // assembled result here the picker would vanish and there would be no second
  // attempt.
  const needsClass = noteCount === 0 && (view.canPickClass ?? false)

  async function pickClass(className: string) {
    setAssembled(await assembleNotes(job.uploadId, className, getToken))
    // The response is fresh from the server, after every undo it saw.
    setUndoneNoteIds(new Set())
  }

  // A child who already has a note from this recording gets the rows appended
  // to it, not a second note. The card, not the review, knows the links: the
  // pipeline's, the class picker's, one an earlier assign here made, or one
  // the poll brought back after a reload. The response then names a note the
  // card already holds and merges in as nothing new.
  async function assign(body: AssignPassagesRequest) {
    const held = view.noteLinks?.find(l => l.studentId === body.studentId)
    const res = await assignPassages(job.uploadId, held ? { ...body, appendToNoteId: held.noteId } : body, getToken)
    setAssignedLinks(prev => mergeLinks(prev, [{ name: res.name, noteId: res.noteId, studentId: res.studentId, className: res.className }]))
    setUndoneNoteIds(prev => {
      if (!prev.has(res.noteId)) return prev
      const next = new Set(prev)
      next.delete(res.noteId)
      return next
    })
    return res
  }

  // Take back what this card filed to one child (#138). The server names the
  // notes it deleted; their links leave the card at once, so the count is
  // right and the child is no longer a link, and a later pick of the same
  // child makes a new note rather than appending to one that is gone.
  async function undo(studentId: number) {
    const res = await undoAssignment(job.uploadId, studentId, getToken)
    setUndoneNoteIds(prev => new Set([...prev, ...res.noteIds]))
    setAssignedLinks(prev => prev.filter(l => !res.noteIds.includes(l.noteId)))
  }

  return (
    <motion.div
      className="job-card job-card-done"
      data-testid="job-done"
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, height: 0, marginTop: 0, marginBottom: 0 }}
      transition={{ duration: 0.2 }}
      style={{ overflow: 'hidden' }}
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
            {noteCount === 0 ? noNotesMessage(view.noNotesReason) : `${noteCount} note${noteCount !== 1 ? 's' : ''} created`}
          </span>
        </div>
        <button className="job-dismiss-btn" onClick={onDismiss} title="Dismiss" data-testid="job-dismiss">
          <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
            <line x1="2" y1="2" x2="8" y2="8" /><line x1="8" y1="2" x2="2" y2="8" />
          </svg>
        </button>
      </div>
      {needsClass && <ClassPicker onPick={pickClass} />}
      {view.noteLinks && view.noteLinks.length > 0 && (
        <div className="job-note-links">
          {view.noteLinks.map((link, i) => (
            <button key={i} className="job-note-link" data-testid="job-note-link" onClick={() => onOpenStudent({ studentId: link.studentId, name: link.name, className: link.className })}>
              <DocIcon /> {link.name}
            </button>
          ))}
        </div>
      )}
      <PassageReview passages={view.passages ?? NO_PASSAGES} classId={view.classId} onAssign={assign} onUndo={undo} />
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
                style={{ overflow: 'hidden' }}
              >
                <TranscriptReview
                  transcript={job.transcript}
                  noteLinks={view.noteLinks ?? []}
                />
              </motion.div>
            )}
          </AnimatePresence>
        </>
      )}
    </motion.div>
  )
}
