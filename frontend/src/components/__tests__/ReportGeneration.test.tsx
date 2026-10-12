import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReportBatchStudent } from '../../api'

const mockGenerateReports = vi.fn()
const mockGetReport = vi.fn()
const mockDeleteReport = vi.fn()
const mockRegenerateReport = vi.fn()
const mockSubmitFeedback = vi.fn()
const mockListClasses = vi.fn()
const mockListStudents = vi.fn()
const mockListLevels = vi.fn()
const mockListReportBatches = vi.fn()
const mockRetryReportBatch = vi.fn()

vi.mock('../../api', async (importOriginal) => ({
  ApiError: (await importOriginal<typeof import('../../api')>()).ApiError,
  generateReports: (...args: unknown[]) => mockGenerateReports(...args),
  listReportBatches: (...args: unknown[]) => mockListReportBatches(...args),
  retryReportBatch: (...args: unknown[]) => mockRetryReportBatch(...args),
  getReport: (...args: unknown[]) => mockGetReport(...args),
  deleteReport: (...args: unknown[]) => mockDeleteReport(...args),
  regenerateReport: (...args: unknown[]) => mockRegenerateReport(...args),
  submitFeedback: (...args: unknown[]) => mockSubmitFeedback(...args),
  listClasses: (...args: unknown[]) => mockListClasses(...args),
  listStudents: (...args: unknown[]) => mockListStudents(...args),
  listLevels: (...args: unknown[]) => mockListLevels(...args),
}))

const stableGetToken = vi.fn().mockResolvedValue('tok')
vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: stableGetToken }),
}))

function level(id: number, name: string, reportInstructions: string) {
  return { id, groupId: 'g1', name, reportInstructions, createdAt: '' }
}

function batch(students: ReportBatchStudent[], status = 'finished', id = 'b1') {
  return { id, startDate: '2026-01-01', endDate: '2026-03-31', createdAt: '', status, students }
}

beforeEach(() => {
  vi.clearAllMocks()
  mockListLevels.mockResolvedValue({ levels: [] })
  mockListReportBatches.mockResolvedValue({ batches: [] })
})

afterEach(() => {
  vi.useRealTimers()
})

async function renderWithStudents() {
  mockListClasses.mockResolvedValue({
    classes: [{ id: 1, name: 'Math 101', levelId: 1, levelName: 'Math 101', timeSlot: '', studentCount: 2 }],
  })
  mockListStudents.mockResolvedValue({
    students: [
      { id: 10, name: 'Alice', classId: 1 },
      { id: 11, name: 'Bob', classId: 1 },
    ],
  })
  mockListLevels.mockResolvedValue({ levels: [level(1, 'Math 101', 'Focus on math skills.')] })
  const { default: ReportGeneration } = await import('../ReportGeneration')
  const user = userEvent.setup()
  render(<ReportGeneration />)
  await waitFor(() => screen.getByText('Math 101'))
  return user
}

