import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import ReportExamples from '../ReportExamples'
import type { ReportExampleItem } from '../../api'

const baseExamples: ReportExampleItem[] = [
  {
    id: 1,
    name: 'Report.jpg',
    content: 'Student showed great improvement in math.',
    status: 'ready',
    classNames: ['Math'],
  },
]

const secondExample: ReportExampleItem = {
  id: 2,
  name: 'Science.jpg',
  content: 'Science report content.',
  status: 'ready',
  classNames: ['Science'],
}

function renderExamples(overrides: Partial<React.ComponentProps<typeof ReportExamples>> = {}) {
  const props = {
    examples: baseExamples,
    loading: false,
    error: null,
    availableClassNames: ['Math'],
    onUpload: vi.fn().mockResolvedValue(undefined),
    onDriveImport: vi.fn().mockResolvedValue(undefined),
    onUpdate: vi.fn().mockResolvedValue(undefined),
    onDelete: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
  render(<ReportExamples {...props} />)
  return props
}

describe('ReportExamples (selection-aware)', () => {
  it('highlights matching examples and dims non-matching', async () => {
    const user = userEvent.setup()
    renderExamples({
      examples: [baseExamples[0], secondExample],
      availableClassNames: ['Math', 'Science'],
      selectedClassNames: ['Math'],
    })
    await user.click(screen.getByText(/Example Report Cards/))
    await waitFor(() => {
      expect(screen.getByText('Report.jpg')).toBeInTheDocument()
      expect(screen.getByText('Science.jpg')).toBeInTheDocument()
    })
    const mathWrapper = screen.getByText('Report.jpg').closest('.example-item-wrapper')
    const sciWrapper = screen.getByText('Science.jpg').closest('.example-item-wrapper')
    expect(mathWrapper).toHaveClass('example-item-wrapper--matching')
    expect(sciWrapper).toHaveClass('example-item-wrapper--dimmed')
  })

  it('shows summary count of matching ready examples', async () => {
    const user = userEvent.setup()
    renderExamples({
      examples: [baseExamples[0], secondExample],
      availableClassNames: ['Math', 'Science'],
      selectedClassNames: ['Math'],
    })
    await user.click(screen.getByText(/Example Report Cards/))
    await waitFor(() => {
      expect(screen.getByTestId('example-selection-summary')).toHaveTextContent('1 example will guide these reports.')
    })
  })

  it('shows plural summary when multiple matching', async () => {
    const user = userEvent.setup()
    const mathExample2: ReportExampleItem = { id: 3, name: 'Math2.pdf', content: 'More math.', status: 'ready', classNames: ['Math'] }
    renderExamples({
      examples: [baseExamples[0], mathExample2, secondExample],
      availableClassNames: ['Math', 'Science'],
      selectedClassNames: ['Math'],
    })
    await user.click(screen.getByText(/Example Report Cards/))
    await waitFor(() => {
      expect(screen.getByTestId('example-selection-summary')).toHaveTextContent('2 examples will guide these reports.')
    })
  })

  it('does not show summary when no classes are selected', async () => {
    const user = userEvent.setup()
    renderExamples({ selectedClassNames: [] })
    await user.click(screen.getByText(/Example Report Cards/))
    await waitFor(() => {
      expect(screen.getByText('Report.jpg')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('example-selection-summary')).not.toBeInTheDocument()
  })

  it('does not dim when selectedClassNames is not provided', async () => {
    const user = userEvent.setup()
    renderExamples()
    await user.click(screen.getByText(/Example Report Cards/))
    await waitFor(() => {
      expect(screen.getByText('Report.jpg')).toBeInTheDocument()
    })
    const wrapper = screen.getByText('Report.jpg').closest('.example-item-wrapper')
    expect(wrapper).not.toHaveClass('example-item-wrapper--dimmed')
    expect(wrapper).not.toHaveClass('example-item-wrapper--matching')
  })

  it('multiple selected classes: example matching any is highlighted', async () => {
    const user = userEvent.setup()
    const multiExample: ReportExampleItem = { id: 4, name: 'Multi.pdf', content: 'Both.', status: 'ready', classNames: ['Math', 'Science'] }
    renderExamples({
      examples: [multiExample],
      availableClassNames: ['Math', 'Science'],
      selectedClassNames: ['Science'],
    })
    await user.click(screen.getByText(/Example Report Cards/))
    await waitFor(() => {
      expect(screen.getByText('Multi.pdf')).toBeInTheDocument()
    })
    const wrapper = screen.getByText('Multi.pdf').closest('.example-item-wrapper')
    expect(wrapper).toHaveClass('example-item-wrapper--matching')
  })
})


describe('ReportExamples (presentational)', () => {
  it('renders toggle button', () => {
    renderExamples()
    expect(screen.getByText(/Example Report Cards/)).toBeInTheDocument()
  })

  it('shows extracted text when example is clicked', async () => {
    const user = userEvent.setup()
    renderExamples()

    // Expand the examples section
    await user.click(screen.getByText(/Example Report Cards/))

    // Wait for the example to appear
    await waitFor(() => {
      expect(screen.getByText('Report.jpg')).toBeInTheDocument()
    })

    // Content should not be visible yet
    expect(screen.queryByText(/great improvement/)).not.toBeInTheDocument()

    // Click the example name to expand it
    await user.click(screen.getByText('Report.jpg'))

    // Content should now be visible
    await waitFor(() => {
      expect(screen.getByText(/great improvement/)).toBeInTheDocument()
    })

    // Click again to collapse
    await user.click(screen.getByText('Report.jpg'))
    await waitFor(() => {
      expect(screen.queryByText(/great improvement/)).not.toBeInTheDocument()
    })
  })

  it('has a trash icon button that calls onDelete', async () => {
    const user = userEvent.setup()
    const props = renderExamples()

    await user.click(screen.getByText(/Example Report Cards/))

    await waitFor(() => {
      expect(screen.getByLabelText('Delete Report.jpg')).toBeInTheDocument()
    })

    await user.click(screen.getByLabelText('Delete Report.jpg'))
    // ItemRow shows an inline confirm step before invoking onDelete.
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    expect(props.onDelete).toHaveBeenCalledWith(1)
  })

  it('enters edit mode and calls onUpdate on save', async () => {
    const user = userEvent.setup()
    const props = renderExamples()

    // Expand section
    await user.click(screen.getByText(/Example Report Cards/))
    await waitFor(() => expect(screen.getByText('Report.jpg')).toBeInTheDocument())

    // Click edit button
    await user.click(screen.getByLabelText('Edit Report.jpg'))

    // Should show edit form with name input
    await waitFor(() => {
      expect(screen.getByDisplayValue('Report.jpg')).toBeInTheDocument()
    })

    // Save without changes
    const saveBtn = screen.getByText('Save')
    expect(saveBtn).not.toBeDisabled()
    await user.click(saveBtn)

    await waitFor(() => {
      expect(props.onUpdate).toHaveBeenCalledWith(
        1,
        'Report.jpg',
        'Student showed great improvement in math.',
        ['Math'],
      )
    })
  })

  it('shows error from the error prop when expanded', async () => {
    const user = userEvent.setup()
    renderExamples({ error: 'Upload failed' })
    await user.click(screen.getByText(/Example Report Cards/))
    expect(await screen.findByText('Upload failed')).toBeInTheDocument()
  })
})
