import { useState, useRef } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import ItemRow from './ItemRow'
import { PencilIcon } from './Icons'
import { type ReportExampleItem } from '../api'

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

interface LevelNameTagsProps {
  levelNames: string[]
}

function LevelNameTags({ levelNames }: LevelNameTagsProps) {
  if (!levelNames || levelNames.length === 0) return null
  return (
    <span className="level-name-tags">
      {levelNames.map(n => (
        <span key={n} className="level-name-tag">{n}</span>
      ))}
    </span>
  )
}

interface ClassNamesSelectProps {
  available: string[]
  selected: string[]
  onChange: (selected: string[]) => void
}

function ClassNamesSelect({ available, selected, onChange }: ClassNamesSelectProps) {
  function toggle(name: string) {
    if (selected.includes(name)) {
      onChange(selected.filter(n => n !== name))
    } else {
      onChange([...selected, name])
    }
  }

  if (available.length === 0) {
    return <p className="level-names-empty">No classes yet — add a class first, then come back to assign it.</p>
  }

  return (
    <div className="level-names-select" role="group">
      {available.map(name => {
        const isSelected = selected.includes(name)
        return (
          <label
            key={name}
            className={`level-names-option${isSelected ? ' is-selected' : ''}`}
          >
            <input
              type="checkbox"
              checked={isSelected}
              onChange={() => toggle(name)}
            />
            <span className="level-names-option-check" aria-hidden="true">✓</span>
            <span className="level-names-option-label">{name}</span>
          </label>
        )
      })}
    </div>
  )
}

interface ReportExamplesProps {
  examples: ReportExampleItem[]
  loading: boolean
  error: string | null
  availableLevelNames: string[]
  selectedLevelNames?: string[]
  onUpload: (files: File[], levelNames: string[]) => Promise<void>
  onDriveImport: () => Promise<void>
  onUpdate: (id: number, name: string, content: string, levelNames: string[]) => Promise<void>
  onDelete: (id: number) => Promise<void>
}

