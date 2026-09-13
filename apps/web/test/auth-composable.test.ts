import { ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useAuth } from '../app/composables/useAuth'

const user = {
  id: '00000000-0000-0000-0000-000000000001',
  username: 'admin',
  email: 'admin@example.com',
  displayName: 'Admin',
  deploymentRole: 'admin' as const,
  status: 'active' as const,
  forcePasswordChange: false
}

function tokens(suffix: string) {
  return {
    accessToken: `test-access-${suffix}`,
    accessTokenExpiresAt: '2026-09-12T14:00:00Z',
    refreshToken: `test-refresh-${suffix}`,
    refreshTokenExpiresAt: '2026-10-12T12:00:00Z',
    user
  }
}

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
}

function installState() {
  const states = new Map<string, ReturnType<typeof ref>>()
  vi.stubGlobal('useState', (key: string, init: () => unknown) => {
    if (!states.has(key)) states.set(key, ref(init()))
    return states.get(key)
  })
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
  sessionStorage.clear()
})

describe('centralized auth composable', () => {
  it('refreshes on a 401 and retries with rotated credentials', async () => {
    installState()
    let protectedCalls = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') return json(tokens('one'))
      if (path === '/api/auth/refresh') return json(tokens('two'))
      if (path === '/api/protected') {
        protectedCalls++
        if (protectedCalls === 1) return json({ error: { code: 'authentication_failed' } }, 401)
        return json({ ok: true })
      }
      throw new Error(`unexpected fetch ${path}`)
    }))

    const auth = useAuth()
    await auth.login('admin', 'test-password-value', true)
    expect(auth.isAuthenticated.value).toBe(true)
    expect(auth.credentials.value?.refreshToken).toBe('test-refresh-one')
    expect(await auth.request('/api/protected')).toEqual({ ok: true })
    expect(protectedCalls).toBe(2)
    expect(auth.credentials.value?.accessToken).toBe('test-access-two')
  })

  it('routes account, session and admin calls through the centralized client', async () => {
    installState()
    const settings = {
      accessTokenLifetimeSeconds: 3600,
      refreshTokenLifetimeSeconds: 2592000,
      minimumPasswordLength: 12,
      requireUppercase: false,
      requireLowercase: false,
      requireNumber: false,
      requireSymbol: false
    }
    const pending = { ...user, id: '00000000-0000-0000-0000-000000000002', username: 'member', deploymentRole: 'member' as const, status: 'pending' as const }
    const group = { id: '00000000-0000-0000-0000-000000000010', name: 'engineering', createdAt: '2026-09-12T12:00:00Z', updatedAt: '2026-09-12T12:00:00Z' }
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const method = init?.method ?? 'GET'
      if (path === '/api/auth/bootstrap') return json({ available: false })
      if (path === '/api/auth/bootstrap/register') return json(user, 201)
      if (path === '/api/auth/login') return json(tokens('main'))
      if (path === '/api/auth/setup/complete' || path === '/api/auth/reset/complete') return json(user)
      if (path === '/api/auth/me' && method === 'PATCH') return json({ ...user, username: 'updated' })
      if (path === '/api/auth/me/password' && method === 'PUT') return json(user)
      if (path === '/api/auth/me/sessions' && method === 'GET') return json([])
      if (path.startsWith('/api/auth/me/sessions/') && method === 'DELETE') return new Response(null, { status: 204 })
      if (path === '/api/auth/me/sessions/logout-others') return new Response(null, { status: 204 })
      if (path === '/api/auth/users' && method === 'GET') return json([user, pending])
      if (path === '/api/auth/users' && method === 'POST') return json({ user: pending, setupToken: 'test-setup-value', setupTokenExpiresAt: '2026-09-13T12:00:00Z' }, 201)
      if (path.endsWith('/setup-token') || path.endsWith('/reset-token')) return json({ token: 'test-one-time-value', expiresAt: '2026-09-13T12:00:00Z' }, 201)
      if (path.endsWith('/password') && path.startsWith('/api/auth/users/')) return json({ ...pending, status: 'active', forcePasswordChange: true })
      if (path.endsWith('/disable')) return json({ ...pending, status: 'disabled' })
      if (path.endsWith('/enable')) return json({ ...pending, status: 'active' })
      if (path === '/api/auth/settings' && method === 'GET') return json(settings)
      if (path === '/api/auth/settings' && method === 'PUT') return json({ ...settings, minimumPasswordLength: 14 })
      if (path === '/api/groups' && method === 'GET') return json([group])
      if (path === '/api/groups' && method === 'POST') return json(group, 201)
      if (path === `/api/groups/${group.id}` && method === 'PATCH') return json({ ...group, name: 'platform' })
      if (path === `/api/groups/${group.id}` && method === 'DELETE') return new Response(null, { status: 204 })
      if (path === `/api/groups/${group.id}/members` && method === 'GET') return json([pending])
      if (path === `/api/groups/${group.id}/members` && method === 'POST') return new Response(null, { status: 204 })
      if (path === `/api/groups/${group.id}/members/${pending.id}` && method === 'DELETE') return new Response(null, { status: 204 })
      if (path === '/api/auth/logout') return new Response(null, { status: 204 })
      throw new Error(`unexpected fetch ${method} ${path}`)
    }))

    const auth = useAuth()
    expect(await auth.bootstrapAvailable()).toBe(false)
    expect((await auth.register({ username: 'admin', email: 'admin@example.com', displayName: 'Admin', password: 'test-password-value' })).id).toBe(user.id)
    await auth.completePasswordToken('setup', 'test-value', 'test-password-value')
    await auth.completePasswordToken('reset', 'test-value', 'test-password-value')
    await auth.login('admin', 'test-password-value', false)
    expect((await auth.updateProfile({ username: 'updated', email: user.email, displayName: user.displayName })).username).toBe('updated')
    expect(await auth.sessions()).toEqual([])
    await auth.revokeSession('00000000-0000-0000-0000-000000000123')
    await auth.logoutOthers()
    expect((await auth.users()).length).toBe(2)
    expect((await auth.createUser({ username: 'member', email: 'member@example.com', displayName: 'Member' })).setupToken).toBe('test-setup-value')
    await auth.passwordToken(pending.id, 'setup')
    await auth.passwordToken(pending.id, 'reset')
    expect((await auth.setUserPassword(pending.id, 'test-password-value')).forcePasswordChange).toBe(true)
    expect((await auth.setUserDisabled(pending.id, true)).status).toBe('disabled')
    expect((await auth.setUserDisabled(pending.id, false)).status).toBe('active')
    expect((await auth.settings()).minimumPasswordLength).toBe(12)
    expect((await auth.updateSettings(settings)).minimumPasswordLength).toBe(14)
    expect((await auth.groups())[0]?.name).toBe('engineering')
    expect((await auth.createGroup('Engineering')).id).toBe(group.id)
    expect((await auth.updateGroup(group.id, 'Platform')).name).toBe('platform')
    expect((await auth.groupMembers(group.id))[0]?.id).toBe(pending.id)
    await auth.addGroupMember(group.id, pending.id)
    await auth.removeGroupMember(group.id, pending.id)
    await auth.deleteGroup(group.id)

    await auth.changePassword('test-password-updated')
    expect(auth.credentials.value).toBeNull()
    await auth.login('admin', 'test-password-updated', false)
    await auth.logout()
    expect(auth.user.value).toBeNull()
  })
})
