import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useMediaQuery } from '../useMediaQuery'

function mockMatchMedia() {
  const listeners: Record<string, ((e: MediaQueryListEvent) => void)[]> = {}

  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => {
      listeners[query] = listeners[query] || []
      return {
        matches: false,
        media: query,
        onchange: null,
        addEventListener: (_: string, cb: (e: MediaQueryListEvent) => void) => { listeners[query].push(cb) },
        removeEventListener: (_: string, cb: (e: MediaQueryListEvent) => void) => {
          listeners[query] = listeners[query].filter(l => l !== cb)
        },
        dispatchEvent: (e: MediaQueryListEvent) => {
          listeners[query]?.forEach(cb => cb(e))
          return true
        },
      }
    }),
  })

  return {
    fire(query: string, matches: boolean) {
      listeners[query]?.forEach(cb => cb({ matches, media: query } as MediaQueryListEvent))
    },
  }
}

describe('useMediaQuery', () => {
  let media: ReturnType<typeof mockMatchMedia>

  beforeEach(() => {
    media = mockMatchMedia()
  })

  it('returns initial match state', () => {
    const { result } = renderHook(() => useMediaQuery('(max-width: 640px)'))
    expect(result.current).toBe(false)
  })

  it('updates when media query changes', () => {
    const { result } = renderHook(() => useMediaQuery('(max-width: 640px)'))
    expect(result.current).toBe(false)

    act(() => {
      media.fire('(max-width: 640px)', true)
    })
    // jsdom matchMedia mock doesn't update .matches, but the event handler sets it
    // The hook reads e.matches from the event
    expect(result.current).toBe(true)
  })
})
