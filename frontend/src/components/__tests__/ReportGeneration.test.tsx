import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockGenerateReports = vi.fn()
const mockListClasses = vi.fn()
const mockListStudents = vi.fn()
const mockListLevels = vi.fn()

vi.mock('../../api', () => ({
  generateReports: (...args: unknown[]) => mockGenerateReports(...args),
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

beforeEach(() => {
  vi.clearAllMocks()
  mockListLevels.mockResolvedValue({ levels: [] })
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
    mockGenerateReports.mockResolvedValue({
      reports: [
        { id: 1, student: 'Alice', className: 'Math 101', studentId: 10, html: '<p>Alice report</p>', startDate: '2026-01-01', endDate: '2026-03-27', createdAt: '2026-03-27T12:00:00Z' },
        { id: 2, student: 'Bob', className: 'Math 101', studentId: 11, html: '<p>Bob report</p>', startDate: '2026-01-01', endDate: '2026-03-27', createdAt: '2026-03-27T12:00:00Z' },
      ],
      error: null,
    })
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))
    expect(screen.getByText(/Generate 2 Report/)).toBeInTheDocument()

    await user.click(screen.getByText(/Generate 2 Report/))
    await waitFor(() => {
      expect(screen.getByText('Generated Reports')).toBeInTheDocument()
    })
    // Results show student names in result cards
    expect(screen.getAllByText('Alice')).toHaveLength(2) // selector + result
    expect(screen.getAllByText('Bob')).toHaveLength(2)
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
