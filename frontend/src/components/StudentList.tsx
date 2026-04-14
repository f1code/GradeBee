import { useAuth } from '@clerk/react'
import { useEffect, useState, useCallback, useRef } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { useMediaQuery } from '../hooks/useMediaQuery'
import {
  listClasses,
  listStudents,
  renameClass,
  deleteClass,
  renameStudent,
  deleteStudent,
  type ClassItem,
  type StudentItem,
} from '../api'
import AddClassForm from './AddClassForm'
import AddStudentForm from './AddStudentForm'
import StudentDetail from './StudentDetail'

import { HexBullet, ChevronIcon, PencilIcon, TrashIcon } from './Icons'
import ItemRow from './ItemRow'

const containerVariants = {
  hidden: {},
  visible: {
    transition: { staggerChildren: 0.08 },
  },
}

const cardVariants = {
  hidden: { opacity: 0, y: 16 },
  visible: { opacity: 1, y: 0, transition: { duration: 0.35, ease: 'easeOut' as const } },
}

type Status = 'loading' | 'error' | 'ready'

export default function StudentList() {
  const { getToken } = useAuth()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const [status, setStatus] = useState<Status>('loading')
  const [classes, setClasses] = useState<ClassItem[]>([])
  const [expandedStudents, setExpandedStudents] = useState<Map<number, StudentItem[]>>(new Map())
  const [expandedClassIds, setExpandedClassIds] = useState<Set<number>>(new Set())
  const [loadingClassIds, setLoadingClassIds] = useState<Set<number>>(new Set())
  const [error, setError] = useState<string | null>(null)
  const [showAddClass, setShowAddClass] = useState(false)
  const [editingClassId, setEditingClassId] = useState<number | null>(null)
  const [editingStudentId, setEditingStudentId] = useState<number | null>(null)
  const [deletingId, setDeletingId] = useState<{ type: 'class' | 'student'; id: number; name: string } | null>(null)
  const [failedClassIds, setFailedClassIds] = useState<Set<number>>(new Set())
  const [flashError, setFlashError] = useState<string | null>(null)
  const flashTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const [collapsed, setCollapsed] = useState(isMobile)
  const [expandedStudentId, setExpandedStudentId] = useState<number | null>(null)

  useEffect(() => {
    setCollapsed(isMobile)
  }, [isMobile])

  function showFlash(msg: string) {
    setFlashError(msg)
    clearTimeout(flashTimer.current)
    flashTimer.current = setTimeout(() => setFlashError(null), 3000)
  }

  const fetchClasses = useCallback(async () => {
    setStatus('loading')
    setError(null)
    try {
      const { classes: cls } = await listClasses(getToken)
      setClasses(cls || [])
      setStatus('ready')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load classes')
      setStatus('error')
    }
  }, [getToken])

  useEffect(() => {
    fetchClasses()
  }, [fetchClasses])

  async function toggleExpand(classId: number) {
    const next = new Set(expandedClassIds)
    if (next.has(classId)) {
      next.delete(classId)
      setExpandedClassIds(next)
      return
    }
    next.add(classId)
    setExpandedClassIds(next)

    // Fetch students if not cached
    if (!expandedStudents.has(classId)) {
      setLoadingClassIds(prev => new Set(prev).add(classId))
      try {
        const { students } = await listStudents(classId, getToken)
        setExpandedStudents(prev => new Map(prev).set(classId, students || []))
      } catch {
        setFailedClassIds(prev => new Set(prev).add(classId))
      } finally {
        setLoadingClassIds(prev => {
          const s = new Set(prev)
          s.delete(classId)
          return s
        })
      }
    }
  }

  function handleClassCreated(cls: ClassItem) {
    setClasses(prev => [...prev, cls].sort((a, b) => a.name.localeCompare(b.name)))
    setShowAddClass(false)
    // Auto-expand the new class and initialize empty student list
    setExpandedClassIds(prev => new Set(prev).add(cls.id))
    setExpandedStudents(prev => new Map(prev).set(cls.id, []))
  }

  function handleStudentCreated(classId: number, student: StudentItem) {
    setExpandedStudents(prev => {
      const m = new Map(prev)
      const existing = m.get(classId) || []
      m.set(classId, [...existing, student].sort((a, b) => a.name.localeCompare(b.name)))
      return m
    })
    // Update count
    setClasses(prev => prev.map(c => c.id === classId ? { ...c, studentCount: c.studentCount + 1 } : c))
  }

  async function handleRenameClass(classId: number, newName: string) {
    const old = classes.find(c => c.id === classId)
    if (!old || newName === old.name) {
      setEditingClassId(null)
      return
    }
    // Optimistic update
    setClasses(prev => prev.map(c => c.id === classId ? { ...c, name: newName, className: newName } : c).sort((a, b) => a.name.localeCompare(b.name)))
    setEditingClassId(null)
    try {
      await renameClass(classId, newName, old.groupName, getToken)
    } catch {
      // Revert
      setClasses(prev => prev.map(c => c.id === classId ? { ...c, name: old.name, className: old.className } : c).sort((a, b) => a.name.localeCompare(b.name)))
      showFlash('Failed to rename class')
    }
  }

  async function handleDeleteClass(classId: number) {
    setDeletingId(null)
    try {
      await deleteClass(classId, getToken)
      setClasses(prev => prev.filter(c => c.id !== classId))
      setExpandedStudents(prev => {
        const m = new Map(prev)
        m.delete(classId)
        return m
      })
      setExpandedClassIds(prev => {
        const s = new Set(prev)
        s.delete(classId)
        return s
      })
    } catch {
      showFlash('Failed to delete class')
    }
  }

  async function handleRenameStudent(studentId: number, classId: number, newName: string) {
    const students = expandedStudents.get(classId) || []
    const old = students.find(s => s.id === studentId)
    if (!old || newName === old.name) {
      setEditingStudentId(null)
      return
    }
    // Optimistic
    setExpandedStudents(prev => {
      const m = new Map(prev)
      m.set(classId, (m.get(classId) || []).map(s => s.id === studentId ? { ...s, name: newName } : s).sort((a, b) => a.name.localeCompare(b.name)))
      return m
    })
    setEditingStudentId(null)
    try {
      await renameStudent(studentId, newName, getToken)
    } catch {
      // Revert
      setExpandedStudents(prev => {
        const m = new Map(prev)
        m.set(classId, (m.get(classId) || []).map(s => s.id === studentId ? { ...s, name: old.name } : s).sort((a, b) => a.name.localeCompare(b.name)))
        return m
      })
      showFlash('Failed to rename student')
    }
  }

  async function handleDeleteStudent(studentId: number, classId: number) {
    setDeletingId(null)
    try {
      await deleteStudent(studentId, getToken)
      setExpandedStudents(prev => {
        const m = new Map(prev)
        m.set(classId, (m.get(classId) || []).filter(s => s.id !== studentId))
        return m
      })
      setClasses(prev => prev.map(c => c.id === classId ? { ...c, studentCount: Math.max(0, c.studentCount - 1) } : c))
    } catch {
      showFlash('Failed to delete student')
    }
  }

  function retryLoadStudents(classId: number) {
    setFailedClassIds(prev => {
      const s = new Set(prev)
      s.delete(classId)
      return s
    })
    // Clear cached entry so toggleExpand re-fetches
    setExpandedStudents(prev => {
      const m = new Map(prev)
      m.delete(classId)
      return m
    })
    // Collapse and re-expand to trigger fetch
    setExpandedClassIds(prev => {
      const s = new Set(prev)
      s.delete(classId)
      return s
    })
    toggleExpand(classId)
  }

  if (status === 'loading') {
    return (
      <div className="student-list" data-testid="student-list-loading">
        <div className="honeycomb-spinner">
          <div className="hex" /><div className="hex" /><div className="hex" />
        </div>
      </div>
    )
  }

  if (status === 'error') {
    return (
      <motion.div
        className="student-list student-list-error"
        data-testid="student-list-error"
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.35 }}
      >
        <h2>Error</h2>
        <p>{error}</p>
        <button onClick={fetchClasses} data-testid="student-list-refresh">Retry</button>
      </motion.div>
    )
  }

  const totalStudents = classes.reduce((sum, cls) => sum + cls.studentCount, 0)

  // Empty state
  if (classes.length === 0 && !showAddClass) {
    return (
      <motion.div
        className="student-list info-box"
        data-testid="student-list-empty"
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.35 }}
      >
        <h2>No Classes Yet</h2>
        <p>Add your first class to get started.</p>
        <AddClassForm onCreated={cls => {
          setClasses([cls])
          setExpandedClassIds(new Set([cls.id]))
          setExpandedStudents(new Map([[cls.id, []]]))
        }} />
      </motion.div>
    )
  }

  return (
    <div className="student-list" data-testid="student-list">
      {/* Header row */}
      <div className="student-list-header">
        <h2 className="student-list-heading">Your Classes</h2>
        <button
          className="btn-sm"
          onClick={() => setShowAddClass(true)}
          disabled={showAddClass}
          data-testid="add-class-btn"
        >
          + Add Class
        </button>
      </div>

      {/* Add class form */}
      <AnimatePresence>
        {showAddClass && (
          <AddClassForm
            onCreated={handleClassCreated}
            onCancel={() => setShowAddClass(false)}
          />
        )}
      </AnimatePresence>

      {/* Mobile collapse toggle */}
      {isMobile && (
        <button
          className="student-list-collapse-toggle"
          onClick={() => setCollapsed(!collapsed)}
          data-testid="student-list-toggle"
        >
          <span>{classes.length} {classes.length === 1 ? 'class' : 'classes'} · {totalStudents} students</span>
          <ChevronIcon open={!collapsed} />
        </button>
      )}

      {/* Class list */}
      <AnimatePresence initial={false}>
        {(!isMobile || !collapsed) && (
          <motion.div
            key="class-list"
            variants={containerVariants}
            initial={isMobile ? { opacity: 0, height: 0 } : 'hidden'}
            animate={isMobile ? { opacity: 1, height: 'auto' } : 'visible'}
            exit={isMobile ? { opacity: 0, height: 0 } : undefined}
            transition={{ duration: 0.3, ease: 'easeInOut' }}
            style={{ overflow: 'hidden' }}
          >
            {classes.map(cls => {
              const isExpanded = expandedClassIds.has(cls.id)
              const isLoading = loadingClassIds.has(cls.id)
              const isFailed = failedClassIds.has(cls.id)
              const students = expandedStudents.get(cls.id) || []
              const isDeleting = deletingId?.type === 'class' && deletingId.id === cls.id

              return (
                <motion.div
                  key={cls.id}
                  className="class-group"
                  data-testid={`class-group-${cls.id}`}
                  variants={cardVariants}
                >
                  {/* Delete confirmation */}
                  <AnimatePresence>
                    {isDeleting && (
                      <motion.div
                        className="delete-confirm"
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: 'auto' }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.2 }}
                      >
                        <span>Delete <strong>{cls.name}</strong> and all its students?</span>
                        <div className="delete-confirm-actions">
                          <button className="btn-secondary btn-sm" onClick={() => setDeletingId(null)}>Cancel</button>
                          <button className="btn-danger btn-sm" onClick={() => handleDeleteClass(cls.id)} data-testid={`confirm-delete-class-${cls.id}`}>Delete</button>
                        </div>
                      </motion.div>
                    )}
                  </AnimatePresence>

                  {/* Class header */}
                  {!isDeleting && (
                    <div className="class-group-header" onClick={() => toggleExpand(cls.id)} data-testid={`class-toggle-${cls.id}`}>
                      <h3>
                        <HexBullet />
                        {editingClassId === cls.id ? (
                          <InlineEdit
                            value={cls.name}
                            onSave={newName => handleRenameClass(cls.id, newName)}
                            onCancel={() => setEditingClassId(null)}
                          />
                        ) : (
                          <span className="class-name-text">{cls.name}</span>
                        )}
                        <span className="count">({cls.studentCount})</span>
                      </h3>
                      <div className="class-actions" onClick={e => e.stopPropagation()}>
                        <button
                          className="icon-btn"
                          onClick={() => setEditingClassId(cls.id)}
                          aria-label={`Rename ${cls.name}`}
                          data-testid={`rename-class-${cls.id}`}
                        >
                          <PencilIcon />
                        </button>
                        <button
                          className="icon-btn icon-btn-danger"
                          onClick={() => setDeletingId({ type: 'class', id: cls.id, name: cls.name })}
                          aria-label={`Delete ${cls.name}`}
                          data-testid={`delete-class-${cls.id}`}
                        >
                          <TrashIcon />
                        </button>
                        <button
                          className="icon-btn"
                          onClick={() => toggleExpand(cls.id)}
                          aria-label={isExpanded ? 'Collapse' : 'Expand'}
                        >
                          <ChevronIcon open={isExpanded} />
                        </button>
                      </div>
                    </div>
                  )}

                  {/* Expanded students */}
                  <AnimatePresence>
                    {isExpanded && !isDeleting && (
                      <motion.div
                        initial={{ opacity: 0, height: 0 }}
                        animate={{ opacity: 1, height: 'auto' }}
                        exit={{ opacity: 0, height: 0 }}
                        transition={{ duration: 0.25 }}
                        style={{ overflow: 'hidden' }}
                      >
                        {isLoading ? (
                          <div className="class-students-loading">
                            <div className="honeycomb-spinner">
                              <div className="hex" /><div className="hex" /><div className="hex" />
                            </div>
                          </div>
                        ) : isFailed ? (
                          <div className="class-students-error" data-testid={`class-error-${cls.id}`}>
                            <span>Failed to load students.</span>
                            <button className="btn-sm btn-secondary" onClick={() => retryLoadStudents(cls.id)}>Retry</button>
                          </div>
                        ) : (
                          <>
                            <ul>
                              <AnimatePresence>
                                {students.map(s => (
                                    <motion.li
                                      key={s.id}
                                      data-testid={`student-${s.id}`}
                                      initial={{ opacity: 0 }}
                                      animate={{ opacity: 1 }}
                                      exit={{ opacity: 0, height: 0, padding: 0, margin: 0 }}
                                      transition={{ duration: 0.2 }}
                                    >
                                      {editingStudentId === s.id ? (
                                        <InlineEdit
                                          value={s.name}
                                          onSave={newName => handleRenameStudent(s.id, cls.id, newName)}
                                          onCancel={() => setEditingStudentId(null)}
                                        />
                                      ) : (
                                        <ItemRow
                                          name={s.name}
                                          expanded={expandedStudentId === s.id}
                                          onToggle={() => setExpandedStudentId(expandedStudentId === s.id ? null : s.id)}
                                          onDelete={() => handleDeleteStudent(s.id, cls.id)}
                                          actions={
                                            <button
                                              className="icon-btn"
                                              onClick={e => { e.stopPropagation(); setEditingStudentId(s.id) }}
                                              aria-label={`Rename ${s.name}`}
                                              data-testid={`rename-student-${s.id}`}
                                            >
                                              <PencilIcon />
                                            </button>
                                          }
                                        >
                                          <StudentDetail
                                            studentId={s.id}
                                            studentName={s.name}
                                            className={cls.name}
                                            onCollapse={() => setExpandedStudentId(null)}
                                          />
                                        </ItemRow>
                                      )}
                                    </motion.li>
                                  ))}
                              </AnimatePresence>
                            </ul>
                            <AddStudentForm
                              classId={cls.id}
                              onCreated={student => handleStudentCreated(cls.id, student)}
                            />
                          </>
                        )}
                      </motion.div>
                    )}
                  </AnimatePresence>
                </motion.div>
              )
            })}
          </motion.div>
        )}
      </AnimatePresence>
      {/* Flash error */}
      <AnimatePresence>
        {flashError && (
          <motion.div
            className="flash-error"
            data-testid="flash-error"
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 8 }}
            transition={{ duration: 0.2 }}
          >
            {flashError}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

function InlineEdit({
  value,
  onSave,
  onCancel,
}: {
  value: string
  onSave: (newValue: string) => void
  onCancel: () => void
}) {
  const [text, setText] = useState(value)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
    inputRef.current?.select()
  }, [])

  function handleBlur() {
    const trimmed = text.trim()
    if (trimmed && trimmed !== value) {
      onSave(trimmed)
    } else {
      onCancel()
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter') {
      const trimmed = text.trim()
      if (trimmed) onSave(trimmed)
      else onCancel()
    } else if (e.key === 'Escape') {
      onCancel()
    }
  }

  return (
    <input
      ref={inputRef}
      type="text"
      value={text}
      onChange={e => setText(e.target.value)}
      onBlur={handleBlur}
      onKeyDown={handleKeyDown}
      className="inline-edit-input"
      data-testid="inline-edit-input"
    />
  )
}
