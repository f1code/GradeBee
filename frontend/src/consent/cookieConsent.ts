import 'vanilla-cookieconsent/dist/cookieconsent.css'
import './cookieconsent-overrides.css'
import * as CookieConsent from 'vanilla-cookieconsent'
import { DIAGNOSTICS_CATEGORY, notifyDiagnosticsConsentChanged } from './diagnosticsConsent'
import { syncSentryWithConsent } from './sentryConsent'

const consentListeners = new Set<() => void>()

/** Whether the user has made a cookie consent decision (accept all / necessary only / preferences). */
export function hasCookieConsent(): boolean {
  return CookieConsent.validConsent()
}

export function subscribeCookieConsent(listener: () => void): () => void {
  consentListeners.add(listener)
  return () => consentListeners.delete(listener)
}

function notifyCookieConsentChanged(): void {
  for (const listener of consentListeners) {
    listener()
  }
}

function onConsentChanged(): void {
  void syncSentryWithConsent()
  notifyDiagnosticsConsentChanged()
  notifyCookieConsentChanged()
}

export function initCookieConsent(): void {
  CookieConsent.run({
    guiOptions: {
      consentModal: {
        layout: 'box',
        position: 'bottom center',
      },
      preferencesModal: {
        layout: 'box',
      },
    },
    categories: {
      necessary: {
        enabled: true,
        readOnly: true,
      },
      [DIAGNOSTICS_CATEGORY]: {},
    },
    language: {
      default: 'en',
      translations: {
        en: {
          consentModal: {
            title: 'Privacy choices for GradeBee',
            description:
              'We use Clerk to sign you in (required). Optional diagnostics help us fix bugs and improve the app via Sentry — including error reports, feedback, and short session replays when something goes wrong.',
            acceptAllBtn: 'Accept all',
            acceptNecessaryBtn: 'Necessary only',
            showPreferencesBtn: 'Manage preferences',
          },
          preferencesModal: {
            title: 'Privacy preferences',
            acceptAllBtn: 'Accept all',
            acceptNecessaryBtn: 'Necessary only',
            savePreferencesBtn: 'Save choices',
            closeIconLabel: 'Close',
            sections: [
              {
                title: 'Necessary',
                description:
                  'Clerk authentication and session cookies are required to sign in and use GradeBee. These cannot be turned off while using the app.',
                linkedCategory: 'necessary',
              },
              {
                title: 'Diagnostics (optional)',
                description:
                  'When enabled, Sentry may collect error reports, in-app feedback you submit, and short session replays tied to errors or feedback. Text inputs are masked in replays by default.',
                linkedCategory: DIAGNOSTICS_CATEGORY,
              },
            ],
          },
        },
      },
    },
    onConsent: onConsentChanged,
    onChange: ({ changedCategories }) => {
      if (
        changedCategories.includes(DIAGNOSTICS_CATEGORY) ||
        changedCategories.includes('necessary')
      ) {
        onConsentChanged()
      }
    },
  })
}

export function showPrivacyPreferences(): void {
  CookieConsent.showPreferences()
}
