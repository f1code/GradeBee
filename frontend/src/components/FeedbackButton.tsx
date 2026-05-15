import { useState, useRef, useEffect } from 'react'
import * as Sentry from '@sentry/react'

type FeedbackType = 'bug' | 'suggestion'

interface FeedbackButtonProps {
  /** Clerk user id, attached to Sentry events */
  userId: string
  /** Clerk user email, attached to Sentry events */
  userEmail: string
}

/**
 * Floating action button (bottom-right) that lets authenticated teachers
 * report bugs or suggest features via Sentry User Feedback.
 *
 * Two distinct entry points produce events tagged with `feedback_type`:
 *   - "bug"        → copy oriented around what broke
 *   - "suggestion" → copy oriented around improvements
 *
 * Sentry handles:
 *   - User id + email (set via setUser before the widget opens)
 *   - Current URL / route
 *   - Browser + OS user-agent
 *   - App version / release (set in Sentry.init)
 *   - Session Replay (~30 s before submit) when triggered
 *   - Default text-input masking (on by default in Replay)
 *
 * Student names: appear only as filenames in the audio-upload flow and are
 * not captured in replay DOM snapshots. Audio URLs are backend-only.
 */
export default function FeedbackButton({ userId, userEmail }: FeedbackButtonProps) {
  const [open, setOpen] = useState(false)
  const popoverRef = useRef<HTMLDivElement>(null)

  // Close popover when clicking outside
  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open])

  // Close on Escape
  useEffect(() => {
    if (!open) return
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [open])

  async function openWidget(type: FeedbackType) {
    setOpen(false)

    // Attach user context so all feedback events carry teacher identity.
    // This is not student data — only the authenticated teacher's info is set.
    Sentry.setUser({ id: userId, email: userEmail })

    const feedback = Sentry.getFeedback()
    if (!feedback) {
      // Sentry not initialised (e.g. no DSN in local dev). Fail silently.
      console.warn('[FeedbackButton] Sentry Feedback integration not available.')
      return
    }

    const isBug = type === 'bug'
    const form = await feedback.createForm({
      formTitle: isBug ? 'Report a bug' : 'Suggest a feature',
      messagePlaceholder: isBug
        ? 'Describe what happened and what you expected instead…'
        : 'What improvement or new feature would help you most?',
      submitButtonLabel: isBug ? 'Send bug report' : 'Send suggestion',
      tags: { feedback_type: type },
    })
    form.appendToDom()
    form.open()
  }

  return (
    <div className="feedback-fab-wrapper" ref={popoverRef}>
      {open && (
        <div className="feedback-popover" role="menu" aria-label="Feedback options">
          <button
            className="feedback-popover-item"
            role="menuitem"
            onClick={() => openWidget('bug')}
          >
            <span className="feedback-popover-icon" aria-hidden="true">🐛</span>
            <span>
              <strong>Report a bug</strong>
              <span className="feedback-popover-desc">Something isn't working right</span>
            </span>
          </button>
          <button
            className="feedback-popover-item"
            role="menuitem"
            onClick={() => openWidget('suggestion')}
          >
            <span className="feedback-popover-icon" aria-hidden="true">💡</span>
            <span>
              <strong>Suggest a feature</strong>
              <span className="feedback-popover-desc">An idea to make GradeBee better</span>
            </span>
          </button>
        </div>
      )}
      <button
        className="feedback-fab"
        aria-label="Give feedback"
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((v) => !v)}
        title="Report a bug or suggest a feature"
      >
        <FeedbackIcon />
      </button>
    </div>
  )
}

function FeedbackIcon() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M20 2H4C2.9 2 2 2.9 2 4v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2z"
        fill="currentColor"
      />
      <circle cx="8" cy="11" r="1.2" fill="#FBF7F0" />
      <circle cx="12" cy="11" r="1.2" fill="#FBF7F0" />
      <circle cx="16" cy="11" r="1.2" fill="#FBF7F0" />
    </svg>
  )
}
