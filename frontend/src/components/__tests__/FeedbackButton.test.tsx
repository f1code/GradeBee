import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, vi } from 'vitest'

const mockCreateForm = vi.fn()
const mockGetFeedback = vi.fn()

vi.mock('@sentry/react', () => ({
  setUser: vi.fn(),
  getFeedback: () => mockGetFeedback(),
}))

let consented = true
vi.mock('../../hooks/useDiagnosticsConsent', () => ({
  useDiagnosticsConsent: () => consented,
}))

beforeEach(() => {
  vi.clearAllMocks()
  consented = true
  mockCreateForm.mockResolvedValue({ appendToDom: vi.fn(), open: vi.fn() })
  mockGetFeedback.mockReturnValue({ createForm: mockCreateForm })
})

async function renderFab() {
  const { default: FeedbackButton } = await import('../FeedbackButton')
  const user = userEvent.setup()
  render(<FeedbackButton userId="user_1" userEmail="t@example.com" />)
  return user
}

describe('FeedbackButton', () => {
  // The widget message body is teacher free text forwarded to Sentry unscrubbed
  // — the same accepted exception as the thumbs-down box, so it carries the same
  // ask. It lives in messageLabel, not the placeholder, which vanishes on the
  // first keystroke. See docs/adr/0003-no-child-pii-in-telemetry.md.
  it.each([
    ['bug', 'Report a bug'],
    ['suggestion', 'Suggest a feature'],
  ])('asks for no student names on the %s form', async (_type, menuItem) => {
    const user = await renderFab()
    await user.click(screen.getByLabelText('Give feedback'))
    await user.click(screen.getByRole('menuitem', { name: new RegExp(menuItem, 'i') }))

    expect(mockCreateForm).toHaveBeenCalledTimes(1)
    expect(mockCreateForm.mock.calls[0][0].messageLabel).toMatch(/student names/i)
  })

  it('renders nothing without diagnostics consent', async () => {
    consented = false
    await renderFab()
    expect(screen.queryByLabelText('Give feedback')).not.toBeInTheDocument()
  })
})
