import * as Sentry from '@sentry/react'
import { isDiagnosticsConsented } from './diagnosticsConsent'

let sentryInitialised = false

const sentryDsn = import.meta.env.VITE_SENTRY_DSN as string | undefined

/**
 * Initialise Sentry only when diagnostics consent is granted and a DSN is configured.
 * Replay is included only here — never at module load before consent.
 */
export function initSentryIfConsented(): void {
  if (!sentryDsn || !isDiagnosticsConsented()) return
  if (sentryInitialised) return

  Sentry.init({
    dsn: sentryDsn,
    release: import.meta.env.VITE_APP_VERSION as string | undefined,
    integrations: [
      Sentry.feedbackIntegration({
        autoInject: false,
      }),
      Sentry.replayIntegration(),
    ],
    replaysSessionSampleRate: 0.01,
    replaysOnErrorSampleRate: 1.0,
  })
  sentryInitialised = true
}

/** Tear down Sentry when diagnostics consent is revoked. */
export async function closeSentryIfRevoked(): Promise<void> {
  if (isDiagnosticsConsented()) return
  if (!sentryInitialised) return

  await Sentry.close(2000)
  sentryInitialised = false
}

/** Sync Sentry client state with current diagnostics consent. */
export async function syncSentryWithConsent(): Promise<void> {
  if (isDiagnosticsConsented()) {
    initSentryIfConsented()
  } else {
    await closeSentryIfRevoked()
  }
}

/** Test-only reset of module state. */
export function resetSentryConsentStateForTests(): void {
  sentryInitialised = false
}

export function isSentryInitialisedForTests(): boolean {
  return sentryInitialised
}
