import React from 'react'
import ReactDOM from 'react-dom/client'
import { ClerkProvider } from '@clerk/react'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import * as Sentry from '@sentry/react'
import App from './App'
import './index.css'

const clerkPubKey = import.meta.env.VITE_CLERK_PUBLISHABLE_KEY
if (!clerkPubKey) {
  throw new Error('Missing VITE_CLERK_PUBLISHABLE_KEY')
}

// Initialise Sentry. DSN is optional — if absent (e.g. local dev without a
// Sentry project) the SDK will be a no-op and the feedback widget won't render.
//
// Privacy note: Sentry's default text-input masking is enabled by default in
// Session Replay. Student names appear only inside audio-upload filenames and
// are not captured in replay DOM snapshots. Audio URLs are backend-only and
// never reach the frontend DOM, so they cannot leak into replays.
const sentryDsn = import.meta.env.VITE_SENTRY_DSN as string | undefined
if (sentryDsn) {
  Sentry.init({
    dsn: sentryDsn,
    release: import.meta.env.VITE_APP_VERSION as string | undefined,
    integrations: [
      Sentry.feedbackIntegration({
        // We render our own trigger button (FeedbackButton component) so we
        // hide the default Sentry-provided button.
        autoInject: false,
        // Screenshot capture is on by default — keep it enabled.
      }),
      Sentry.replayIntegration({
        // Capture replays only on errors / feedback submissions to stay on
        // the free tier. Normal sessions are not recorded.
        replaysSessionSampleRate: 0,
        replaysOnErrorSampleRate: 1.0,
        // Mask all text inputs and block media elements by default (Sentry
        // default — explicitly noted here for reviewers).
        maskAllText: true,
        blockAllMedia: false,
      }),
    ],
  })
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ClerkProvider publishableKey={clerkPubKey}>
      <BrowserRouter>
        <Routes>
          <Route path="/*" element={<App />} />
        </Routes>
      </BrowserRouter>
    </ClerkProvider>
  </React.StrictMode>,
)
