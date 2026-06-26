import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockGenerateReports = vi.fn()
const mockListClasses = vi.fn()
const mockListStudents = vi.fn()
const mockListReportExamples = vi.fn()
const mockUploadReportExample = vi.fn()
const mockUpdateReportExample = vi.fn()
const mockDeleteReportExample = vi.fn()
const mockImportExampleFromDrive = vi.fn()
const mockGetGoogleToken = vi.fn()
const mockListClassNames = vi.fn()
const mockOpenPicker = vi.fn()

vi.mock('../../api', () => ({
  generateReports: (...args: unknown[]) => mockGenerateReports(...args),
  listClasses: (...args: unknown[]) => mockListClasses(...args),
  listStudents: (...args: unknown[]) => mockListStudents(...args),
  listReportExamples: (...args: unknown[]) => mockListReportExamples(...args),
  uploadReportExample: (...args: unknown[]) => mockUploadReportExample(...args),
  updateReportExample: (...args: unknown[]) => mockUpdateReportExample(...args),
  deleteReportExample: (...args: unknown[]) => mockDeleteReportExample(...args),
  importExampleFromDrive: (...args: unknown[]) => mockImportExampleFromDrive(...args),
  getGoogleToken: (...args: unknown[]) => mockGetGoogleToken(...args),
  listClassNames: (...args: unknown[]) => mockListClassNames(...args),
}))

vi.mock('../../hooks/useDrivePicker', () => ({
  useDrivePicker: () => ({ openPicker: mockOpenPicker }),
}))

const stableGetToken = vi.fn().mockResolvedValue('tok')
vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: stableGetToken }),
}))

beforeEach(() => {
  vi.clearAllMocks()
  mockListReportExamples.mockResolvedValue({ examples: [] })
  mockListClassNames.mockResolvedValue({ classNames: [] })
  mockUploadReportExample.mockResolvedValue({})
})

async function renderWithStudents() {
  mockListClasses.mockResolvedValue({
    classes: [{ id: 1, name: 'Math 101', studentCount: 2 }],
  })
  mockListStudents.mockResolvedValue({
    students: [
      { id: 10, name: 'Alice', classId: 1 },
      { id: 11, name: 'Bob', classId: 1 },
    ],
  })
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

    await user.click(screen.getByText('Math 101'))
    expect(screen.getByText(/Generate 0 Report/)).toBeInTheDocument()
  })

  it('generates reports on submit', async () => {
    mockGenerateReports.mockResolvedValue({
      reports: [
        { id: 1, student: 'Alice', class: 'Math 101', studentId: 10, html: '<p>Alice report</p>', startDate: '2026-01-01', endDate: '2026-03-27', createdAt: '2026-03-27T12:00:00Z' },
        { id: 2, student: 'Bob', class: 'Math 101', studentId: 11, html: '<p>Bob report</p>', startDate: '2026-01-01', endDate: '2026-03-27', createdAt: '2026-03-27T12:00:00Z' },
      ],
      error: null,
    })
    mockListReportExamples.mockResolvedValue({
      examples: [{ id: 1, name: 'Example.pdf', content: 'ex', status: 'ready', classNames: ['Math 101'] }],
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
    mockListReportExamples.mockResolvedValue({
      examples: [{ id: 1, name: 'Example.pdf', content: 'ex', status: 'ready', classNames: ['Math 101'] }],
    })
    const user = await renderWithStudents()
    await user.click(screen.getByText('Math 101'))

    await user.click(screen.getByText(/Generate 2 Report/))
    await waitFor(() => {
      expect(screen.getByText(/Generation failed/)).toBeInTheDocument()
    })
  })

  it('fetches and renders example report cards', async () => {
    mockListReportExamples.mockResolvedValue({
      examples: [
        { id: 1, name: 'Report.jpg', content: 'Student showed great improvement in math.', status: 'ready', classNames: ['Math'] },
      ],
    })
    const user = await renderWithStudents()

    await user.click(screen.getByText(/Example Report Cards/))
    await waitFor(() => {
      expect(screen.getByText('Report.jpg')).toBeInTheDocument()
    })
    expect(mockListReportExamples).toHaveBeenCalled()
  })

  it('uploads example files with selected class names', async () => {
    mockListClassNames.mockResolvedValue({ classNames: ['Math'] })
    const user = await renderWithStudents()

    await user.click(screen.getByText(/Example Report Cards/))

    // The hidden file input lives inside the drop zone.
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['example'], 'example.txt', { type: 'text/plain' })
    await user.upload(fileInput, file)

    // Class selection panel appears; choose the Math class.
    await waitFor(() => expect(screen.getByText('Math')).toBeInTheDocument())
    await user.click(screen.getByRole('checkbox', { name: 'Math' }))

    await user.click(screen.getByText('Upload'))

    await waitFor(() => {
      expect(mockUploadReportExample).toHaveBeenCalled()
    })
    const [uploadedFile, classNames] = mockUploadReportExample.mock.calls[0]
    expect(uploadedFile).toBeInstanceOf(File)
    expect(classNames).toEqual(['Math'])
  })
})

