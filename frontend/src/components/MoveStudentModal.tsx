import { useState, useEffect, useMemo } from 'react'
import { useAuth } from '@clerk/react'
import { motion } from 'motion/react'
import { listClasses, moveStudent, MoveConflictError, type ClassItem } from '../api'
import InlineError from './InlineError'
import { HexBullet, ChevronIcon } from './Icons'

export interface MoveStudentModalProps {
  studentId: number
  studentName: string
  currentClassId: number
  currentLevelId: number
  onClose: () => void
  onMoved: (result: {
    classId: number
    className: string
    levelId: number
    levelName: string
    droppedAliases: string[]
  }) => void
}

type Step = 'pick' | 'confirm' | 'result'

/**
 * Move-to-class picker: Level-grouped target classes, a confirm step with a
 * Report-Instructions warning for cross-Level moves, and a dismissible
 * result step listing any aliases dropped for colliding with the target
 * class. Dismiss on × / Escape only — matches the Enter text modal
 * (DESIGN.md "Modals") so a stray tap can't abandon a half-configured move.
 */
export default function MoveStudentModal({
  studentId,
  studentName,
  currentClassId,
  currentLevelId,
  onClose,
  onMoved,
}: MoveStudentModalProps) {
  const { getToken } = useAuth()
  const [classes, setClasses] = useState<ClassItem[]>([])
  const [loadStatus, setLoadStatus] = useState<'loading' | 'error' | 'ready'>('loading')
  const [step, setStep] = useState<Step>('pick')
  const [target, setTarget] = useState<ClassItem | null>(null)
  const [moving, setMoving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [conflictName, setConflictName] = useState<string | null>(null)
  const [droppedAliases, setDroppedAliases] = useState<string[]>([])

  useEffect(() => {
    listClasses(getToken)
      .then(({ classes: cls }) => {
        setClasses(cls || [])
        setLoadStatus('ready')
      })
      .catch(() => setLoadStatus('error'))
  }, [getToken])

  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onClose])

  const levelGroups = useMemo(() => {
    const map = new Map<number, { levelName: string; classes: ClassItem[] }>()
    for (const c of classes) {
      if (c.id === currentClassId) continue
      if (!map.has(c.levelId)) map.set(c.levelId, { levelName: c.levelName, classes: [] })
      map.get(c.levelId)!.classes.push(c)
    }
    return [...map.entries()]
      .map(([levelId, group]) => ({ levelId, ...group }))
      .sort((a, b) => a.levelName.localeCompare(b.levelName))
  }, [classes, currentClassId])

  function classRowLabel(c: ClassItem) {
    const prefix = `${c.levelName} · `
    return c.name.startsWith(prefix) ? c.name.slice(prefix.length) : c.name
  }

  function handlePick(c: ClassItem) {
    setError(null)
    setConflictName(null)
    if (c.levelId === currentLevelId) {
      doMove(c)
    } else {
      setTarget(c)
      setStep('confirm')
    }
  }

  async function doMove(c: ClassItem) {
    setMoving(true)
    setError(null)
    try {
      const { droppedAliases: dropped } = await moveStudent(studentId, c.id, getToken)
      setDroppedAliases(dropped)
      setTarget(c)
      setStep('result')
      onMoved({ classId: c.id, className: c.name, levelId: c.levelId, levelName: c.levelName, droppedAliases: dropped })
    } catch (err) {
      if (err instanceof MoveConflictError) {
        setError(err.message)
        setConflictName(err.conflictStudentName || null)
      } else {
        setError(err instanceof Error ? err.message : 'Failed to move student')
        setConflictName(null)
      }
      setStep('pick')
    } finally {
      setMoving(false)
    }
  }

  return (
    <motion.div
      className="how-it-works-overlay"
      data-testid="move-student-overlay"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
    >
      <motion.div
        className="how-it-works-card card move-student-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="move-student-modal-heading"
        initial={{ opacity: 0, y: 30, scale: 0.97 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 20 }}
        transition={{ duration: 0.3, ease: 'easeOut' }}
        onClick={e => e.stopPropagation()}
      >
        <button
          type="button"
          className="how-it-works-close move-student-modal-close"
          onClick={onClose}
          aria-label="Close"
        >
          ×
        </button>

        {step === 'result' && target ? (
          <div className="move-student-result" data-testid="move-student-result">
            <h2 id="move-student-modal-heading">Moved</h2>
            <p>
              <strong>{studentName}</strong> is now in <strong>{target.name}</strong>.
            </p>
            {droppedAliases.length > 0 && (
              <InlineError severity="info">
                Dropped alias{droppedAliases.length > 1 ? 'es' : ''} already used in the target class:{' '}
                <strong>{droppedAliases.join(', ')}</strong>. Re-add them from Aliases if needed.
              </InlineError>
            )}
            <button type="button" onClick={onClose} data-testid="move-student-done-btn">
              Done
            </button>
          </div>
        ) : step === 'confirm' && target ? (
          <div className="move-student-confirm" data-testid="move-student-confirm">
            <h2 id="move-student-modal-heading">Move to a different Level?</h2>
            <p>
              Moving <strong>{studentName}</strong> to <strong>{target.name}</strong> changes their Level. Future
              Reports will follow {target.levelName}&rsquo;s Report Instructions. Existing Reports are unaffected.
            </p>
            {error && <InlineError onDismiss={() => setError(null)}>{error}</InlineError>}
            <div className="move-student-confirm-actions">
              <button
                type="button"
                className="btn-secondary"
                onClick={() => { setStep('pick'); setTarget(null) }}
                disabled={moving}
              >
                Cancel
              </button>
              <button type="button" onClick={() => doMove(target)} disabled={moving} data-testid="move-student-confirm-btn">
                {moving ? 'Moving…' : 'Confirm move'}
              </button>
            </div>
          </div>
        ) : (
          <div className="move-student-pick" data-testid="move-student-pick">
            <h2 id="move-student-modal-heading">Move {studentName}</h2>
            {error && (
              conflictName ? (
                <InlineError title={conflictName} onDismiss={() => { setError(null); setConflictName(null) }}>
                  already has a student with this name in the target class. Rename one of them first.
                </InlineError>
              ) : (
                <InlineError onDismiss={() => setError(null)}>{error}</InlineError>
              )
            )}
            {loadStatus === 'loading' && (
              <div className="honeycomb-spinner">
                <div className="hex" /><div className="hex" /><div className="hex" />
              </div>
            )}
            {loadStatus === 'error' && <InlineError>Failed to load classes.</InlineError>}
            {loadStatus === 'ready' && levelGroups.length === 0 && <p>No other classes to move to.</p>}
            {loadStatus === 'ready' &&
              levelGroups.map(group => (
                <div className="move-student-level-group" key={group.levelId}>
                  <h3 className="move-student-level-heading">{group.levelName}</h3>
                  <ul className="move-student-class-list">
                    {group.classes.map(c => (
                      <li key={c.id}>
                        <button
                          type="button"
                          className="move-student-class-row"
                          onClick={() => handlePick(c)}
                          disabled={moving}
                          data-testid={`move-student-target-${c.id}`}
                        >
                          <HexBullet />
                          <span className="move-student-class-row-label">{classRowLabel(c)}</span>
                          <span className="move-student-class-row-chevron"><ChevronIcon open={false} /></span>
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
          </div>
        )}
      </motion.div>
    </motion.div>
  )
}
