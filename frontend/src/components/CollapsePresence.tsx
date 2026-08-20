import { motion } from 'motion/react'
import type { ReactNode } from 'react'

/** In-flow enter/exit: collapse height so the layout box shrinks with the fade. */
export default function CollapsePresence({ children }: { children: ReactNode }) {
  return (
    <motion.div
      initial={{ opacity: 0, height: 0 }}
      animate={{ opacity: 1, height: 'auto' }}
      exit={{ opacity: 0, height: 0 }}
      transition={{ duration: 0.2 }}
      style={{ overflow: 'hidden' }}
    >
      {children}
    </motion.div>
  )
}