describe('ReportGeneration — selection-aware blocking', () => {
  async function renderWithClass(className: string, studentName = 'Alice') {
    mockListClasses.mockResolvedValue({
      classes: [{ id: 1, name: className, studentCount: 1, userId: '', className: '', groupName: '', position: 0, createdAt: '' }],
    })
    mockListStudents.mockResolvedValue({
      students: [{ id: 10, name: studentName, classId: 1, createdAt: '', aliases: [] }],
    })
    const { default: ReportGeneration } = await import('../ReportGeneration')
    const user = userEvent.setup()
    render(<ReportGeneration />)
    await waitFor(() => screen.getByText(className || studentName))
    return user
  }

  it('blocks generation when selected student class has no matching examples', async () => {
    mockListReportExamples.mockResolvedValue({ examples: [] })
    const user = await renderWithClass('3B')
    // Select Alice
    await user.click(screen.getByLabelText('Alice') ?? screen.getByText('Alice'))
    await waitFor(() => {
      expect(screen.getByTestId('generate-blocker')).toHaveTextContent('3B (no examples)')
    })
    const generateBtn = screen.getByRole('button', { name: /Generate/ })
    expect(generateBtn).toBeDisabled()
  })

  it('shows correct message format: class name + reason', async () => {
    mockListReportExamples.mockResolvedValue({ examples: [] })
    const user = await renderWithClass('Class 3B')
    await user.click(screen.getByText('Alice'))
    await waitFor(() => {
      expect(screen.getByTestId('generate-blocker')).toHaveTextContent(
        'Class 3B (no examples) — assign a class / add examples to continue.'
      )
    })
  })

  it('enables generation when selected student class has a matching ready example', async () => {
    mockListReportExamples.mockResolvedValue({
      examples: [
        { id: 1, name: 'Report.pdf', content: 'Example.', status: 'ready', classNames: ['ClassA'] },
      ],
    })
    const user = await renderWithClass('ClassA')
    await user.click(screen.getByText('Alice'))
    await waitFor(() => {
      expect(screen.queryByTestId('generate-blocker')).not.toBeInTheDocument()
    })
    const generateBtn = screen.getByRole('button', { name: /Generate/ })
    expect(generateBtn).not.toBeDisabled()
  })

  it('multiple classes: lists each class without examples separately', async () => {
    mockListClasses.mockResolvedValue({
      classes: [
        { id: 1, name: 'ClassA', studentCount: 1, userId: '', className: '', groupName: '', position: 0, createdAt: '' },
        { id: 2, name: 'ClassB', studentCount: 1, userId: '', className: '', groupName: '', position: 0, createdAt: '' },
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
    mockListReportExamples.mockResolvedValue({
      examples: [
        { id: 1, name: 'R.pdf', content: 'e', status: 'ready', classNames: ['ClassA'] },
      ],
    })
    const { default: ReportGeneration } = await import('../ReportGeneration')
    const user = userEvent.setup()
    render(<ReportGeneration />)
    await waitFor(() => screen.getByText('ClassA'))
    // Select students from both classes
    await user.click(screen.getByText('Alice'))
    await user.click(screen.getByText('Bob'))
    await waitFor(() => {
      const blocker = screen.getByTestId('generate-blocker')
      expect(blocker).toHaveTextContent('ClassB (no examples)')
      expect(blocker).not.toHaveTextContent('ClassA (no examples)')
    })
  })
})
