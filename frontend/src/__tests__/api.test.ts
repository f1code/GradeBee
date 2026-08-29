import { describe, it, expect, vi, afterEach } from 'vitest'

const { captureMessage } = vi.hoisted(() => ({ captureMessage: vi.fn() }))
vi.mock('@sentry/react', () => ({ captureMessage }))

import { uploadAudio } from '../api'

/**
 * Regression tests for the API client's error handling.
 *
 * Production bug (2026-08-29): a recorded voice note larger than the nginx
 * `client_max_body_size` was rejected by Dokku's nginx with an HTML 413 page.
 * `uploadAudio` called `resp.json()` unconditionally, so the browser surfaced
 * "JSON.parse: unexpected character at line 1 column 1 of the JSON data"
 * instead of the actual HTTP failure — and the real status never reached the
 * user or Sentry.
 */

const token = async () => 'test-token'

function stubFetch(resp: {
  ok: boolean
  status: number
  body: string
  contentType?: string
}) {
  const fn = vi.fn().mockResolvedValue({
    ok: resp.ok,
    status: resp.status,
    headers: { get: () => resp.contentType ?? 'text/html' },
    url: 'https://gradebee.app/api/voice-notes/upload',
    // Real Response.json() rejects on invalid JSON rather than throwing
    // synchronously; an async fn reproduces that.
    json: async () => JSON.parse(resp.body),
    text: async () => resp.body,
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

const NGINX_413 = `<html>
<head><title>413 Request Entity Too Large</title></head>
<body>
<center><h1>413 Request Entity Too Large</h1></center>
<hr><center>nginx</center>
</body>
</html>
`

describe('uploadAudio error handling', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    captureMessage.mockClear()
  })

  it('reports a size failure when the proxy rejects the body with an HTML 413', async () => {
    stubFetch({ ok: false, status: 413, body: NGINX_413 })
    const file = new File([new Uint8Array(8)], 'recording-2026-08-29T07-31-01-935Z.webm', {
      type: 'audio/webm',
    })

    await expect(uploadAudio(file, token)).rejects.toThrow(/too large/i)
    // Callers catch upload errors to render them as UI state, so the API layer
    // must report the failure itself or it never reaches diagnostics.
    expect(captureMessage).toHaveBeenCalledWith(
      'API 413 with non-JSON body: /api/voice-notes/upload',
      expect.objectContaining({ level: 'error' }),
    )
  })

  it('reports an expired session when the server returns 401 with an empty body', async () => {
    stubFetch({ ok: false, status: 401, body: '', contentType: 'application/json' })
    const file = new File([new Uint8Array(8)], 'recording.webm', { type: 'audio/webm' })

    await expect(uploadAudio(file, token)).rejects.toThrow(/sign in/i)
    // Session expiry is routine; it must not raise a Sentry issue.
    expect(captureMessage).not.toHaveBeenCalled()
  })

  it('reports a gateway failure when the proxy returns an HTML 502', async () => {
    stubFetch({ ok: false, status: 502, body: '<html><body>502 Bad Gateway</body></html>' })
    const file = new File([new Uint8Array(8)], 'recording.webm', { type: 'audio/webm' })

    await expect(uploadAudio(file, token)).rejects.toThrow(/unavailable/i)
    expect(captureMessage).toHaveBeenCalledWith(
      'API 502 with non-JSON body: /api/voice-notes/upload',
      expect.objectContaining({ level: 'error' }),
    )
  })

  it('still surfaces the server-supplied JSON error message', async () => {
    stubFetch({
      ok: false,
      status: 400,
      body: JSON.stringify({ error: 'unsupported file type' }),
      contentType: 'application/json',
    })
    const file = new File([new Uint8Array(8)], 'recording.webm', { type: 'audio/webm' })

    await expect(uploadAudio(file, token)).rejects.toThrow('unsupported file type')
    // Our own JSON errors already reach Sentry from the backend.
    expect(captureMessage).not.toHaveBeenCalled()
  })

  it('fails loudly when a 200 carries the SPA fallback HTML instead of JSON', async () => {
    stubFetch({ ok: true, status: 200, body: '<!doctype html><html></html>' })
    const file = new File([new Uint8Array(8)], 'recording.webm', { type: 'audio/webm' })

    await expect(uploadAudio(file, token)).rejects.toThrow(/unreadable/i)
    expect(captureMessage).toHaveBeenCalledWith(
      'API 200 with non-JSON body: /api/voice-notes/upload',
      expect.objectContaining({ level: 'error' }),
    )
  })

  it('returns the parsed payload on success', async () => {
    stubFetch({
      ok: true,
      status: 200,
      body: JSON.stringify({ uploadId: 7, fileName: 'recording.webm' }),
      contentType: 'application/json',
    })
    const file = new File([new Uint8Array(8)], 'recording.webm', { type: 'audio/webm' })

    await expect(uploadAudio(file, token)).resolves.toEqual({
      uploadId: 7,
      fileName: 'recording.webm',
    })
  })
})
