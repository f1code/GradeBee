import { useState, useCallback, useEffect, useRef } from 'react'
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
  const frameRef = useRef<HTMLDivElement | null>(null)

  // Attach per-paragraph copy buttons after the sanitized HTML is rendered.
  useEffect(() => {
    const frame = frameRef.current
    if (!frame) return

    const blocks = frame.querySelectorAll<HTMLElement>('p, li')
    const cleanups: Array<() => void> = []

    blocks.forEach(block => {
      // Skip empty/whitespace-only blocks
      if (!(block.textContent || '').trim()) return
      // Avoid double-decorating on re-render
      if (block.querySelector(':scope > .para-copy-btn')) return

      block.classList.add('has-para-copy')

      const btn = document.createElement('button')
      btn.type = 'button'
      btn.className = 'para-copy-btn'
      btn.setAttribute('aria-label', 'Copy paragraph')
      btn.textContent = 'Copy'
      // Don't let clicking the button clear the user's selection
      btn.addEventListener('mousedown', e => e.preventDefault())

      const onClick = async (e: MouseEvent) => {
        e.stopPropagation()
        // Clone and strip copy buttons so their label isn't included in the text.
        const clone = block.cloneNode(true) as HTMLElement
        clone.querySelectorAll('.para-copy-btn').forEach(el => el.remove())
        const text = (clone.textContent || '').trim()
        if (!text) return
        try {
          await navigator.clipboard.writeText(text)
        } catch {
          return
        }
        btn.textContent = '✓ Copied'
        btn.classList.add('para-copy-btn-success')
        window.setTimeout(() => {
          btn.textContent = 'Copy'
          btn.classList.remove('para-copy-btn-success')
        }, 1500)
      }
      btn.addEventListener('click', onClick)

      block.appendChild(btn)
      cleanups.push(() => {
        btn.removeEventListener('click', onClick)
        btn.remove()
        block.classList.remove('has-para-copy')
      })
    })

    return () => cleanups.forEach(fn => fn())
  }, [sanitizedHtml])

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
          className={`btn-sm report-copy-btn${copied ? ' report-copy-btn-success' : ''}`}
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
        ref={frameRef}
        className="report-viewer-frame"
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
                className="btn-sm"
                onClick={handleRegenerate}
                disabled={regenerating || !feedback.trim()}
              >
                {regenerating ? (
                  <span className="btn-loading">
                    <span className="honeycomb-spinner honeycomb-spinner-inline"><span className="hex" /><span className="hex" /><span className="hex" /></span>
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
