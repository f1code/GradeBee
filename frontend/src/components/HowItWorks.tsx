import { motion, AnimatePresence } from 'motion/react'

const steps = [
  {
    num: 1,
    heading: 'Set up your class list',
    desc: 'Create a Google Sheets spreadsheet with your classes and student names. GradeBee reads it to match recordings to students.',
  },
  {
    num: 2,
    heading: 'Record your observations',
    desc: 'Upload or record audio of your verbal feedback. You can also import audio files already in your Drive.',
  },
  {
    num: 3,
    heading: 'Review & edit notes',
    desc: 'GradeBee transcribes the audio and creates a structured note for each student mentioned. Review, tweak, and save — notes are stored as Google Docs.',
  },
  {
    num: 4,
    heading: 'Generate report cards',
    desc: 'When it\'s report time, select a date range and students. GradeBee aggregates all notes into a report card that matches your style. Upload example reports so it learns your voice.',
  },
]

export default function HowItWorks({ onClose }: { onClose: () => void }) {
  return (
    <AnimatePresence>
      <motion.div
        className="how-it-works-overlay"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        onClick={onClose}
      >
        <motion.div
          className="how-it-works-card card"
          initial={{ opacity: 0, y: 30, scale: 0.97 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, y: 20 }}
          transition={{ duration: 0.3, ease: 'easeOut' }}
          onClick={(e) => e.stopPropagation()}
        >
          <button className="how-it-works-close" onClick={onClose} aria-label="Close">×</button>
          <h2>How it works</h2>
          <div className="guide-steps">
            {steps.map((s) => (
              <div className="guide-step" key={s.num}>
                <span className="guide-step-num">{s.num}</span>
                <div>
                  <h3>{s.heading}</h3>
                  <p>{s.desc}</p>
                </div>
              </div>
            ))}
          </div>
          <button className="guide-dismiss-btn" onClick={onClose}>Got it</button>
        </motion.div>
      </motion.div>
    </AnimatePresence>
  )
}
