import { Show, SignInButton, UserButton } from '@clerk/react'
import { useState, useEffect, useRef } from 'react'
import { motion } from 'motion/react'
import StudentList from './components/StudentList'
import AudioUpload from './components/AudioUpload'
import JobStatus from './components/JobStatus'
import ReportGeneration from './components/ReportGeneration'
import HowItWorks from './components/HowItWorks'
import HintBanner from './components/HintBanner'

function BeeIcon({ size = 28 }: { size?: number }) {
  return (
    <svg
      className="bee-icon"
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      {/* Honeycomb hexagon */}
      <path
        d="M16 2L28.124 9V23L16 30L3.876 23V9L16 2Z"
        fill="#FFF3D4"
        stroke="#E8A317"
        strokeWidth="1.5"
      />
      {/* Bee body */}
      <ellipse cx="16" cy="16.5" rx="5.5" ry="6.5" fill="#E8A317" />
      {/* Stripes */}
      <rect x="10.5" y="14" width="11" height="1.8" rx="0.9" fill="#2C1810" />
      <rect x="10.5" y="17.5" width="11" height="1.8" rx="0.9" fill="#2C1810" />
      {/* Wings */}
      <ellipse cx="12" cy="12" rx="3" ry="2" fill="#FFF3D4" opacity="0.85" transform="rotate(-20 12 12)" />
      <ellipse cx="20" cy="12" rx="3" ry="2" fill="#FFF3D4" opacity="0.85" transform="rotate(20 20 12)" />
      {/* Eyes */}
      <circle cx="14" cy="13.8" r="0.9" fill="#2C1810" />
      <circle cx="18" cy="13.8" r="0.9" fill="#2C1810" />
    </svg>
  )
}

function App() {
  const [activeTab, setActiveTab] = useState<'notes' | 'reports'>('notes')
  const [showGuide, setShowGuide] = useState(false)

  return (
    <div className="app">
      <header>
        <div className="header-logo">
          <BeeIcon />
          <h1>GradeBee</h1>
        </div>
        <div className="header-actions">
          <Show when="signed-in">
            <button className="how-it-works-trigger" onClick={() => setShowGuide(true)} aria-label="How it works">?</button>
            <UserButton />
          </Show>
        </div>
      </header>
      <main>
        <Show when="signed-out">
          <div className="sign-in-container" data-testid="sign-in-container">
            <motion.div
              className="sign-in-card"
              initial={{ opacity: 0, y: 20 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.5, ease: 'easeOut' }}
            >
              <h2>Welcome to GradeBee</h2>
              <p className="sign-in-tagline">Record verbal feedback about your students and GradeBee turns it into polished, structured notes and report cards — saved straight to your Google Drive.</p>
              <ul className="feature-list">
                <li>🎤 Record or upload audio of your observations</li>
                <li>🗂️ Notes are created automatically for each student</li>
                <li>📄 Generate report cards that match your writing style</li>
              </ul>
              <SignInButton mode="modal">
                <button className="sign-in-btn" data-testid="sign-in-button">Sign in with Google</button>
              </SignInButton>
            </motion.div>
          </div>
        </Show>
        <Show when="signed-in">
          <SignedInContent activeTab={activeTab} setActiveTab={setActiveTab} setShowGuide={setShowGuide} />
        </Show>
      </main>
      {showGuide && <HowItWorks onClose={() => setShowGuide(false)} />}
    </div>
  )
}

function SignedInContent({ activeTab, setActiveTab, setShowGuide }: {
  activeTab: 'notes' | 'reports'
  setActiveTab: (v: 'notes' | 'reports') => void
  setShowGuide: (v: boolean) => void
}) {
  const jobPollNowRef = useRef<(() => void) | null>(null)

  // Auto-show guide on first visit
  useEffect(() => {
    if (!localStorage.getItem('gradebee:seenGuide')) {
      setShowGuide(true)
      localStorage.setItem('gradebee:seenGuide', '1')
    }
  }, [setShowGuide])

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: 0.3 }}
    >
      <nav className="app-nav">
        <button
          className={`toolbar-link ${activeTab === 'notes' ? 'active' : ''}`}
          onClick={() => setActiveTab('notes')}
        >
          🎙️ Notes
        </button>
        <button
          className={`toolbar-link ${activeTab === 'reports' ? 'active' : ''}`}
          onClick={() => setActiveTab('reports')}
        >
          📝 Reports
        </button>
      </nav>
      {activeTab === 'notes' ? (
        <>
          <HintBanner storageKey="gradebee:hint:notes">Upload audio — GradeBee processes it in the background and creates notes automatically.</HintBanner>
          <StudentList />
          <JobStatus pollNowRef={jobPollNowRef} />
          <AudioUpload onUploadDone={() => jobPollNowRef.current?.()} />
        </>
      ) : (
        <>
          <HintBanner storageKey="gradebee:hint:reports">Select students and a date range to generate report cards from your accumulated notes.</HintBanner>
          <ReportGeneration />
        </>
      )}
    </motion.div>
  )
}

export default App