export default function ReportExamples({
  examples,
  loading,
  error,
  availableLevelNames,
  selectedLevelNames,
  onUpload,
  onDriveImport,
  onUpdate,
  onDelete,
}: ReportExamplesProps) {
  const [uploading, setUploading] = useState(false)
  const [driveImporting, setDriveImporting] = useState(false)
  const [dragOver, setDragOver] = useState(false)
  const [collapsed, setCollapsed] = useState(true)
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editName, setEditName] = useState('')
  const [editContent, setEditContent] = useState('')
  const [editLevelNames, setEditLevelNames] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  // Upload level names selection state
  const [pendingFiles, setPendingFiles] = useState<File[] | null>(null)
  const [uploadLevelNames, setUploadLevelNames] = useState<string[]>([])
  const fileInputRef = useRef<HTMLInputElement>(null)

  function handleFiles(files: FileList | null) {
    if (!files || files.length === 0) return
    // Collect files and show class name picker
    setPendingFiles(Array.from(files))
    setUploadLevelNames([])
  }

  async function confirmUpload() {
    if (!pendingFiles) return
    setUploading(true)
    try {
      await onUpload(pendingFiles, uploadLevelNames)
    } finally {
      setUploading(false)
      setPendingFiles(null)
      setUploadLevelNames([])
    }
  }

  async function handleDriveImport() {
    setDriveImporting(true)
    try {
      await onDriveImport()
    } finally {
      setDriveImporting(false)
    }
  }

  function startEditing(ex: ReportExampleItem) {
    setEditingId(ex.id)
    setEditName(ex.name)
    setEditContent(ex.content)
    setEditLevelNames(ex.levelNames || [])
  }

  function cancelEditing() {
    setEditingId(null)
  }

  async function saveEdit() {
    if (!editingId) return
    setSaving(true)
    try {
      await onUpdate(editingId, editName, editContent, editLevelNames)
      setEditingId(null)
    } catch {
      // error surfaced by parent via the error prop; keep edit form open
    } finally {
      setSaving(false)
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
            {/* Pending upload class selection */}
            {pendingFiles && (
              <div className="upload-levelnames-panel">
                <p className="upload-levelnames-title">
                Which level {pendingFiles.length > 1 ? 'are these examples' : 'is this example'} for?
                </p>
                <p className="upload-levelnames-help">
                  Pick the level{availableLevelNames.length > 1 ? 's' : ''} this should guide — reports for the
                  selected level{availableLevelNames.length > 1 ? 's' : ''} will follow its writing style.
                </p>
                <ul className="upload-levelnames-files">
                  {pendingFiles.map((f, i) => (
                    <li key={`${f.name}-${i}`} className="upload-levelnames-file">{f.name}</li>
                  ))}
                </ul>
                {availableLevelNames.length > 0 && (
                  <p className="upload-levelnames-steplabel">Choose a level</p>
                )}
                <ClassNamesSelect
                  available={availableLevelNames}
                  selected={uploadLevelNames}
                  onChange={setUploadLevelNames}
                />
                {availableLevelNames.length > 0 && uploadLevelNames.length === 0 && (
                  <p className="upload-levelnames-hint">Select at least one level to continue.</p>
                )}
                <div className="upload-levelnames-actions">
                  <button
                    className="btn-secondary btn-sm"
                    onClick={() => { setPendingFiles(null); setUploadLevelNames([]) }}
                  >
                    Cancel
                  </button>
                  <button
                    className="btn-sm"
                    onClick={confirmUpload}
                    disabled={uploadLevelNames.length === 0 || uploading}
                  >
                    {uploading ? 'Uploading…' : 'Upload'}
                  </button>
                </div>
              </div>
            )}

            {/* Drop zone */}
            {!pendingFiles && (
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
            )}

            {!pendingFiles && (
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
            )}

            {error && <p className="example-error">{error}</p>}

            {loading ? (
              <div className="honeycomb-spinner" />
            ) : examples.length === 0 ? (
              <p className="example-empty">No examples uploaded yet. Upload example report cards to guide the AI's writing style.</p>
            ) : (
              <div className="example-list">
                {(() => {
                  const hasSelection = !!selectedLevelNames && selectedLevelNames.length > 0
                  const matchingReadyCount = hasSelection
                    ? examples.filter(e => e.status === 'ready' && (e.levelNames ?? []).some(cn => selectedLevelNames!.includes(cn))).length
                    : 0
                  return (
                    <>
                      {hasSelection && matchingReadyCount > 0 && (
                        <p className="example-selection-summary" data-testid="example-selection-summary">
                          {matchingReadyCount} example{matchingReadyCount !== 1 ? 's' : ''} will guide these reports.
                        </p>
                      )}
                      {examples.map((ex) => {
                        const isMatching = hasSelection && (ex.levelNames ?? []).some(cn => selectedLevelNames!.includes(cn))
                        const isDimmed = hasSelection && !isMatching
                        return (
                          <motion.div
                            key={ex.id}
                            className={`example-item-wrapper${isMatching ? ' example-item-wrapper--matching' : ''}${isDimmed ? ' example-item-wrapper--dimmed' : ''}`}
                            initial={{ opacity: 0, x: -10 }}
                            animate={{ opacity: 1, x: 0 }}
                          >
                    <ItemRow
                      name={ex.name}
                      expanded={expandedId === ex.id}
                      onToggle={() => setExpandedId(expandedId === ex.id ? null : ex.id)}
                      onDelete={() => onDelete(ex.id)}
                      badge={
                        ex.status === 'processing' ? <ProcessingBadge /> :
                        ex.status === 'failed' ? <FailedBadge /> :
                        <LevelNameTags levelNames={ex.levelNames || []} />
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
                            Levels
                          </label>
                          <ClassNamesSelect
                            available={availableLevelNames}
                            selected={editLevelNames}
                            onChange={setEditLevelNames}
                          />
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
                            <button
                              className="btn-sm"
                              onClick={saveEdit}
                              disabled={saving || !editName.trim() || !editContent.trim() || editLevelNames.length === 0}
                            >
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
                        )
                      })}
                    </>
                  )
                })()}
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
