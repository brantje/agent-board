const publicAuthPaths = new Set(['/auth/login', '/auth/register', '/auth/setup', '/auth/reset'])

export function authRedirect(path: string, authenticated: boolean, forcePasswordChange: boolean, admin: boolean) {
  if (!authenticated) return publicAuthPaths.has(path) ? null : '/auth/login'
  if (forcePasswordChange && path !== '/account') return '/account'
  if ((path === '/settings' || path.startsWith('/settings/')) && !admin) return '/account'
  if (publicAuthPaths.has(path)) return '/account'
  return null
}
