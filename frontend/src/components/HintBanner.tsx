import { useState } from 'react'
import { motion, AnimatePresence } from 'motion/react'

export default function HintBanner({ storageKey, children }: { storageKey: string; children: React.ReactNode }) {
  const [dismissed, setDismissed] = useState(() => localStorage.getItem(storageKey) === '1')

  if (dismissed) return null

  function dismiss() {
    localStorage.setItem(storageKey, '1')
    setDismissed(true)
  }

  return (
    <AnimatePresence>
      {!dismissed && (
        <motion.div
          className="hint-banner"
          initial={{ opacity: 0, height: 0 }}
          animate={{ opacity: 1, height: 'auto' }}
          exit={{ opacity: 0, height: 0 }}
          transition={{ duration: 0.25 }}
        >
          <p>{children}</p>
          <button className="hint-banner-close" onClick={dismiss} aria-label="Dismiss">×</button>
        </motion.div>
      )}
    </AnimatePresence>
  )
}
