import { useAuth } from '@clerk/react'
import { useEffect, useState, useCallback, useRef } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { useMediaQuery } from '../hooks/useMediaQuery'
import {
  listClasses,
  listStudents,
  listLevels,
  renameClass,
  deleteClass,
  renameStudent,
  deleteStudent,
  WEEKDAYS,
  type ClassItem,
  type StudentItem,
  type LevelItem,
} from '../api'
import AddClassForm from './AddClassForm'
import AddStudentForm from './AddStudentForm'
import StudentDetail from './StudentDetail'
import MoveStudentModal from './MoveStudentModal'
import InlineError from './InlineError'
import InlineEdit from './InlineEdit'

import { HexBullet, ChevronIcon, PencilIcon, TrashIcon } from './Icons'
import ItemRow from './ItemRow'
import CollapsePresence from './CollapsePresence'

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
  const [levels, setLevels] = useState<LevelItem[]>([])
  const [movingStudent, setMovingStudent] = useState<{ studentId: number; studentName: string; classId: number; levelId: number } | null>(null)

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setCollapsed(isMobile)
  }, [isMobile])

  useEffect(() => {
    listLevels(getToken).then(({ levels: lvls }) => setLevels(lvls || [])).catch(() => {})
  }, [getToken])

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
    // eslint-disable-next-line react-hooks/set-state-in-effect
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

  async function handleRenameClass(classId: number, newLevelId: number, newDay: string, newTimeSlot: string) {
    const old = classes.find(c => c.id === classId)
    if (!old || (newLevelId === old.levelId && newDay === old.day && newTimeSlot === old.timeSlot)) {
      setEditingClassId(null)
      return
    }
    const newLevelName = levels.find(l => l.id === newLevelId)?.name ?? old.levelName
    const displayName = `${newLevelName} · ${newDay.slice(0, 3)}` + (newTimeSlot ? ` · ${newTimeSlot}` : '')
    // Optimistic update
    setClasses(prev => prev.map(c => c.id === classId ? { ...c, name: displayName, levelId: newLevelId, levelName: newLevelName, day: newDay, timeSlot: newTimeSlot } : c).sort((a, b) => a.name.localeCompare(b.name)))
    setEditingClassId(null)
    try {
      await renameClass(classId, newLevelId, newDay, newTimeSlot, getToken)
    } catch {
      // Revert
      setClasses(prev => prev.map(c => c.id === classId ? { ...c, name: old.name, levelId: old.levelId, levelName: old.levelName, day: old.day, timeSlot: old.timeSlot } : c).sort((a, b) => a.name.localeCompare(b.name)))
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

  async function refetchExpandedClass(classId: number) {
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

  function handleStudentMoved(result: { classId: number; className: string; levelId: number; levelName: string; droppedAliases: string[] }) {
    const moved = movingStudent
    if (!moved) return
    const { studentId, classId: sourceClassId } = moved
    setExpandedStudents(prev => {
      const m = new Map(prev)
      m.set(sourceClassId, (m.get(sourceClassId) || []).filter(s => s.id !== studentId))
      // Drop the target's cached list; if it's already expanded, nothing else
      // will refetch it (toggleExpand only fetches on the expand transition),
      // so refetch it directly. Otherwise it refetches on next expand.
      m.delete(result.classId)
      return m
    })
    setClasses(prev => prev.map(c => {
      if (c.id === sourceClassId) return { ...c, studentCount: Math.max(0, c.studentCount - 1) }
      if (c.id === result.classId) return { ...c, studentCount: c.studentCount + 1 }
      return c
    }))
    if (expandedClassIds.has(result.classId)) {
      refetchExpandedClass(result.classId)
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

      <AnimatePresence mode="wait" initial={false}>
        {showAddClass ? (
          <CollapsePresence key="add-class">
            <AddClassForm
              onCreated={handleClassCreated}
              onCancel={() => setShowAddClass(false)}
            />
          </CollapsePresence>
        ) : classes.length === 0 ? (
          <CollapsePresence key="empty">
            <div className="info-box" data-testid="student-list-empty">
              <h2>No Classes Yet</h2>
              <p>Add your first class to get started.</p>
            </div>
          </CollapsePresence>
        ) : null}
      </AnimatePresence>

      {/* Mobile collapse toggle */}
      {isMobile && classes.length > 0 && (
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
                  className="level"
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
                        style={{ overflow: 'hidden' }}
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
                    <div className="level-header" onClick={() => toggleExpand(cls.id)} data-testid={`class-toggle-${cls.id}`}>
                      <h3>
                        <HexBullet />
                        {editingClassId === cls.id ? (
                          <InlineClassEdit
                            levelId={cls.levelId}
                            day={cls.day}
                            timeSlot={cls.timeSlot}
                            levels={levels}
                            onSave={(newLevelId, newDay, newTimeSlot) => handleRenameClass(cls.id, newLevelId, newDay, newTimeSlot)}
                            onCancel={() => setEditingClassId(null)}
                          />
                        ) : (
                          <span className="level-name-text">
                            {cls.levelName}
                            <span className="time-slot-text"> · {cls.day.slice(0, 3)}</span>
                            {cls.timeSlot && <span className="time-slot-text"> · {cls.timeSlot}</span>}
                          </span>
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
                          <div data-testid={`class-error-${cls.id}`}>
                            <InlineError>
                              Failed to load students.{' '}
                              <button className="btn-sm btn-secondary" onClick={() => retryLoadStudents(cls.id)}>Retry</button>
                            </InlineError>
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
                                      style={{ overflow: 'hidden' }}
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
                                           subtitle={s.aliases && s.aliases.length > 0 ? (
                                             <><span className="aka-label">AKA</span><span className="aka-names">{s.aliases.join(', ')}</span></>
                                           ) : undefined}
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
                                            onRequestMove={() => setMovingStudent({ studentId: s.id, studentName: s.name, classId: cls.id, levelId: cls.levelId })}
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
      <AnimatePresence>
        {movingStudent && (
          <MoveStudentModal
            studentId={movingStudent.studentId}
            studentName={movingStudent.studentName}
            currentClassId={movingStudent.classId}
            currentLevelId={movingStudent.levelId}
            onClose={() => setMovingStudent(null)}
            onMoved={handleStudentMoved}
          />
        )}
      </AnimatePresence>
    </div>
  )
}


function InlineClassEdit({
  levelId,
  day,
  timeSlot,
  levels,
  onSave,
  onCancel,
}: {
  levelId: number
  day: string
  timeSlot: string
  levels: LevelItem[]
  onSave: (levelId: number, day: string, timeSlot: string) => void
  onCancel: () => void
}) {
  const [selectedLevelId, setSelectedLevelId] = useState(levelId)
  const [selectedDay, setSelectedDay] = useState(day)
  const [selectedTimeSlot, setSelectedTimeSlot] = useState(timeSlot)
  const selectRef = useRef<HTMLSelectElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    selectRef.current?.focus()
  }, [])

  function doSave() {
    onSave(selectedLevelId, selectedDay, selectedTimeSlot.trim())
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter') {
      doSave()
    } else if (e.key === 'Escape') {
      onCancel()
    }
  }

  function handleBlur(e: React.FocusEvent) {
    // Only save/cancel if focus leaves both inputs
    if (!containerRef.current?.contains(e.relatedTarget as Node)) {
      doSave()
    }
  }

  return (
    <div ref={containerRef} className="inline-class-edit" onClick={e => e.stopPropagation()}>
      <select
        ref={selectRef}
        value={selectedLevelId}
        onChange={e => setSelectedLevelId(Number(e.target.value))}
        onKeyDown={handleKeyDown}
        onBlur={handleBlur}
        className="inline-edit-input"
        data-testid="inline-edit-class-level"
      >
        {levels.map(l => (
          <option key={l.id} value={l.id}>{l.name}</option>
        ))}
      </select>
      <select
        value={selectedDay}
        onChange={e => setSelectedDay(e.target.value)}
        onKeyDown={handleKeyDown}
        onBlur={handleBlur}
        className="inline-edit-input"
        data-testid="inline-edit-class-day"
      >
        {WEEKDAYS.map(d => (
          <option key={d} value={d}>{d}</option>
        ))}
      </select>
      <input
        type="text"
        value={selectedTimeSlot}
        onChange={e => setSelectedTimeSlot(e.target.value)}
        onKeyDown={handleKeyDown}
        onBlur={handleBlur}
        className="inline-edit-input inline-edit-time-slot"
        data-testid="inline-edit-time-slot"
        placeholder="Time slot (optional)"
      />
    </div>
  )
}
