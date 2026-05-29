import { showPrivacyPreferences } from '../consent/cookieConsent'

/**
 * Reopens the cookie consent preferences modal so users can change diagnostics consent.
 */
export default function PrivacyPreferencesLink() {
  return (
    <button
      type="button"
      className="privacy-preferences-link text-link"
      onClick={() => showPrivacyPreferences()}
    >
      Privacy preferences
    </button>
  )
}
