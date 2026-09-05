import '@testing-library/jest-dom/vitest'
import { vi } from 'vitest'

// Mock motion/react — render children directly, no animations
vi.mock('motion/react', async () => {
  const React = await import('react')

  function createMotionProxy() {
    // One component per tag, cached: a fresh one on every `motion.div` read
    // would be a new component type each render, and React would remount the
    // subtree and wipe any child state a test is watching.
    const cache = new Map<string, unknown>()

    return new Proxy(
      {},
      {
        get(_target: Record<string, unknown>, prop: string) {
          const cached = cache.get(prop)
          if (cached) return cached
          const component = React.forwardRef((props: Record<string, unknown>, ref: React.Ref<unknown>) => {
            const {
              initial: _initial,
              animate: _animate,
              exit: _exit,
              transition: _transition,
              variants: _variants,
              whileHover: _whileHover,
              whileTap: _whileTap,
              whileFocus: _whileFocus,
              layout: _layout,
              layoutId: _layoutId,
              ...rest
            } = props
            void _initial; void _animate; void _exit; void _transition;
            void _variants; void _whileHover; void _whileTap; void _whileFocus;
            void _layout; void _layoutId;
            return React.createElement(prop, { ...rest, ref })
          })
          cache.set(prop, component)
          return component
        },
      },
    )
  }

  const AnimatePresence = ({ children }: { children: React.ReactNode }) => React.createElement(React.Fragment, null, children)

  return {
    motion: createMotionProxy(),
    AnimatePresence,
    useAnimation: () => ({ start: vi.fn(), stop: vi.fn() }),
    useMotionValue: (init: number) => ({ get: () => init, set: vi.fn() }),
    useTransform: (_val: unknown, _i: unknown, o: number[]) => ({ get: () => o[0] }),
  }
})
