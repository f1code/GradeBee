import 'vanilla-cookieconsent/dist/cookieconsent.css'
import './cookieconsent-overrides.css'
import * as CookieConsent from 'vanilla-cookieconsent'
import { DIAGNOSTICS_CATEGORY, notifyDiagnosticsConsentChanged } from './diagnosticsConsent'
import { syncSentryWithConsent } from './sentryConsent'

function onConsentChanged(): void {
  void syncSentryWithConsent()
  notifyDiagnosticsConsentChanged()
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
              'We use Clerk to sign you in (required). Optional diagnostics help us fix bugs and improve the app via Sentry — error reports and short session replays. Feedback you deliberately send us is handled separately; see below.',
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
                  'When enabled, Sentry may collect error reports, a short session replay whenever something goes wrong, and a small random sample of other sessions. Replays mask all text on screen, not just what you type, and block images and video.',
                linkedCategory: DIAGNOSTICS_CATEGORY,
              },
              {
                // Not a toggle: this is the accepted exception in
                // docs/adr/0003-no-child-pii-in-telemetry.md, stated where the
                // teacher actually decides rather than only in the README.
                title: 'Feedback you send us',
                description:
                  'A bug report, a suggestion, or a comment on a 👎 rating is forwarded to Sentry exactly as you wrote it, and the 👎 comment is sent whether or not diagnostics are enabled above — you asked us to read it, so we do not treat it as optional diagnostics. It is not filtered, so please leave student names out.',
              },
            ],
          },
        },
      },
    },
    onConsent: onConsentChanged,
    onChange: ({ changedCategories }) => {
      if (changedCategories.includes(DIAGNOSTICS_CATEGORY) || changedCategories.includes('necessary')) {
        onConsentChanged()
      }
    },
  })
}

export function showPrivacyPreferences(): void {
  CookieConsent.showPreferences()
}
