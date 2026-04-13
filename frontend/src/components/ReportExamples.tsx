import { useState, useEffect, useCallback, useRef } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import ItemRow from './ItemRow'
import { PencilIcon } from './Icons'
import {
  listReportExamples,
  uploadReportExample,
  updateReportExample,
  deleteReportExample,
  importExampleFromDrive,
  getGoogleToken,
  type ReportExampleItem,
} from '../api'
import { useDrivePicker } from '../hooks/useDrivePicker'

const REPORT_MIME_TYPES = [
  'application/pdf',
  'image/png',
  'image/jpeg',
  'image/webp',
  'text/plain',
  'text/markdown',
].join(',')

const POLL_INTERVAL = 3000

function DriveIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" style={{ flexShrink: 0 }}>
      <path d="M8.01 2.56L1.38 14H7.37L14 2.56H8.01Z" fill="currentColor" opacity="0.8" />
      <path d="M22.62 14H10.38L7.37 19.44H19.61L22.62 14Z" fill="currentColor" opacity="0.6" />
      <path d="M14 2.56L22.62 14L19.61 19.44L11 7.56L14 2.56Z" fill="currentColor" opacity="0.4" />
    </svg>
  )
}

function ProcessingBadge() {
  return (
    <span className="example-status-badge processing">
      <span className="honeycomb-spinner" style={{ width: 14, height: 14 }} />
      Extracting…
    </span>
  )
}

function FailedBadge() {
  return <span className="example-status-badge failed">Extraction failed</span>
}