describe('ReportGeneration', () => {
  it('shows loading then class selection', async () => {
    await renderWithStudents()
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
  })

  it('select all toggles entire class', async () => {
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))
    expect(screen.getByText(/Generate 2 Report/)).toBeInTheDocument()

    await user.click(screen.getByText('Math 101', { selector: 'strong' }))
    expect(screen.getByText(/Generate 0 Report/)).toBeInTheDocument()
  })

  it('generates reports on submit', async () => {
    mockGenerateReports.mockResolvedValue(batch([
      { studentId: 10, name: 'Alice', status: 'done', reportId: 1 },
      { studentId: 11, name: 'Bob', status: 'done', reportId: 2 },
    ]))
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))
    expect(screen.getByText(/Generate 2 Report/)).toBeInTheDocument()

    await user.click(screen.getByText(/Generate 2 Report/))
    await waitFor(() => {
      expect(screen.getByText('Reports for Jan 1, 2026 – Mar 31, 2026')).toBeInTheDocument()
    })
    // Results show student names in result cards
    expect(screen.getAllByText('Alice')).toHaveLength(2) // selector + result
    expect(screen.getAllByText('Bob')).toHaveLength(2)
  })

  it('shows each failed student with its error', async () => {
    mockGenerateReports.mockResolvedValue(batch([
      { studentId: 10, name: 'Alice', status: 'failed', error: 'report generation failed' },
      { studentId: 11, name: 'Bob', status: 'done', reportId: 2 },
    ]))
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))
    await user.click(screen.getByText(/Generate 2 Report/))

    const failed = await screen.findByTestId('report-result-failed')
    expect(failed).toHaveTextContent('Alice')
    expect(failed).toHaveTextContent('report generation failed')
    expect(screen.getByTestId('report-result-name')).toHaveTextContent('Bob')
  })

  it('expanding a done student fetches its report', async () => {
    mockGenerateReports.mockResolvedValue(batch([{ studentId: 10, name: 'Alice', status: 'done', reportId: 7 }]))
    mockGetReport.mockResolvedValue({
      id: 7, studentId: 10, student: 'Alice', className: 'Math 101', html: '<p>Alice report</p>',
      startDate: '2026-01-01', endDate: '2026-03-27', createdAt: '2026-03-27T12:00:00Z',
    })
    const user = await renderWithStudents()
    await user.click(screen.getByText('Alice'))
    await user.click(screen.getByText(/Generate 1 Report/))

    await user.click(await screen.findByTestId('report-result-name'))
    expect(await screen.findByText('Alice report')).toBeInTheDocument()
    expect(mockGetReport).toHaveBeenCalledWith(7, stableGetToken)
  })

  it('a slower earlier fetch does not land on the row expanded after it', async () => {
    mockGenerateReports.mockResolvedValue(batch([
      { studentId: 10, name: 'Alice', status: 'done', reportId: 1 },
      { studentId: 11, name: 'Bob', status: 'done', reportId: 2 },
    ]))
    let resolveAlice: (r: unknown) => void = () => {}
    mockGetReport.mockImplementation((id: number) => id === 1
      ? new Promise(r => { resolveAlice = r })
      : Promise.resolve({ id: 2, studentId: 11, student: 'Bob', className: 'Math 101', html: '<p>Bob report</p>', startDate: '', endDate: '', createdAt: '' }))
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))
    await user.click(screen.getByText(/Generate 2 Report/))

    const [aliceRow, bobRow] = await screen.findAllByTestId('report-result-name')
    await user.click(aliceRow)
    await user.click(bobRow)
    expect(await screen.findByText('Bob report')).toBeInTheDocument()
    resolveAlice({ id: 1, studentId: 10, student: 'Alice', className: 'Math 101', html: '<p>Alice report</p>', startDate: '', endDate: '', createdAt: '' })
    await waitFor(() => expect(screen.getByText('Bob report')).toBeInTheDocument())
    expect(screen.queryByText('Alice report')).not.toBeInTheDocument()
  })

  it('a failed fetch collapses its row and shows the error, unless another row was expanded since', async () => {
    mockGenerateReports.mockResolvedValue(batch([
      { studentId: 10, name: 'Alice', status: 'done', reportId: 1 },
      { studentId: 11, name: 'Bob', status: 'done', reportId: 2 },
    ]))
    let rejectAlice: (e: Error) => void = () => {}
    mockGetReport.mockImplementation((id: number) => id === 1
      ? new Promise((_, reject) => { rejectAlice = reject })
      : Promise.reject(new Error('Bob fetch failed')))
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))
    await user.click(screen.getByText(/Generate 2 Report/))

    const [aliceRow, bobRow] = await screen.findAllByTestId('report-result-name')
    await user.click(aliceRow)
    await user.click(bobRow)
    expect(await screen.findByText(/Bob fetch failed/)).toBeInTheDocument()

    rejectAlice(new Error('Alice fetch failed'))
    await waitFor(() => expect(screen.getByText(/Bob fetch failed/)).toBeInTheDocument())
    expect(screen.queryByText(/Alice fetch failed/)).not.toBeInTheDocument()
    expect(document.querySelector('.report-loading-inline')).not.toBeInTheDocument()
  })

  it('deleting a report from the viewer marks its row deleted', async () => {
    mockGenerateReports.mockResolvedValue(batch([{ studentId: 10, name: 'Alice', status: 'done', reportId: 7 }]))
    mockGetReport.mockResolvedValue({
      id: 7, studentId: 10, student: 'Alice', className: 'Math 101', html: '<p>Alice report</p>',
      startDate: '', endDate: '', createdAt: '',
    })
    mockDeleteReport.mockResolvedValue(undefined)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const user = await renderWithStudents()
    await user.click(screen.getByText('Alice'))
    await user.click(screen.getByText(/Generate 1 Report/))
    await user.click(await screen.findByTestId('report-result-name'))
    await screen.findByText('Alice report')

    await user.click(screen.getByRole('button', { name: /delete/i }))
    const row = await screen.findByTestId('report-result-deleted')
    expect(row).toHaveTextContent('Alice')
    expect(row).toHaveTextContent('deleted')
    expect(screen.queryByTestId('report-result-name')).not.toBeInTheDocument()
  })

  it('after regenerate the row targets the new report', async () => {
    mockGenerateReports.mockResolvedValue(batch([{ studentId: 10, name: 'Alice', status: 'done', reportId: 7 }]))
    mockGetReport.mockResolvedValue({
      id: 7, studentId: 10, student: 'Alice', className: 'Math 101', html: '<p>Alice report</p>',
      startDate: '', endDate: '', createdAt: '',
    })
    mockRegenerateReport.mockResolvedValue({
      id: 8, studentId: 10, student: 'Alice', className: 'Math 101', html: '<p>Alice report v2</p>',
      startDate: '', endDate: '', createdAt: '',
    })
    mockDeleteReport.mockResolvedValue(undefined)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const user = await renderWithStudents()
    await user.click(screen.getByText('Alice'))
    await user.click(screen.getByText(/Generate 1 Report/))
    await user.click(await screen.findByTestId('report-result-name'))
    await screen.findByText('Alice report')
    await user.click(screen.getByTestId('thumb-up'))
    await screen.findByText(/thanks for your feedback/i)

    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    await user.type(screen.getByPlaceholderText(/what should be different/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))
    await screen.findByText('Alice report v2')

    // Thumbs reset for the new report and rate it, not the old one.
    await user.click(screen.getByTestId('thumb-up'))
    await waitFor(() => expect(mockSubmitFeedback).toHaveBeenLastCalledWith(
      { artifact_type: 'report', artifact_id: 8, rating: 'up' },
      stableGetToken,
    ))

    await user.click(screen.getByRole('button', { name: /delete/i }))
    expect(mockDeleteReport).toHaveBeenCalledWith(8, stableGetToken)
    expect(await screen.findByTestId('report-result-deleted')).toHaveTextContent('Alice')
    expect(screen.getByTestId('report-batch-summary')).toHaveTextContent(/deleted/i)
  })

  it('a regenerate finishing after the row collapsed leaves it collapsed', async () => {
    mockGenerateReports.mockResolvedValue(batch([{ studentId: 10, name: 'Alice', status: 'done', reportId: 7 }]))
    mockGetReport.mockResolvedValue({
      id: 7, studentId: 10, student: 'Alice', className: 'Math 101', html: '<p>Alice report</p>',
      startDate: '', endDate: '', createdAt: '',
    })
    let resolveRegen: (r: unknown) => void = () => {}
    mockRegenerateReport.mockReturnValue(new Promise(r => { resolveRegen = r }))
    const user = await renderWithStudents()
    await user.click(screen.getByText('Alice'))
    await user.click(screen.getByText(/Generate 1 Report/))
    await user.click(await screen.findByTestId('report-result-name'))
    await screen.findByText('Alice report')

    await user.click(screen.getByRole('button', { name: 'Regenerate' }))
    await user.type(screen.getByPlaceholderText(/what should be different/i), 'Shorter')
    await user.click(screen.getByRole('button', { name: 'Submit' }))
    await user.click(screen.getByTestId('report-result-name'))
    await waitFor(() => expect(screen.queryByText('Alice report')).not.toBeInTheDocument())

    await act(async () => resolveRegen({
      id: 8, studentId: 10, student: 'Alice', className: 'Math 101', html: '<p>Alice report v2</p>',
      startDate: '', endDate: '', createdAt: '',
    }))
    expect(screen.queryByText('Alice report v2')).not.toBeInTheDocument()

    mockGetReport.mockResolvedValue({
      id: 8, studentId: 10, student: 'Alice', className: 'Math 101', html: '<p>Alice report v2</p>',
      startDate: '', endDate: '', createdAt: '',
    })
    await user.click(screen.getByTestId('report-result-name'))
    expect(await screen.findByText('Alice report v2')).toBeInTheDocument()
    expect(mockGetReport).toHaveBeenLastCalledWith(8, stableGetToken)
  })

  it('shows error on failed generation', async () => {
    mockGenerateReports.mockRejectedValue(new Error('Generation failed'))
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))

    await user.click(screen.getByText(/Generate 2 Report/))
    await waitFor(() => {
      expect(screen.getByText(/Generation failed/)).toBeInTheDocument()
    })
  })
})

