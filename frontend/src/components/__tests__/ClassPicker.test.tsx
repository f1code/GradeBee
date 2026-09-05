import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockListClasses = vi.fn()

vi.mock('../../api', () => ({
  listClasses: (...args: unknown[]) => mockListClasses(...args),
}))

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

// Two classes on the same level and day: the collision that makes the model
// decline in the first place, and the pair a teacher can pick the wrong one of.
const siblings = {
  classes: [
    { id: 1, name: 'Pam & Paul · Wed · 14.10', studentCount: 6 },
    { id: 2, name: 'Pam & Paul · Wed · 16.30', studentCount: 6 },
  ],
}

describe('ClassPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListClasses.mockResolvedValue(siblings)
  })

  it('lists every class the teacher owns', async () => {
    const { default: ClassPicker } = await import('../ClassPicker')
    render(<ClassPicker onPick={vi.fn().mockResolvedValue(undefined)} />)

    await waitFor(() => {
      expect(screen.getAllByTestId('class-picker-option')).toHaveLength(2)
    })
    expect(screen.getByText('Pam & Paul · Wed · 14.10')).toBeInTheDocument()
    expect(screen.getByText('Pam & Paul · Wed · 16.30')).toBeInTheDocument()
  })

  // The picker outlives a successful pick that resolved nobody — that is what
  // lets a wrong sibling class be corrected. A busy flag cleared only on the
  // error path would leave every option dead exactly then, so the flag is
  // asserted here rather than through the card, whose own re-render would mask
  // it.
  it('re-enables its options after a pick resolves', async () => {
    const user = userEvent.setup()
    const onPick = vi.fn().mockResolvedValue(undefined)
    const { default: ClassPicker } = await import('../ClassPicker')
    render(<ClassPicker onPick={onPick} />)

    await waitFor(() => {
      expect(screen.getAllByTestId('class-picker-option')).toHaveLength(2)
    })
    await user.click(screen.getByText('Pam & Paul · Wed · 14.10'))

    await waitFor(() => {
      expect(onPick).toHaveBeenCalledWith('Pam & Paul · Wed · 14.10')
    })
    await waitFor(() => {
      screen.getAllByTestId('class-picker-option').forEach(o => expect(o).toBeEnabled())
    })
    expect(screen.queryByText('Creating notes…')).not.toBeInTheDocument()

    // ...and a second pick actually goes through.
    await user.click(screen.getByText('Pam & Paul · Wed · 16.30'))
    await waitFor(() => {
      expect(onPick).toHaveBeenCalledTimes(2)
    })
  })

  it('shows why a pick failed and leaves the options usable', async () => {
    const user = userEvent.setup()
    const onPick = vi.fn().mockRejectedValue(new Error('this recording already has notes'))
    const { default: ClassPicker } = await import('../ClassPicker')
    render(<ClassPicker onPick={onPick} />)

    await waitFor(() => {
      expect(screen.getAllByTestId('class-picker-option')).toHaveLength(2)
    })
    await user.click(screen.getByText('Pam & Paul · Wed · 14.10'))

    await waitFor(() => {
      expect(screen.getByTestId('class-picker-error')).toHaveTextContent('this recording already has notes')
    })
    screen.getAllByTestId('class-picker-option').forEach(o => expect(o).toBeEnabled())
  })

  it('says so when the class list cannot be read', async () => {
    mockListClasses.mockRejectedValue(new Error('Failed to list classes'))
    const { default: ClassPicker } = await import('../ClassPicker')
    render(<ClassPicker onPick={vi.fn()} />)

    await waitFor(() => {
      expect(screen.getByTestId('class-picker-error')).toHaveTextContent('Failed to list classes')
    })
    expect(screen.queryAllByTestId('class-picker-option')).toHaveLength(0)
  })

  it('says so when the teacher has no classes', async () => {
    mockListClasses.mockResolvedValue({ classes: [] })
    const { default: ClassPicker } = await import('../ClassPicker')
    render(<ClassPicker onPick={vi.fn()} />)

    await waitFor(() => {
      expect(screen.getByText('You have no classes yet.')).toBeInTheDocument()
    })
    expect(screen.queryAllByTestId('class-picker-option')).toHaveLength(0)
  })
})
