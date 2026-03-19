import { useAuth } from '@clerk/react'
import { useEffect, useState } from 'react'
import { motion } from 'motion/react'

interface Student {
  name: string
}

interface ClassGroup {
  name: string
  students: Student[]
}

interface StudentsResponse {
  spreadsheetUrl: string
  classes: ClassGroup[]
}

type StudentListStatus = 'loading' | 'empty' | 'error' | 'success'

interface StudentListProps {
  onSetupRequired?: () => void
}

function HexBullet() {
  return (
    <svg className="hex-bullet" width="14" height="14" viewBox="0 0 14 14" fill="none">
      <path d="M7 1L12.66 4.25V10.75L7 14L1.34 10.75V4.25L7 1Z" fill="#E8A317" opacity="0.7" />
    </svg>
  )
}

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

export default function StudentList({ onSetupRequired }: StudentListProps) {
  const { getToken } = useAuth()
  const [status, setStatus] = useState<StudentListStatus>('loading')
  const [data, setData] = useState<StudentsResponse | null>(null)
  const [error, setError] = useState<{ code?: string; message?: string; spreadsheetUrl?: string } | null>(null)

  const apiUrl = import.meta.env.VITE_API_URL

  async function fetchStudents() {
    setStatus('loading')
    setError(null)
    try {
      const token = await getToken()
      const resp = await fetch(`${apiUrl}/students`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const body = await resp.json().catch(() => ({}))

      if (!resp.ok) {
        if (body.error === 'no_spreadsheet') {
          setError({ code: 'no_spreadsheet', message: body.message || 'ClassSetup spreadsheet not found. Try running setup again.' })
          setStatus('error')
        } else if (body.error === 'empty_spreadsheet') {
          setError({
            code: 'empty_spreadsheet',
            message: body.message || 'No students found. Add your students to the ClassSetup spreadsheet.',
            spreadsheetUrl: body.spreadsheetUrl,
          })
          setStatus('empty')
        } else {
          setError({ message: body.error || body.message || resp.statusText })
          setStatus('error')
        }
        return
      }

      setData(body as StudentsResponse)
      setStatus('success')
    } catch (err) {
      setError({ message: err instanceof Error ? err.message : 'Failed to load students' })
      setStatus('error')
    }
  }

  useEffect(() => {
    fetchStudents()
  }, [])

  if (status === 'loading') {
    return (
      <div className="student-list" data-testid="student-list-loading">
        <p className="loading-text">Loading students...</p>
      </div>
    )
  }

  if (status === 'error' && error?.code === 'no_spreadsheet') {
    return (
      <motion.div
        className="student-list student-list-error"
        data-testid="student-list-no-spreadsheet"
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.35 }}
      >
        <h2>Setup Required</h2>
        <p>{error.message}</p>
        <button onClick={onSetupRequired} data-testid="run-setup-again-btn">
          Run setup again
        </button>
      </motion.div>
    )
  }

  if (status === 'empty' || (status === 'error' && error?.code === 'empty_spreadsheet')) {
    const spreadsheetUrl = data?.spreadsheetUrl ?? error?.spreadsheetUrl
    return (
      <motion.div
        className="student-list info-box"
        data-testid="student-list-empty"
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.35 }}
      >
        <h2>No Students Found</h2>
        <p>{error?.message || 'No students found. Add your students to the ClassSetup spreadsheet.'}</p>
        {spreadsheetUrl && (
          <a href={spreadsheetUrl} target="_blank" rel="noopener noreferrer" data-testid="spreadsheet-link">
            Open ClassSetup spreadsheet
          </a>
        )}
        <button onClick={fetchStudents} data-testid="student-list-refresh">
          Refresh
        </button>
      </motion.div>
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
        <p>{error?.message}</p>
        <button onClick={fetchStudents} data-testid="student-list-refresh">
          Retry
        </button>
      </motion.div>
    )
  }

  if (!data) {
    return null
  }

  return (
    <div className="student-list" data-testid="student-list">
      <div className="toolbar">
        <a
          href={data.spreadsheetUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="toolbar-link"
          data-testid="edit-students-link"
        >
          ✏️ Edit students in Sheets
        </a>
        <button className="btn-secondary" onClick={fetchStudents} data-testid="student-list-refresh">
          Refresh
        </button>
      </div>

      <motion.div variants={containerVariants} initial="hidden" animate="visible">
        {data.classes.map((cls) => (
          <motion.div
            key={cls.name}
            className="class-group"
            data-testid={`class-group-${cls.name}`}
            variants={cardVariants}
          >
            <h3>
              <HexBullet />
              {cls.name}
              <span className="count" data-testid={`class-count-${cls.name}`}>
                ({cls.students.length})
              </span>
            </h3>
            <ul>
              {cls.students.map((s) => (
                <li key={s.name} data-testid={`student-${cls.name}-${s.name}`}>
                  {s.name}
                </li>
              ))}
            </ul>
          </motion.div>
        ))}
      </motion.div>
    </div>
  )
}
