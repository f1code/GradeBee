import { useEffect } from 'react'

/** Call `onClose` when Escape is pressed while `active`. */
export function useEscape(onClose: () => void, active = true) {
  useEffect(() => {
    if (!active) return
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onClose, active])
}
