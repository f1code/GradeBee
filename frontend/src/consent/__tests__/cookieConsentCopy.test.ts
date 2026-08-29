import { beforeEach, describe, it, expect, vi } from 'vitest'

const mockRun = vi.fn()

vi.mock('vanilla-cookieconsent', () => ({
  run: (...args: unknown[]) => mockRun(...args),
}))
vi.mock('vanilla-cookieconsent/dist/cookieconsent.css', () => ({}))
vi.mock('../cookieconsent-overrides.css', () => ({}))

interface Section {
  title: string
  description?: string
  linkedCategory?: string
}

interface EnCopy {
  consentModal: { description: string }
  preferencesModal: { sections: Section[] }
}

async function copy(): Promise<EnCopy> {
  const { initCookieConsent } = await import('../cookieConsent')
  initCookieConsent()
  expect(mockRun).toHaveBeenCalledTimes(1)
  const config = mockRun.mock.calls[0][0] as {
    language: { translations: { en: EnCopy } }
  }
  return config.language.translations.en
}

async function sections(): Promise<Section[]> {
  return (await copy()).preferencesModal.sections
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.resetModules()
})

// The dialog is where the teacher actually decides, so it is the one place the
// consent claim must not be wrong. ADR 0003 names this copy as part of the
// accepted exception, which is why it is asserted rather than remembered.
describe('privacy dialog copy', () => {
  it('does not present feedback as gated by the diagnostics toggle', async () => {
    const diagnostics = (await sections()).find(s => s.linkedCategory === 'diagnostics')
    expect(diagnostics).toBeDefined()
    expect(diagnostics!.description).not.toMatch(/feedback/i)
  })

  // The banner is read before "Accept all" — the widest readership of the three
  // statements, and the one that was conflating feedback with diagnostics.
  it('does not file feedback under optional diagnostics in the banner', async () => {
    const banner = (await copy()).consentModal.description
    expect(banner).toMatch(/diagnostics/i)
    expect(banner).toMatch(/handled separately/i)
    expect(banner).not.toMatch(/diagnostics[^.]*feedback/i)
  })

  it('tells the teacher the feedback comment is sent either way', async () => {
    const feedback = (await sections()).find(s => /feedback/i.test(s.title))
    expect(feedback).toBeDefined()
    // No linkedCategory: it must not render as a toggle the teacher could
    // mistake for an opt-out that does not exist.
    expect(feedback!.linkedCategory).toBeUndefined()
    expect(feedback!.description).toMatch(/whether or not/i)
    expect(feedback!.description).toMatch(/student names/i)
  })
})