describe('ReportGeneration — background batch', () => {
  it('shows the latest batch on mount', async () => {
    mockListReportBatches.mockResolvedValue({ batches: [batch([
      { studentId: 10, name: 'Alice', status: 'done', reportId: 1 },
      { studentId: 11, name: 'Bob', status: 'failed', error: 'report generation failed' },
    ])] })
    await renderWithStudents()
    expect(await screen.findByTestId('report-batch-summary')).toHaveTextContent('2 students · 1 done · 1 failed')
    expect(screen.getByTestId('report-result-failed')).toHaveTextContent('Bob')
  })

  it('polls a running batch until it finishes, with Generate disabled meanwhile', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    mockListReportBatches
      .mockResolvedValueOnce({ batches: [batch([
        { studentId: 10, name: 'Alice', status: 'generating' },
        { studentId: 11, name: 'Bob', status: 'queued' },
      ], 'running')] })
      .mockResolvedValueOnce({ batches: [batch([
        { studentId: 10, name: 'Alice', status: 'done', reportId: 1 },
        { studentId: 11, name: 'Bob', status: 'done', reportId: 2 },
      ])] })
    await renderWithStudents()
    expect(await screen.findByTestId('report-batch-summary')).toHaveTextContent('2 in progress')
    const pending = screen.getAllByTestId('report-result-pending')
    expect(pending[0]).toHaveTextContent('generating')
    expect(pending[1]).toHaveTextContent('queued')
    expect(screen.getByRole('button', { name: /Generating/ })).toBeDisabled()

    await act(() => vi.advanceTimersByTimeAsync(3000))
    expect(await screen.findByTestId('report-batch-summary')).toHaveTextContent('2 done')
    expect(screen.getByRole('button', { name: /Generate 0 Reports/ })).toBeInTheDocument()

    await act(() => vi.advanceTimersByTimeAsync(6000))
    expect(mockListReportBatches).toHaveBeenCalledTimes(2)
  })

  it('a running batch the server lost shows as interrupted', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    mockListReportBatches
      .mockResolvedValueOnce({ batches: [batch([{ studentId: 10, name: 'Alice', status: 'queued' }], 'running')] })
      .mockResolvedValueOnce({ batches: [] })
    await renderWithStudents()
    await screen.findByTestId('report-batch-summary')

    await act(() => vi.advanceTimersByTimeAsync(3000))
    expect(await screen.findByTestId('report-batch-interrupted')).toHaveTextContent('The batch was interrupted. Check History.')
    expect(screen.queryByTestId('report-batch-summary')).not.toBeInTheDocument()
  })

  it('a conflict shows the batch already running instead of an error', async () => {
    const { ApiError } = await vi.importActual<typeof import('../../api')>('../../api')
    mockGenerateReports.mockRejectedValue(new ApiError('a report batch is already running', 409))
    const user = await renderWithStudents()
    mockListReportBatches.mockResolvedValue({ batches: [batch([{ studentId: 11, name: 'Bob', status: 'queued' }], 'running', 'other')] })
    await user.click(screen.getByText('Math 101'))
    await user.click(screen.getByText(/Generate 2 Report/))

    expect(await screen.findByTestId('report-result-pending')).toHaveTextContent('Bob')
    expect(screen.getByRole('button', { name: /Generating/ })).toBeDisabled()
    expect(screen.queryByText(/already running/)).not.toBeInTheDocument()
  })

  it('a conflict with no running batch yet shows the error', async () => {
    const { ApiError } = await vi.importActual<typeof import('../../api')>('../../api')
    mockGenerateReports.mockRejectedValue(new ApiError('a report batch is already running', 409))
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))
    await user.click(screen.getByText(/Generate 2 Report/))

    expect(await screen.findByText(/a report batch is already running/)).toBeInTheDocument()
  })

  it('a 404 on expand marks the row deleted', async () => {
    const { ApiError } = await vi.importActual<typeof import('../../api')>('../../api')
    mockListReportBatches.mockResolvedValue({ batches: [batch([{ studentId: 10, name: 'Alice', status: 'done', reportId: 7 }])] })
    mockGetReport.mockRejectedValue(new ApiError('report not found', 404))
    const user = await renderWithStudents()
    await user.click(await screen.findByTestId('report-result-name'))

    expect(await screen.findByTestId('report-result-deleted')).toHaveTextContent('Alice')
    expect(screen.queryByText(/report not found/)).not.toBeInTheDocument()
    expect(document.querySelector('.report-loading-inline')).not.toBeInTheDocument()
    expect(screen.getByTestId('report-batch-summary')).toHaveTextContent('1 student · 1 deleted')
  })

  it('a running batch replaced from another tab shows the newer batch', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    mockListReportBatches
      .mockResolvedValueOnce({ batches: [batch([{ studentId: 10, name: 'Alice', status: 'queued' }], 'running', 'old')] })
      .mockResolvedValueOnce({ batches: [batch([{ studentId: 11, name: 'Bob', status: 'queued' }], 'running', 'new')] })
    await renderWithStudents()
    await screen.findByTestId('report-result-pending')

    await act(() => vi.advanceTimersByTimeAsync(3000))
    await waitFor(() => expect(screen.getByTestId('report-result-pending')).toHaveTextContent('Bob'))
    expect(screen.queryByTestId('report-batch-interrupted')).not.toBeInTheDocument()
  })
})

