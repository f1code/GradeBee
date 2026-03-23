import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import HowItWorks from '../HowItWorks'

describe('HowItWorks', () => {
  it('renders all step headings', () => {
    render(<HowItWorks onClose={vi.fn()} />)
    expect(screen.getByText('Set up your class list')).toBeInTheDocument()
    expect(screen.getByText('Record your observations')).toBeInTheDocument()
    expect(screen.getByText('Review & edit notes')).toBeInTheDocument()
    expect(screen.getByText('Generate report cards')).toBeInTheDocument()
  })

  it('calls onClose when Got it is clicked', async () => {
    const onClose = vi.fn()
    render(<HowItWorks onClose={onClose} />)
    const { default: userEvent } = await import('@testing-library/user-event')
    const user = userEvent.setup()
    await user.click(screen.getByText('Got it'))
    expect(onClose).toHaveBeenCalled()
  })
})
