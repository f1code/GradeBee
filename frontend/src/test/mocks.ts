import { vi } from 'vitest'

export function mockClerkAuth(overrides: Record<string, unknown> = {}) {
  const auth = {
    getToken: vi.fn().mockResolvedValue('mock-token'),
    isSignedIn: true,
    userId: 'user_test',
    ...overrides,
  }
  vi.doMock('@clerk/react', () => ({
    useAuth: () => auth,
    ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  }))
  return auth
}

export function mockFetch(response: unknown, ok = true) {
  const fn = vi.fn().mockResolvedValue({
    ok,
    json: () => Promise.resolve(response),
    text: () => Promise.resolve(JSON.stringify(response)),
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

export function mockLocalStorage() {
  const store: Record<string, string> = {}
  const mock = {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => { store[key] = value }),
    removeItem: vi.fn((key: string) => { delete store[key] }),
    clear: vi.fn(() => { Object.keys(store).forEach((k) => delete store[k]) }),
  }
  vi.stubGlobal('localStorage', mock)
  return mock
}
