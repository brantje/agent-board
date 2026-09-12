const publicAuthPaths = new Set(['/login', '/register', '/setup', '/reset'])

export function authRedirect(path: string, authenticated: boolean, forcePasswordChange: boolean, admin: boolean) {
  if (!authenticated) return publicAuthPaths.has(path) ? null : '/login'
  if (forcePasswordChange && path !== '/account/password') return '/account/password'
  if (path.startsWith('/settings/') && !admin) return '/account'
  if (publicAuthPaths.has(path)) return '/account'
  return null
}
