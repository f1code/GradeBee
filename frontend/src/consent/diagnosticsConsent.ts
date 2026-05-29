import * as CookieConsent from 'vanilla-cookieconsent'

export const DIAGNOSTICS_CATEGORY = 'diagnostics'

const listeners = new Set<() => void>()

/** Whether the user has opted in to optional diagnostics (Sentry). */
export function isDiagnosticsConsented(): boolean {
  if (!CookieConsent.validConsent()) return false
  return CookieConsent.acceptedCategory(DIAGNOSTICS_CATEGORY)
}

export function subscribeDiagnosticsConsent(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function notifyDiagnosticsConsentChanged(): void {
  for (const listener of listeners) {
    listener()
  }
}
