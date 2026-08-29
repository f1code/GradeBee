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
    feedbackIntegrationMock.mockClear()
    replayIntegrationMock.mockClear()
    isDiagnosticsConsentedMock.mockReturnValue(false)
    vi.stubEnv('VITE_SENTRY_DSN', 'https://example@o0.ingest.sentry.io/1')
    vi.stubEnv('VITE_SENTRY_ENVIRONMENT', 'production')
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
    expect(initMock.mock.calls[0][0]).toMatchObject({ environment: 'production' })
    expect(replayIntegrationMock).toHaveBeenCalled()
    expect(feedbackIntegrationMock).toHaveBeenCalled()
  })

  it('tags review-app events and disables replay outside production', async () => {
    vi.stubEnv('VITE_SENTRY_ENVIRONMENT', 'review')
    isDiagnosticsConsentedMock.mockReturnValue(true)
    const { initSentryIfConsented, resetSentryConsentStateForTests } = await import('../sentryConsent')
    resetSentryConsentStateForTests()
    initSentryIfConsented()
    expect(initMock.mock.calls[0][0]).toMatchObject({
      environment: 'review',
      replaysSessionSampleRate: 0,
      replaysOnErrorSampleRate: 0,
    })
    expect(replayIntegrationMock).not.toHaveBeenCalled()
    expect(feedbackIntegrationMock).toHaveBeenCalled()
  })

  it('defaults to the development environment when unconfigured', async () => {
    vi.stubEnv('VITE_SENTRY_ENVIRONMENT', '')
    isDiagnosticsConsentedMock.mockReturnValue(true)
    const { initSentryIfConsented, resetSentryConsentStateForTests } = await import('../sentryConsent')
    resetSentryConsentStateForTests()
    initSentryIfConsented()
    expect(initMock.mock.calls[0][0]).toMatchObject({ environment: 'development' })
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
