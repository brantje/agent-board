import { authRedirect } from '../utils/auth-route'

export default defineNuxtRouteMiddleware(async (to) => {
  if (import.meta.server) return
  const auth = useAuth()
  await auth.initialize()

  let bootstrapAvailable = false
  if (!auth.isAuthenticated.value) {
    try {
      bootstrapAvailable = await auth.bootstrapAvailable()
    } catch {
      // Keep the normal login boundary when bootstrap status is temporarily unavailable.
    }
  }

  const redirect = authRedirect(
    to.path,
    auth.isAuthenticated.value,
    Boolean(auth.user.value?.forcePasswordChange),
    auth.isAdmin.value,
    bootstrapAvailable
  )
  if (redirect) return navigateTo(redirect)
})
