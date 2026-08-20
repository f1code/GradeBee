import { useState } from 'react'
import { AnimatePresence } from 'motion/react'
import CollapsePresence from './CollapsePresence'

export default function HintBanner({ storageKey, children }: { storageKey: string; children: React.ReactNode }) {
  const [dismissed, setDismissed] = useState(() => localStorage.getItem(storageKey) === '1')

  function dismiss() {
    localStorage.setItem(storageKey, '1')
    setDismissed(true)
  }

  return (
    <AnimatePresence>
      {!dismissed && (
        <CollapsePresence>
          <div className="hint-banner">
            <p>{children}</p>
            <button className="hint-banner-close" onClick={dismiss} aria-label="Dismiss">×</button>
          </div>
        </CollapsePresence>
      )}
    </AnimatePresence>
  )
}
