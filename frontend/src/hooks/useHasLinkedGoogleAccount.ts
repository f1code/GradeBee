import { useUser } from '@clerk/react'

/** True when the signed-in user has a linked Google external account in Clerk. */
export function useHasLinkedGoogleAccount(): boolean {
  const { user, isLoaded } = useUser()
  if (!isLoaded || !user) return false
  return user.externalAccounts.some((account) => account.provider === 'google')
}
