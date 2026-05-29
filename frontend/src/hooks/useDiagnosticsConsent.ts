import { useEffect, useState } from 'react'
import {
  isDiagnosticsConsented,
  subscribeDiagnosticsConsent,
} from '../consent/diagnosticsConsent'

export function useDiagnosticsConsent(): boolean {
  const [consented, setConsented] = useState(() => isDiagnosticsConsented())

  useEffect(() => {
    return subscribeDiagnosticsConsent(() => setConsented(isDiagnosticsConsented()))
  }, [])

  return consented
}
