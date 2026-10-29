import { useState, useCallback, useEffect, useMemo, useRef, type CSSProperties } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import DOMPurify from 'dompurify'
import { regenerateReport, deleteReport, submitFeedback, type ReportResult } from '../api'
import { ThumbUpIcon, ThumbDownIcon, CopyIcon, CheckIcon, WandIcon } from './Icons'
import AIBadge from './AIBadge'

interface ReportViewerProps {
  reportId: number
  html: string
  studentName: string
  // paragraph: the block a Tweak rewrote; undefined for a whole-report regenerate.
  onRegenerate?: (report: ReportResult, paragraph?: number) => void
  highlightParagraph?: number
  onDelete?: () => void
}

export default function ReportViewer({
  reportId,
  html,
  studentName,
  onRegenerate,
  onDelete,
  highlightParagraph,
}: ReportViewerProps) {
  const { getToken } = useAuth()
  const [copied, setCopied] = useState(false)
  const [showRegenerateForm, setShowRegenerateForm] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [regenerating, setRegenerating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // The p/li block a Tweak rewrites (index in querySelectorAll('p, li'), as the
  // server numbers them) and its opening text; null for a whole-report regenerate.
  const [tweak, setTweak] = useState<{ paragraph: number; excerpt: string; quote: string } | null>(null)
  const tweakQuote = tweak?.quote ?? null
  const canTweak = !!onRegenerate

  // Thumbs feedback state
  const [thumbRating, setThumbRating] = useState<'up' | 'down' | null>(null)
  const [showThumbComment, setShowThumbComment] = useState(false)
  const [thumbComment, setThumbComment] = useState('')
  const [thumbSubmitting, setThumbSubmitting] = useState(false)
  const [thumbDone, setThumbDone] = useState(false)

  const sanitizedHtml = useMemo(() => DOMPurify.sanitize(html), [html])
  // React 19 compares this prop by identity: a fresh object each render would
  // rewrite the frame's innerHTML and detach every block.
  const frameHtml = useMemo(() => ({ __html: sanitizedHtml }), [sanitizedHtml])
  const frameRef = useRef<HTMLDivElement | null>(null)
  const bodyRef = useRef<HTMLDivElement | null>(null)
  const toolbarRef = useRef<HTMLDivElement | null>(null)
  const pointerInside = useRef(false)
  const feedbackRef = useRef<HTMLTextAreaElement | null>(null)

  // The form sits below the report: focus brings it into view.
  useEffect(() => {
    if (tweak) feedbackRef.current?.focus()
  }, [tweak])

  // Same index the server spliced: a tweak keeps the block count.
  useEffect(() => {
    if (highlightParagraph === undefined) return
    const block = frameRef.current?.querySelectorAll('p, li')[highlightParagraph]
    if (!block) return
    block.classList.add('para-tweaked')
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    // ponytail: waits out the parents' 0.25s expand; the target moves until it ends.
    const timer = window.setTimeout(() => block.scrollIntoView({ block: 'center', behavior: reduce ? 'auto' : 'smooth' }), 300)
    return () => window.clearTimeout(timer)
  }, [highlightParagraph, sanitizedHtml])

  // The paragraph the toolbar serves. Parents key the viewer by report id, so
  // its html never changes underneath.
  const [shown, setShown] = useState<{ el: HTMLElement; paragraph: number; top: number; bottom: number; right: number } | null>(null)
  const [copiedEl, setCopiedEl] = useState<HTMLElement | null>(null)

  // List items are header data and placeholders, not prose: p only. The index
  // still counts li, as the server numbers blocks.
  const activate = useCallback((node: Node | null | undefined) => {
    const frame = frameRef.current
    const body = bodyRef.current
    const el = node instanceof Element ? node : node?.parentElement
    if (!frame || !body || !el || !frame.contains(el)) return
    const p = el.closest('p')
    if (!p || !(p.textContent || '').trim()) {
      // The frame's own margins keep it, so the pointer can reach a toolbar
      // that overhangs a short paragraph.
      if (el !== frame) setShown(null)
      return
    }
    const rect = p.getBoundingClientRect()
    const origin = body.getBoundingClientRect()
    const paragraph = Array.from(frame.querySelectorAll('p, li')).indexOf(p)
    setShown({
      el: p,
      paragraph,
      top: rect.top - origin.top,
      bottom: rect.bottom - origin.top,
      right: origin.right - rect.right,
    })
  }, [])

  // Selection reaches the toolbar without a mouse: caret browsing, long-press.
  useEffect(() => {
    const onSelection = () => activate(document.getSelection()?.anchorNode)
    document.addEventListener('selectionchange', onSelection)
    return () => document.removeEventListener('selectionchange', onSelection)
  }, [activate])

  async function handleParagraphCopy() {
    if (!shown) return
    const el = shown.el
    try {
      await navigator.clipboard.writeText((el.textContent || '').trim())
    } catch {
      return
    }
    setCopiedEl(el)
    window.setTimeout(() => setCopiedEl(c => (c === el ? null : c)), 1500)
  }

  function handleParagraphTweak() {
    if (!shown) return
    const text = (shown.el.textContent || '').trim().replace(/\s+/g, ' ')
    // By code point: a split surrogate pair fails the server's prefix check.
    const chars = Array.from(text)
    const excerpt = chars.slice(0, 60).join('')
    setTweak({ paragraph: shown.paragraph, excerpt, quote: chars.length > 60 ? `${excerpt}…` : text })
    setShowRegenerateForm(true)
  }

  function closeRegenerateForm() {
    setShowRegenerateForm(false)
    setFeedback('')
    setTweak(null)
  }

  const handleCopy = useCallback(async () => {
    const aiFooter = '\n\nPortions of this report were generated by AI and reviewed by the teacher.'
    const htmlWithFooter = sanitizedHtml + `<p style="font-size:0.8em;color:#7A6B5D;">${aiFooter.trim()}</p>`
    const textContent = new DOMParser().parseFromString(sanitizedHtml, 'text/html').body.textContent || ''
    try {
      // Try to copy as HTML for rich paste into email/Word
      const blob = new Blob([htmlWithFooter], { type: 'text/html' })
      const textBlob = new Blob([textContent + aiFooter], { type: 'text/plain' })
      await navigator.clipboard.write([
        new ClipboardItem({
          'text/html': blob,
          'text/plain': textBlob,
        }),
      ])
    } catch {
      // Fallback: plain text
      await navigator.clipboard.writeText(textContent + aiFooter)
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [sanitizedHtml])

  async function handleRegenerate() {
    if (!feedback.trim()) return
    setRegenerating(true)
    setError(null)
    try {
      const target = tweak ? { paragraph: tweak.paragraph, excerpt: tweak.excerpt } : undefined
      const result = await regenerateReport(reportId, feedback.trim(), getToken, target)
      closeRegenerateForm()
      onRegenerate?.(result, target?.paragraph)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Regeneration failed')
    } finally {
      setRegenerating(false)
    }
  }

  async function handleThumb(rating: 'up' | 'down') {
    if (thumbSubmitting || thumbDone) return
    if (rating === 'down') {
      setThumbRating('down')
      setShowThumbComment(true)
      return
    }
    // Thumbs-up: fire immediately
    setThumbSubmitting(true)
    try {
      await submitFeedback({ artifact_type: 'report', artifact_id: reportId, rating: 'up' }, getToken)
      setThumbRating('up')
      setThumbDone(true)
    } catch {
      setError('Could not save feedback — please try again.')
    } finally {
      setThumbSubmitting(false)
    }
  }

  async function handleThumbDownSubmit() {
    setThumbSubmitting(true)
    try {
      await submitFeedback({
        artifact_type: 'report',
        artifact_id: reportId,
        rating: 'down',
        comment: thumbComment.trim() || undefined,
      }, getToken)
      setThumbDone(true)
      setShowThumbComment(false)
    } catch {
      setError('Could not save feedback — please try again.')
    } finally {
      setThumbSubmitting(false)
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
            onClick={() => {
              setTweak(null)
              setShowRegenerateForm(!showRegenerateForm || !!tweakQuote)
            }}
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
            style={{ overflow: 'hidden' }}
          >
            <span>⚠️ {error}</span>
          </motion.div>
        )}
      </AnimatePresence>

      {/* AI label */}
      <div className="report-ai-label">
        <AIBadge /> Please review before sharing or saving to official records
      </div>

      {/* Report HTML content; the toolbar is a sibling since React owns no children of the frame */}
      <div
        ref={bodyRef}
        className="report-viewer-body"
        onMouseOver={e => {
          pointerInside.current = true
          activate(e.target as Node)
        }}
        onMouseLeave={() => {
          pointerInside.current = false
          // Unmounting a focused button would drop focus to <body>: wait for blur.
          if (toolbarRef.current?.contains(document.activeElement)) return
          const sel = document.getSelection()
          if (sel && !sel.isCollapsed && frameRef.current?.contains(sel.anchorNode)) return
          setShown(null)
        }}
      >
        <div
          ref={frameRef}
          className="report-viewer-frame"
          dangerouslySetInnerHTML={frameHtml}
        />
        {shown && (
          // preventDefault on mousedown keeps the teacher's selection
          <div
            ref={toolbarRef}
            className="para-toolbar"
            style={{ '--para-top': `${shown.top}px`, '--para-bottom': `${shown.bottom}px`, right: shown.right } as CSSProperties}
            onMouseDown={e => e.preventDefault()}
            onBlur={e => {
              if (!pointerInside.current && !toolbarRef.current?.contains(e.relatedTarget)) setShown(null)
            }}>
            <button
              type="button"
              className={`para-btn${copiedEl === shown.el ? ' para-btn-success' : ''}`}
              aria-label="Copy paragraph"
              title="Copy"
              onClick={handleParagraphCopy}
            >
              {copiedEl === shown.el ? <CheckIcon /> : <CopyIcon />}
            </button>
            {canTweak && (
              <button type="button" className="para-btn" aria-label="Tweak paragraph" title="Tweak" onClick={handleParagraphTweak}>
                <WandIcon />
              </button>
            )}
          </div>
        )}
      </div>

      {/* Thumbs feedback — explicit rating below the content, above the regen box.
          A user can click 👎 + comment AND ALSO click Regenerate: each produces its own
          feedback row (append-only design). We don't dedupe by intent. */}
      <div className="report-thumbs" data-testid="report-thumbs">
        {thumbDone ? (
          <span className="report-thumbs-done">Thanks for your feedback!</span>
        ) : (
          <>
            <span className="report-thumbs-label">Was this report helpful?</span>
            <button
              className={`icon-btn report-thumb-btn${thumbRating === 'up' ? ' report-thumb-active' : ''}`}
              aria-label="Thumbs up"
              data-testid="thumb-up"
              disabled={thumbSubmitting}
              onClick={() => handleThumb('up')}
            >
              <ThumbUpIcon />
            </button>
            <button
              className={`icon-btn report-thumb-btn${thumbRating === 'down' ? ' report-thumb-active' : ''}`}
              aria-label="Thumbs down"
              data-testid="thumb-down"
              disabled={thumbSubmitting}
              onClick={() => handleThumb('down')}
            >
              <ThumbDownIcon />
            </button>
          </>
        )}
      </div>

      {/* Thumbs-down comment box */}
      <AnimatePresence>
        {showThumbComment && !thumbDone && (
          <motion.div
            className="report-thumb-comment"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.2 }}
            style={{ overflow: 'hidden' }}
          >
            <textarea
              value={thumbComment}
              onChange={e => setThumbComment(e.target.value)}
              placeholder="What was wrong? (optional)"
              rows={2}
              className="report-regenerate-textarea"
              data-testid="thumb-down-comment"
              aria-describedby={`thumb-down-privacy-hint-${reportId}`}
            />
            <p
              className="thumb-comment-privacy-hint"
              id={`thumb-down-privacy-hint-${reportId}`}
              data-testid="thumb-down-privacy-hint"
            >
              Please avoid student names — comments are sent to our diagnostics provider.
            </p>
            <div className="report-regenerate-actions">
              <button
                className="btn-sm"
                onClick={handleThumbDownSubmit}
                disabled={thumbSubmitting}
                data-testid="thumb-down-submit"
              >
                {thumbSubmitting ? 'Saving…' : 'Submit'}
              </button>
              <button
                className="btn-secondary btn-sm"
                onClick={() => { setShowThumbComment(false); setThumbRating(null) }}
              >
                Cancel
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Regenerate form */}
      <AnimatePresence>
        {showRegenerateForm && (
          <motion.div
            className="report-regenerate-form"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.2 }}
            style={{ overflow: 'hidden' }}
          >
            {tweakQuote && (
              <p className="report-tweak-quote" data-testid="report-tweak-quote">
                Tweaking: “{tweakQuote}”
              </p>
            )}
            <textarea
              ref={feedbackRef}
              value={feedback}
              onChange={e => setFeedback(e.target.value)}
              placeholder={tweakQuote
                ? "How should this paragraph change? e.g. 'About 50 words, add some general statements'"
                : "What should be different? e.g. 'Make it shorter', 'Focus more on math skills'"}
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
                onClick={closeRegenerateForm}
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