describe('ReportGeneration — retry failed', () => {
  const doneAlice = { studentId: 10, name: 'Alice', status: 'done', reportId: 1 }
  const failedBob = { studentId: 11, name: 'Bob', status: 'failed', error: 'report generation failed' }

  it.each([
    ['a running batch', batch([{ ...failedBob }, { studentId: 10, name: 'Alice', status: 'queued' }], 'running')],
    ['a finished batch with no failures', batch([doneAlice])],
  ])('hides Retry failed on %s', async (_, shown) => {
    mockListReportBatches.mockResolvedValue({ batches: [shown] })
    await renderWithStudents()
    await screen.findByTestId('report-batch-summary')
    expect(screen.queryByTestId('report-batch-retry')).not.toBeInTheDocument()
  })

  it('requeues the failed rows and polls until they finish', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    mockListReportBatches
      .mockResolvedValueOnce({ batches: [batch([doneAlice, failedBob])] })
      .mockResolvedValueOnce({ batches: [batch([doneAlice, { studentId: 11, name: 'Bob', status: 'done', reportId: 2 }])] })
    mockRetryReportBatch.mockResolvedValue(batch([doneAlice, { studentId: 11, name: 'Bob', status: 'queued' }], 'running'))
    const user = await renderWithStudents()

    await user.click(await screen.findByTestId('report-batch-retry'))
    expect(mockRetryReportBatch).toHaveBeenCalledWith('b1', stableGetToken)
    expect(await screen.findByTestId('report-result-pending')).toHaveTextContent('Bob')
    expect(screen.queryByTestId('report-batch-retry')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Generating/ })).toBeDisabled()

    await act(() => vi.advanceTimersByTimeAsync(3000))
    expect(await screen.findByTestId('report-batch-summary')).toHaveTextContent('2 done')
    expect(mockListReportBatches).toHaveBeenCalledTimes(2)
  })

  it('a conflict shows the batch where another tab left it', async () => {
    const { ApiError } = await vi.importActual<typeof import('../../api')>('../../api')
    mockListReportBatches
      .mockResolvedValueOnce({ batches: [batch([doneAlice, failedBob])] })
      .mockResolvedValue({ batches: [batch([doneAlice, { studentId: 11, name: 'Bob', status: 'generating' }], 'running')] })
    mockRetryReportBatch.mockRejectedValue(new ApiError('a report batch is already running', 409))
    const user = await renderWithStudents()

    await user.click(await screen.findByTestId('report-batch-retry'))
    expect(await screen.findByTestId('report-result-pending')).toHaveTextContent('generating')
    expect(screen.queryByText(/already running/)).not.toBeInTheDocument()
  })

  it('a conflict that leaves the batch unchanged shows the error', async () => {
    const { ApiError } = await vi.importActual<typeof import('../../api')>('../../api')
    mockListReportBatches.mockResolvedValue({ batches: [batch([doneAlice, failedBob])] })
    mockRetryReportBatch.mockRejectedValue(new ApiError('a report batch is already running', 409))
    const user = await renderWithStudents()

    await user.click(await screen.findByTestId('report-batch-retry'))
    expect(await screen.findByText(/already running/)).toBeInTheDocument()
  })

  it('shows the error when retry fails', async () => {
    mockListReportBatches.mockResolvedValue({ batches: [batch([doneAlice, failedBob])] })
    mockRetryReportBatch.mockRejectedValue(new Error('report generation is busy, try again shortly'))
    const user = await renderWithStudents()

    await user.click(await screen.findByTestId('report-batch-retry'))
    expect(await screen.findByText(/report generation is busy/)).toBeInTheDocument()
    expect(screen.getByTestId('report-batch-retry')).toBeEnabled()
  })
})

