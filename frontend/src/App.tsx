import { SignedIn, SignedOut, SignInButton, UserButton } from '@clerk/clerk-react'
import DriveSetup from './components/DriveSetup'

function App() {
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
          <DriveSetup />
        </SignedIn>
      </main>
    </div>
  )
}

export default App
