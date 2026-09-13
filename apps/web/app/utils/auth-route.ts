const publicAuthPaths = new Set(['/auth/login', '/auth/register', '/auth/setup', '/auth/reset'])

export function isPublicAuthPath(path: string) {
  return publicAuthPaths.has(path)
}

export function authRedirect(path: string, authenticated: boolean, forcePasswordChange: boolean, admin: boolean, bootstrapAvailable = false) {
  if (!authenticated) {
    if (bootstrapAvailable) return path === '/auth/register' ? null : '/auth/register'
    if (path === '/auth/register') return '/auth/login'
    return isPublicAuthPath(path) ? null : '/auth/login'
  }
  if (forcePasswordChange && path !== '/account') return '/account'
  if ((path === '/settings' || path.startsWith('/settings/')) && !admin) return '/account'
  if (isPublicAuthPath(path)) return '/'
  return null
}
