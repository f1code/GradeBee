import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const initMock = vi.fn()
const closeMock = vi.fn().mockResolvedValue(undefined)
const feedbackIntegrationMock = vi.fn(() => ({}))
const replayIntegrationMock = vi.fn(() => ({}))

vi.mock('@sentry/react', () => ({
  init: initMock,
  close: closeMock,
  feedbackIntegration: feedbackIntegrationMock,
  replayIntegration: replayIntegrationMock,
}))

const isDiagnosticsConsentedMock = vi.fn()

vi.mock('../diagnosticsConsent', () => ({
  isDiagnosticsConsented: () => isDiagnosticsConsentedMock(),
}))

describe('sentryConsent', () => {
  beforeEach(() => {
    vi.resetModules()
    initMock.mockClear()
    closeMock.mockClear()
    isDiagnosticsConsentedMock.mockReturnValue(false)
    vi.stubEnv('VITE_SENTRY_DSN', 'https://example@o0.ingest.sentry.io/1')
  })

  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('does not initialise Sentry without diagnostics consent', async () => {
    const { initSentryIfConsented } = await import('../sentryConsent')
    initSentryIfConsented()
    expect(initMock).not.toHaveBeenCalled()
  })

  it('initialises Sentry with replay when diagnostics consent is granted', async () => {
    isDiagnosticsConsentedMock.mockReturnValue(true)
    const { initSentryIfConsented, resetSentryConsentStateForTests } = await import('../sentryConsent')
    resetSentryConsentStateForTests()
    initSentryIfConsented()
    expect(initMock).toHaveBeenCalledTimes(1)
    expect(replayIntegrationMock).toHaveBeenCalled()
    expect(feedbackIntegrationMock).toHaveBeenCalled()
  })

  it('closes Sentry when diagnostics consent is revoked', async () => {
    isDiagnosticsConsentedMock.mockReturnValue(true)
    const {
      initSentryIfConsented,
      closeSentryIfRevoked,
      resetSentryConsentStateForTests,
    } = await import('../sentryConsent')
    resetSentryConsentStateForTests()
    initSentryIfConsented()
    isDiagnosticsConsentedMock.mockReturnValue(false)
    await closeSentryIfRevoked()
    expect(closeMock).toHaveBeenCalledWith(2000)
  })
})
