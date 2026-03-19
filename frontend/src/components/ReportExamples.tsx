import { useState, useEffect, useCallback, useRef } from 'react'
import { useAuth } from '@clerk/react'
import { motion, AnimatePresence } from 'motion/react'
import {
  listReportExamples,
  uploadReportExample,
  deleteReportExample,
  type ReportExampleItem,
} from '../api'

export default function ReportExamples() {
  const { getToken } = useAuth()
  const [examples, setExamples] = useState<ReportExampleItem[]>([])
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dragOver, setDragOver] = useState(false)
  const [collapsed, setCollapsed] = useState(true)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    try {
      const { examples } = await listReportExamples(() => getToken())
      setExamples(examples)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [getToken])

  useEffect(() => { load() }, [load])

  async function handleFiles(files: FileList | null) {
    if (!files || files.length === 0) return
    setUploading(true)
    setError(null)
    try {
      for (const file of Array.from(files)) {
        await uploadReportExample(file, () => getToken())
      }
      await load()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setUploading(false)
    }
  }

  async function handleDelete(id: string) {
    try {
      await deleteReportExample(id, () => getToken())
      setExamples(prev => prev.filter(e => e.id !== id))
    } catch (e: any) {
      setError(e.message)
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
                accept=".txt,.md,.text"
                multiple
                style={{ display: 'none' }}
                onChange={(e) => handleFiles(e.target.files)}
              />
              {uploading ? (
                <div className="honeycomb-spinner" />
              ) : (
                <p>Drop text files here or click to upload</p>
              )}
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
                    className="example-item"
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    layout
                  >
                    <span className="example-name">📄 {ex.name}</span>
                    <button
                      className="example-delete-btn"
                      onClick={() => handleDelete(ex.id)}
                      title="Remove example"
                    >
                      ✕
                    </button>
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
