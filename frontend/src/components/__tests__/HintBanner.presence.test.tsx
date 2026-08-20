vi.unmock('motion/react')

import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'vitest'
import HintBanner from '../HintBanner'

describe('HintBanner presence', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('keeps the banner mounted through the exit animation', async () => {
    render(<HintBanner storageKey="test-hint">Hello hint</HintBanner>)

    fireEvent.click(screen.getByLabelText('Dismiss'))

    expect(screen.getByText('Hello hint')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByText('Hello hint')).not.toBeInTheDocument()
    })
    expect(localStorage.getItem('test-hint')).toBe('1')
  })
})
