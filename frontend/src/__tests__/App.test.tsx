import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

const validConsentMock = vi.fn()
const subscribeCookieConsentMock = vi.fn().mockReturnValue(() => {})

vi.mock('@clerk/react', () => ({
  Show: ({ when, children }: { when: string; children: ReactNode }) => {
    if (when === 'signed-out') return <>{children}</>
    return null
  },
  SignInButton: ({ children }: { children: ReactNode }) => <>{children}</>,
  UserButton: () => null,
  useUser: () => ({ user: null }),
}))

vi.mock('../consent/cookieConsent', () => ({
  hasCookieConsent: () => validConsentMock(),
  subscribeCookieConsent: (listener: () => void) => subscribeCookieConsentMock(listener),
}))

import App from '../App'

describe('App sign-in gating', () => {
  beforeEach(() => {
    localStorage.clear()
    validConsentMock.mockReset()
    subscribeCookieConsentMock.mockClear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('disables the Sign in button until the AI disclosure is checked and cookie consent is given', () => {
    validConsentMock.mockReturnValue(false)
    render(<App />)
    const button = screen.getByTestId('sign-in-button') as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect(screen.getByTestId('cookie-consent-hint')).toBeInTheDocument()
  })

  it('keeps the Sign in button disabled when only cookie consent is given', () => {
    validConsentMock.mockReturnValue(true)
    render(<App />)
    const button = screen.getByTestId('sign-in-button') as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect(screen.queryByTestId('cookie-consent-hint')).not.toBeInTheDocument()
  })

  it('enables the Sign in button once both consents are given', async () => {
    const user = userEvent.setup()
    validConsentMock.mockReturnValue(true)
    render(<App />)
    const checkbox = screen.getByRole('checkbox')
    await user.click(checkbox)
    const button = screen.getByTestId('sign-in-button') as HTMLButtonElement
    expect(button.disabled).toBe(false)
  })

  it('keeps the Sign in button disabled when only the AI disclosure is checked', async () => {
    const user = userEvent.setup()
    validConsentMock.mockReturnValue(false)
    render(<App />)
    const checkbox = screen.getByRole('checkbox')
    await user.click(checkbox)
    const button = screen.getByTestId('sign-in-button') as HTMLButtonElement
    expect(button.disabled).toBe(true)
  })
})