export default function ReportExamples() {
  const { getToken } = useAuth()
  const [examples, setExamples] = useState<ReportExampleItem[]>([])
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [driveImporting, setDriveImporting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dragOver, setDragOver] = useState(false)
  const [collapsed, setCollapsed] = useState(true)
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editName, setEditName] = useState('')
  const [editContent, setEditContent] = useState('')
  const [saving, setSaving] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const { openPicker } = useDrivePicker()

  const load = useCallback(async () => {
    try {
      const { examples } = await listReportExamples(() => getToken())
      setExamples(examples)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load examples')
    } finally {
      setLoading(false)
    }
  }, [getToken])

  useEffect(() => { load() }, [load])

  // Poll while any example is still processing.
  useEffect(() => {
    const hasProcessing = examples.some(e => e.status === 'processing')
    if (!hasProcessing) return
    const timer = setInterval(() => { load() }, POLL_INTERVAL)
    return () => clearInterval(timer)
  }, [examples, load])

  async function handleFiles(files: FileList | null) {
    if (!files || files.length === 0) return
    setUploading(true)
    setError(null)
    try {
      for (const file of Array.from(files)) {
        await uploadReportExample(file, () => getToken())
      }
      await load()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Upload failed')
    } finally {
      setUploading(false)
    }
  }

  async function handleDriveImport() {
    setError(null)
    try {
      const { accessToken } = await getGoogleToken(getToken)
      const picked = await openPicker(accessToken, {
        mimeTypes: REPORT_MIME_TYPES,
        title: 'Select a report card',
      })
      if (!picked || picked.length === 0) return
      setDriveImporting(true)
      await importExampleFromDrive(picked[0].id, picked[0].name, getToken)
      await load()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Drive import failed')
    } finally {
      setDriveImporting(false)
    }
  }

  function startEditing(ex: ReportExampleItem) {
    setEditingId(ex.id)
    setEditName(ex.name)
    setEditContent(ex.content)
  }

  function cancelEditing() {
    setEditingId(null)
  }

  async function saveEdit() {
    if (!editingId) return
    setSaving(true)
    setError(null)
    try {
      await updateReportExample(editingId, editName, editContent, () => getToken())
      await load()
      setEditingId(null)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Update failed')
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: number) {
    try {
      await deleteReportExample(id, () => getToken())
      setExamples(prev => prev.filter(e => e.id !== id))
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Delete failed')
    }
  }

  return (
    <div className="report-examples">
      <button
        className="report-examples-toggle"
        onClick={() => setCollapsed(!collapsed)}
        type="button"
      >
        <span className="toggle-arrow" style={{ transform: collapsed ? 'rotate(-90deg)' : 'rotate(0)' }}>▼</span>
        Example Report Cards
        {examples.length > 0 && (
          <span className="example-count-badge">{examples.length}</span>
        )}
      </button>

      <AnimatePresence>
        {!collapsed && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2 }}
            style={{ overflow: 'hidden' }}
          >
            {/* Drop zone */}
            <div
              className={`example-drop-zone ${dragOver ? 'drag-over' : ''}`}
              onDragOver={(e) => { e.preventDefault(); setDragOver(true) }}
              onDragLeave={() => setDragOver(false)}
              onDrop={(e) => {
                e.preventDefault()
                setDragOver(false)
                handleFiles(e.dataTransfer.files)
              }}
              onClick={() => fileInputRef.current?.click()}
            >
              <input
                ref={fileInputRef}
                type="file"
                accept=".txt,.md,.text,.pdf,.png,.jpg,.jpeg,.webp"
                multiple
                style={{ display: 'none' }}
                onChange={(e) => handleFiles(e.target.files)}
              />
              {uploading || driveImporting ? (
                <>
                  <div className="honeycomb-spinner" />
                  <p style={{ marginTop: '0.5rem', fontSize: '0.85rem', opacity: 0.7 }}>
                    {driveImporting ? 'Importing from Drive…' : 'Uploading…'}
                  </p>
                </>
              ) : (
                <p>Drop files here or click to upload<br/><span style={{ fontSize: '0.8rem', opacity: 0.6 }}>Text, PDF, or image files</span></p>
              )}
            </div>
            <div className="secondary-actions" style={{ marginTop: '0.5rem' }}>
              <button
                type="button"
                className="btn-secondary"
                onClick={(e) => { e.stopPropagation(); handleDriveImport() }}
                disabled={uploading || driveImporting}
              >
                <DriveIcon />
                Import from Drive
              </button>
            </div>

            {error && <p className="example-error">{error}</p>}

            {loading ? (
              <div className="honeycomb-spinner" />
            ) : examples.length === 0 ? (
              <p className="example-empty">No examples uploaded yet. Upload example report cards to guide the AI's writing style.</p>
            ) : (
              <div className="example-list">
                {examples.map((ex) => (
                  <motion.div
                    key={ex.id}
                    className="example-item-wrapper"
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                  >
                    <ItemRow
                      name={ex.name}
                      expanded={expandedId === ex.id}
                      onToggle={() => setExpandedId(expandedId === ex.id ? null : ex.id)}
                      onDelete={() => handleDelete(ex.id)}
                      badge={
                        ex.status === 'processing' ? <ProcessingBadge /> :
                        ex.status === 'failed' ? <FailedBadge /> :
                        undefined
                      }
                      actions={
                        ex.status === 'ready' ? (
                          <button
                            className="icon-btn"
                            onClick={(e) => { e.stopPropagation(); setExpandedId(ex.id); startEditing(ex) }}
                            aria-label={`Edit ${ex.name}`}
                          >
                            <PencilIcon />
                          </button>
                        ) : undefined
                      }
                    >
                      {editingId === ex.id ? (
                        <div className="example-edit-form">
                          <label className="example-edit-label">
                            Name
                            <input
                              className="example-edit-name"
                              value={editName}
                              onChange={(e) => setEditName(e.target.value)}
                            />
                          </label>
                          <label className="example-edit-label">
                            Content
                            <textarea
                              className="example-edit-content"
                              value={editContent}
                              onChange={(e) => setEditContent(e.target.value)}
                              rows={12}
                            />
                          </label>
                          <div className="example-edit-actions">
                            <button className="btn-secondary btn-sm" onClick={cancelEditing} disabled={saving}>Cancel</button>
                            <button className="btn-sm" onClick={saveEdit} disabled={saving || !editName.trim() || !editContent.trim()}>
                              {saving ? 'Saving…' : 'Save'}
                            </button>
                          </div>
                        </div>
                      ) : ex.status === 'processing' ? (
                        <div className="example-content-preview">
                          <p style={{ opacity: 0.6, fontStyle: 'italic' }}>Extracting text from document…</p>
                        </div>
                      ) : ex.status === 'failed' ? (
                        <div className="example-content-preview">
                          <p style={{ color: 'var(--error-red)' }}>Text extraction failed. You can delete this and try again.</p>
                        </div>
                      ) : (
                        <div className="example-content-preview">
                          <pre className="example-content-text">{ex.content}</pre>
                        </div>
                      )}
                    </ItemRow>
                  </motion.div>
                ))}
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
