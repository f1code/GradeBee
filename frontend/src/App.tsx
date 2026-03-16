import { SignedIn, SignedOut, SignInButton, UserButton } from '@clerk/clerk-react'
import { useState, useEffect } from 'react'
import DriveSetup from './components/DriveSetup'
import StudentList from './components/StudentList'

const SETUP_DONE_KEY = 'gradebee-setup-done'

function App() {
  const [setupDone, setSetupDoneState] = useState<boolean | null>(null)

  useEffect(() => {
    const stored = localStorage.getItem(SETUP_DONE_KEY)
    setSetupDoneState(stored === 'true')
  }, [])

  function markSetupDone() {
    localStorage.setItem(SETUP_DONE_KEY, 'true')
    setSetupDoneState(true)
  }

  function resetSetupDone() {
    localStorage.removeItem(SETUP_DONE_KEY)
    setSetupDoneState(false)
  }

  return (
    <div className="app">
      <header>
        <h1>GradeBee</h1>
        <SignedIn>
          <UserButton />
        </SignedIn>
      </header>
      <main>
        <SignedOut>
          <div className="sign-in-container" data-testid="sign-in-container">
            <h2>Welcome to GradeBee</h2>
            <p>Sign in with Google to get started.</p>
            <SignInButton mode="modal">
              <button className="sign-in-btn" data-testid="sign-in-button">Sign in with Google</button>
            </SignInButton>
          </div>
        </SignedOut>
        <SignedIn>
          {setupDone === null ? (
            <p>Loading...</p>
          ) : setupDone ? (
            <StudentList onSetupRequired={resetSetupDone} />
          ) : (
            <DriveSetup onComplete={markSetupDone} />
          )}
        </SignedIn>
      </main>
    </div>
  )
}

export default App
