import { type ReactNode } from 'react'

export interface InlineErrorProps {
  /** Bolded key value shown before children (e.g. the user's input verbatim) */
  title?: string
  /** Visual severity — defaults to 'error' */
  severity?: 'error' | 'warning' | 'info'
  /** If provided, renders a ✕ dismiss button */
  onDismiss?: () => void
  children: ReactNode
}

/**
 * InlineError renders a bordered card for inline, non-blocking error / warning /
 * info messages. Use `title` for a bolded key value (e.g. the user's input),
 * and `children` for the explanatory message. Supply `onDismiss` to add a ✕ button.
 */
export default function InlineError({
  title,
  severity = 'error',
  onDismiss,
  children,
}: InlineErrorProps) {
  const icon = severity === 'info' ? 'ℹ' : '⚠'

  return (
    <div className={`inline-error inline-error-${severity}`} role="alert">
      <span className="inline-error-icon" aria-hidden="true">{icon}</span>
      <span className="inline-error-body">
        {title && <strong className="inline-error-title">{title}</strong>}{' '}
        {children}
      </span>
      {onDismiss && (
        <button
          className="inline-error-dismiss icon-btn"
          onClick={onDismiss}
          aria-label="Dismiss"
          type="button"
        >
          ✕
        </button>
      )}
    </div>
  )
}
