import { authRedirect } from '../utils/auth-route'

export default defineNuxtRouteMiddleware(async (to) => {
  if (import.meta.server) return
  const auth = useAuth()
  await auth.initialize()
  const redirect = authRedirect(
    to.path,
    auth.isAuthenticated.value,
    Boolean(auth.user.value?.forcePasswordChange),
    auth.isAdmin.value
  )
  if (redirect) return navigateTo(redirect)
})
