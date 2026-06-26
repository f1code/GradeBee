import { test, expect } from '@playwright/test'

const API_URL = process.env.VITE_API_URL ?? 'http://localhost:8080'

test.describe('Backend API health check', () => {
  test('GET /health returns ok', async ({ request }) => {
    const response = await request.get(`${API_URL}/health`)
    expect(response.status()).toBe(200)
    expect(await response.json()).toEqual({ status: 'ok' })
  })

  test('GET / serves SPA', async ({ request }) => {
    const response = await request.get(`${API_URL}/`)
    expect(response.status()).toBe(200)
    const ct = response.headers()['content-type'] ?? ''
    expect(ct).toContain('text/html')
  })

  test('unknown API route returns 404', async ({ request }) => {
    const response = await request.get(`${API_URL}/api/nonexistent`)
    expect(response.status()).toBe(404)
    expect(await response.json()).toEqual({ error: 'not found' })
  })
})
