import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect } from 'vitest'
import HintBanner from '../HintBanner'

describe('HintBanner', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('renders children', () => {
    render(<HintBanner storageKey="test-hint">Hello hint</HintBanner>)
    expect(screen.getByText('Hello hint')).toBeInTheDocument()
  })

  it('dismiss hides banner and sets localStorage', async () => {
    const user = userEvent.setup()
    render(<HintBanner storageKey="test-hint">Hello hint</HintBanner>)

    await user.click(screen.getByLabelText('Dismiss'))
    expect(screen.queryByText('Hello hint')).not.toBeInTheDocument()
    expect(localStorage.getItem('test-hint')).toBe('1')
  })

  it('does not render if already dismissed', () => {
    localStorage.setItem('test-hint', '1')
    render(<HintBanner storageKey="test-hint">Hello hint</HintBanner>)
    expect(screen.queryByText('Hello hint')).not.toBeInTheDocument()
  })
})
