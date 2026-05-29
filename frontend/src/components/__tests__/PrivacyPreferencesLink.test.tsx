import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import PrivacyPreferencesLink from '../PrivacyPreferencesLink'

const showPrivacyPreferencesMock = vi.fn()

vi.mock('../../consent/cookieConsent', () => ({
  showPrivacyPreferences: () => showPrivacyPreferencesMock(),
}))

describe('PrivacyPreferencesLink', () => {
  it('opens privacy preferences when clicked', async () => {
    const user = userEvent.setup()
    render(<PrivacyPreferencesLink />)
    await user.click(screen.getByRole('button', { name: 'Privacy preferences' }))
    expect(showPrivacyPreferencesMock).toHaveBeenCalledTimes(1)
  })
})