describe('ReportGeneration — Level instructions gate', () => {
  async function renderWithClass(
    levelName: string,
    reportInstructions: string,
    studentName = 'Alice'
  ) {
    mockListClasses.mockResolvedValue({
      classes: [{ id: 1, name: levelName, studentCount: 1, userId: '', levelId: 1, levelName, timeSlot: '', position: 0, createdAt: '' }],
    })
    mockListStudents.mockResolvedValue({
      students: [{ id: 10, name: studentName, classId: 1, createdAt: '', aliases: [] }],
    })
    mockListLevels.mockResolvedValue({ levels: [level(1, levelName, reportInstructions)] })
    const { default: ReportGeneration } = await import('../ReportGeneration')
    const user = userEvent.setup()
    render(<ReportGeneration />)
    await waitFor(() => screen.getByText(levelName || studentName))
    return user
  }

  it('blocks generation when the selected Level has no report instructions', async () => {
    const user = await renderWithClass('3B', '')
    await user.click(screen.getByText('Alice'))
    await waitFor(() => {
      expect(screen.getByTestId('level-instructions-blocker')).toHaveTextContent('3B')
      expect(screen.getByTestId('level-instructions-blocker')).toHaveTextContent(
        'An admin must add report instructions'
      )
    })
    const generateBtn = screen.getByRole('button', { name: /Generate/ })
    expect(generateBtn).toBeDisabled()
  })

  it('treats whitespace-only instructions as empty', async () => {
    const user = await renderWithClass('3B', '   \n\t  ')
    await user.click(screen.getByText('Alice'))
    await waitFor(() => {
      expect(screen.getByTestId('level-instructions-blocker')).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: /Generate/ })).toBeDisabled()
  })

  it('renders a read-only instructions block and enables generation when the Level has instructions', async () => {
    const user = await renderWithClass('ClassA', 'Keep it warm and encouraging.')
    await user.click(screen.getByText('Alice'))
    await waitFor(() => {
      expect(screen.queryByTestId('level-instructions-blocker')).not.toBeInTheDocument()
      expect(screen.getByTestId('level-instructions-block')).toHaveTextContent('ClassA')
      expect(screen.getByTestId('level-instructions-block')).toHaveTextContent('Keep it warm and encouraging.')
    })
    const generateBtn = screen.getByRole('button', { name: /Generate/ })
    expect(generateBtn).not.toBeDisabled()
    // Read-only: no textarea/input to edit the instructions text within the block.
    const block = screen.getByTestId('level-instructions-block')
    expect(block.querySelector('textarea, input')).toBeNull()
  })

  it('treats a Class levelId absent from the loaded Levels list as unresolved', async () => {
    mockListClasses.mockResolvedValue({
      classes: [{ id: 1, name: '3B', levelId: 99, levelName: '3B', studentCount: 1, userId: '', scheduleName: '', position: 0, createdAt: '' }],
    })
    mockListStudents.mockResolvedValue({
      students: [{ id: 10, name: 'Alice', classId: 1, createdAt: '', aliases: [] }],
    })
    mockListLevels.mockResolvedValue({ levels: [level(1, 'Other', 'Some instructions.')] })
    const { default: ReportGeneration } = await import('../ReportGeneration')
    const user = userEvent.setup()
    render(<ReportGeneration />)
    await waitFor(() => screen.getByText('3B'))

    await user.click(screen.getByText('Alice'))

    expect(screen.queryByTestId('level-instructions-block')).not.toBeInTheDocument()
    expect(screen.queryByTestId('level-instructions-blocker')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Generate/ })).toBeDisabled()
  })

  it('renders exactly one block for several students in the same Level', async () => {
    mockListClasses.mockResolvedValue({
      classes: [{ id: 1, name: 'Math 101', levelId: 1, levelName: 'Math 101', scheduleName: '', studentCount: 2, userId: '', position: 0, createdAt: '' }],
    })
    mockListStudents.mockResolvedValue({
      students: [
        { id: 10, name: 'Alice', classId: 1, createdAt: '', aliases: [] },
        { id: 11, name: 'Bob', classId: 1, createdAt: '', aliases: [] },
      ],
    })
    mockListLevels.mockResolvedValue({ levels: [level(1, 'Math 101', 'Some instructions.')] })
    const { default: ReportGeneration } = await import('../ReportGeneration')
    const user = userEvent.setup()
    render(<ReportGeneration />)
    await waitFor(() => screen.getByText('Math 101'))

    await user.click(screen.getByText('Alice'))
    await user.click(screen.getByText('Bob'))

    await waitFor(() => {
      expect(screen.getAllByTestId('level-instructions-block')).toHaveLength(1)
    })
  })

  it('renders one block per distinct Level across multiple selected Levels', async () => {
    mockListClasses.mockResolvedValue({
      classes: [
        { id: 1, name: 'ClassA', levelId: 1, levelName: 'ClassA', studentCount: 1, userId: '', timeSlot: '', position: 0, createdAt: '' },
        { id: 2, name: 'ClassB', levelId: 2, levelName: 'ClassB', studentCount: 1, userId: '', timeSlot: '', position: 0, createdAt: '' },
      ],
    })
    mockListStudents.mockImplementation((_classId: unknown) => {
      const classId = _classId as number
      return Promise.resolve({
        students: classId === 1
          ? [{ id: 10, name: 'Alice', classId: 1, createdAt: '', aliases: [] }]
          : [{ id: 11, name: 'Bob', classId: 2, createdAt: '', aliases: [] }],
      })
    })
    mockListLevels.mockResolvedValue({
      levels: [level(1, 'ClassA', 'Instructions A.'), level(2, 'ClassB', '')],
    })
    const { default: ReportGeneration } = await import('../ReportGeneration')
    const user = userEvent.setup()
    render(<ReportGeneration />)
    await waitFor(() => screen.getByText('ClassA'))
    // Select students from both classes
    await user.click(screen.getByText('Alice'))
    await user.click(screen.getByText('Bob'))
    await waitFor(() => {
      expect(screen.getByTestId('level-instructions-block')).toHaveTextContent('ClassA')
      expect(screen.getByTestId('level-instructions-blocker')).toHaveTextContent('ClassB')
    })
    expect(screen.getByRole('button', { name: /Generate/ })).toBeDisabled()
  })
})

describe('ReportGeneration — Levels load failure', () => {
  it('surfaces a listLevels() failure and disables Generate', async () => {
    mockListClasses.mockResolvedValue({
      classes: [{ id: 1, name: 'Math 101', levelId: 1, levelName: 'Math 101', timeSlot: '', studentCount: 2 }],
    })
    mockListStudents.mockResolvedValue({
      students: [
        { id: 10, name: 'Alice', classId: 1 },
        { id: 11, name: 'Bob', classId: 1 },
      ],
    })
    mockListLevels.mockRejectedValue(new Error('network down'))
    const { default: ReportGeneration } = await import('../ReportGeneration')
    const user = userEvent.setup()
    render(<ReportGeneration />)
    await waitFor(() => screen.getByText('Math 101'))

    await user.click(screen.getByText('Math 101', { selector: 'strong' }))
    await waitFor(() => {
      expect(screen.getByTestId('levels-load-error')).toHaveTextContent('network down')
    })
    expect(screen.getByRole('button', { name: /Generate/ })).toBeDisabled()
  })
})
