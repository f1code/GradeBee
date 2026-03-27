import { useState, useCallback } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import DOMPurify from 'dompurify'
import { regenerateReport, deleteReport } from '../api'

interface ReportViewerProps {
  reportId: number
  html: string
  studentName: string
  onRegenerate?: (updatedHtml: string) => void
  onDelete?: () => void
}

export default function ReportViewer({
  reportId,
  html,
  studentName,
  onRegenerate,
  onDelete,
}: ReportViewerProps) {
  const { getToken } = useAuth()
  const [copied, setCopied] = useState(false)
  const [showRegenerateForm, setShowRegenerateForm] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [regenerating, setRegenerating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const sanitizedHtml = DOMPurify.sanitize(html)

  const handleCopy = useCallback(async () => {
    try {
      // Try to copy as HTML for rich paste into email/Word
      const blob = new Blob([sanitizedHtml], { type: 'text/html' })
      const textBlob = new Blob([new DOMParser().parseFromString(sanitizedHtml, 'text/html').body.textContent || ''], { type: 'text/plain' })
      await navigator.clipboard.write([
        new ClipboardItem({
          'text/html': blob,
          'text/plain': textBlob,
        }),
      ])
    } catch {
      // Fallback: plain text
      const text = new DOMParser().parseFromString(sanitizedHtml, 'text/html').body.textContent || ''
      await navigator.clipboard.writeText(text)
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [sanitizedHtml])

  async function handleRegenerate() {
    if (!feedback.trim()) return
    setRegenerating(true)
    setError(null)
    try {
      const result = await regenerateReport(reportId, feedback.trim(), getToken)
      setShowRegenerateForm(false)
      setFeedback('')
      onRegenerate?.(result.html)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Regeneration failed')
    } finally {
      setRegenerating(false)
    }
  }

  function handleDelete() {
    if (!confirm(`Delete this report for ${studentName}?`)) return
    deleteReport(reportId, getToken).then(() => {
      onDelete?.()
    }).catch((e) => {
      setError(e instanceof Error ? e.message : 'Delete failed')
    })
  }

  return (
    <motion.div
      className="report-viewer"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: 0.25 }}
    >
      {/* Action bar */}
      <div className="report-viewer-actions">
        <button
          className={`btn-primary btn-sm report-copy-btn${copied ? ' report-copy-btn-success' : ''}`}
          onClick={handleCopy}
        >
          {copied ? '✓ Copied!' : 'Copy to Clipboard'}
        </button>
        {onRegenerate && (
          <button
            className="btn-secondary btn-sm"
            onClick={() => setShowRegenerateForm(!showRegenerateForm)}
          >
            Regenerate
          </button>
        )}
        {onDelete && (
          <button
            className="btn-secondary btn-sm report-delete-btn"
            onClick={handleDelete}
          >
            Delete
          </button>
        )}
      </div>

      {/* Error */}
      <AnimatePresence>
        {error && (
          <motion.div
            className="report-viewer-error"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            exit={{ opacity: 0, height: 0 }}
          >
            <span>⚠️ {error}</span>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Report HTML content */}
      <div
        className="report-viewer-frame"
        style={{ userSelect: 'all' }}
        dangerouslySetInnerHTML={{ __html: sanitizedHtml }}
      />

      {/* Regenerate form */}
      <AnimatePresence>
        {showRegenerateForm && (
          <motion.div
            className="report-regenerate-form"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.2 }}
          >
            <textarea
              value={feedback}
              onChange={e => setFeedback(e.target.value)}
              placeholder="What should be different? e.g. 'Make it shorter', 'Focus more on math skills'"
              rows={3}
              className="report-regenerate-textarea"
            />
            <div className="report-regenerate-actions">
              <button
                className="btn-primary btn-sm"
                onClick={handleRegenerate}
                disabled={regenerating || !feedback.trim()}
              >
                {regenerating ? (
                  <span className="btn-loading">
                    <span className="honeycomb-spinner honeycomb-spinner-sm" />
                    Regenerating...
                  </span>
                ) : (
                  'Submit'
                )}
              </button>
              <button
                className="btn-secondary btn-sm"
                onClick={() => { setShowRegenerateForm(false); setFeedback('') }}
              >
                Cancel
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  )
}
