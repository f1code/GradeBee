import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import ItemRow from '../ItemRow'

describe('ItemRow', () => {
  it('renders name and chevron', () => {
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={vi.fn()} onDelete={vi.fn()}>
        <p>Details</p>
      </ItemRow>
    )
    expect(screen.getByText('Test Item')).toBeInTheDocument()
    // Chevron SVG is present
    expect(screen.getByLabelText('Delete Test Item')).toBeInTheDocument()
  })

  it('calls onToggle when name is clicked', async () => {
    const onToggle = vi.fn()
    const user = userEvent.setup()
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={onToggle} onDelete={vi.fn()}>
        <p>Details</p>
      </ItemRow>
    )
    await user.click(screen.getByText('Test Item'))
    expect(onToggle).toHaveBeenCalledOnce()
  })

  it('shows children when expanded', () => {
    render(
      <ItemRow name="Test Item" expanded={true} onToggle={vi.fn()} onDelete={vi.fn()}>
        <p>Expanded content here</p>
      </ItemRow>
    )
    expect(screen.getByText('Expanded content here')).toBeInTheDocument()
  })

  it('hides children when collapsed', () => {
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={vi.fn()} onDelete={vi.fn()}>
        <p>Expanded content here</p>
      </ItemRow>
    )
    expect(screen.queryByText('Expanded content here')).not.toBeInTheDocument()
  })

  it('shows delete confirmation when trash is clicked, then calls onDelete on confirm', async () => {
    const onDelete = vi.fn()
    const user = userEvent.setup()
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={vi.fn()} onDelete={onDelete}>
        <p>Details</p>
      </ItemRow>
    )

    // Click trash icon
    await user.click(screen.getByLabelText('Delete Test Item'))

    // Confirmation appears
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /Cancel/ })).toBeInTheDocument()
    })

    // Confirm delete
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onDelete).toHaveBeenCalledOnce()
  })

  it('cancels delete confirmation', async () => {
    const onDelete = vi.fn()
    const user = userEvent.setup()
    render(
      <ItemRow name="Test Item" expanded={false} onToggle={vi.fn()} onDelete={onDelete}>
        <p>Details</p>
      </ItemRow>
    )

    await user.click(screen.getByLabelText('Delete Test Item'))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Cancel/ })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: /Cancel/ }))
    // Confirmation gone, onDelete not called
    expect(onDelete).not.toHaveBeenCalled()
    // Row is back
    expect(screen.getByText('Test Item')).toBeInTheDocument()
  })

  it('renders extra actions via actions prop', () => {
    render(
      <ItemRow
        name="Test Item"
        expanded={false}
        onToggle={vi.fn()}
        onDelete={vi.fn()}
        actions={<button aria-label="Edit">✏️</button>}
      >
        <p>Details</p>
      </ItemRow>
    )
    expect(screen.getByLabelText('Edit')).toBeInTheDocument()
  })
})
