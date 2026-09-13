import { ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useAuth } from '../app/composables/useAuth'
import { AUTH_STORAGE_KEY } from '../app/utils/auth-storage'

const activeUser = {
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
    user: activeUser
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

function seedPersistentCredentials() {
  const value = tokens('stored')
  localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify({
    accessToken: value.accessToken,
    accessTokenExpiresAt: value.accessTokenExpiresAt,
    refreshToken: value.refreshToken,
    refreshTokenExpiresAt: value.refreshTokenExpiresAt
  }))
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
  sessionStorage.clear()
})

describe('authentication management auth rereview regressions', () => {
  it('sends currentPassword and newPassword for ordinary self password changes', async () => {
    installState()
    let passwordBody: unknown
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/auth/login') return json(tokens('login'))
      if (path === '/api/auth/me/password') {
        passwordBody = JSON.parse(String(init?.body ?? '{}'))
        return json(activeUser)
      }
      throw new Error(`unexpected fetch ${path}`)
    }))

    const auth = useAuth()
    await auth.login('admin', 'long-enough-password', true)
    const changePassword = auth.changePassword as unknown as (currentPassword: string, newPassword: string) => Promise<unknown>
    await changePassword('long-enough-password', 'replacement-long-password')

    expect(passwordBody).toEqual({
      currentPassword: 'long-enough-password',
      newPassword: 'replacement-long-password'
    })
  })

  it.each([
    ['network failure', () => Promise.reject(new Error('offline'))],
    ['HTTP 5xx', () => Promise.resolve(json({ error: { code: 'temporary' } }, 503))]
  ])('preserves stored credentials and retryability after /me %s', async (_name, response) => {
    installState()
    seedPersistentCredentials()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === '/api/auth/me') return response()
      throw new Error(`unexpected fetch ${String(input)}`)
    }))

    const auth = useAuth()
    await auth.initialize()

    expect(auth.credentials.value?.refreshToken).toBe('test-refresh-stored')
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).not.toBeNull()
    expect(auth.initialized.value).toBe(false)
  })

  it.each([
    ['network failure', () => Promise.reject(new Error('offline'))],
    ['HTTP 5xx', () => Promise.resolve(json({ error: { code: 'temporary' } }, 503))]
  ])('preserves credentials after refresh %s', async (_name, response) => {
    installState()
    seedPersistentCredentials()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/me') return json(activeUser)
      if (path === '/api/auth/refresh') return response()
      throw new Error(`unexpected fetch ${path}`)
    }))

    const auth = useAuth()
    await auth.initialize()
    expect(auth.persistent.value).toBe(true)
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).not.toBeNull()

    expect(await auth.refresh()).toBe(false)
    expect(auth.credentials.value?.refreshToken).toBe('test-refresh-stored')
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).not.toBeNull()
  })

  it('clears credentials after refresh is definitively rejected with 401', async () => {
    installState()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') return json(tokens('one'))
      if (path === '/api/auth/refresh') return json({ error: { code: 'authentication_failed' } }, 401)
      throw new Error(`unexpected fetch ${path}`)
    }))

    const auth = useAuth()
    await auth.login('admin', 'long-enough-password', true)
    expect(await auth.refresh()).toBe(false)
    expect(auth.credentials.value).toBeNull()
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull()
  })

  it('retries initialization successfully after a transient failure', async () => {
    installState()
    seedPersistentCredentials()
    let attempts = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) !== '/api/auth/me') throw new Error(`unexpected fetch ${String(input)}`)
      attempts++
      if (attempts === 1) throw new Error('temporary network failure')
      return json(activeUser)
    }))

    const auth = useAuth()
    await auth.initialize()
    expect(auth.initialized.value).toBe(false)
    expect(auth.credentials.value?.refreshToken).toBe('test-refresh-stored')

    await auth.initialize()
    expect(auth.initialized.value).toBe(true)
    expect(auth.user.value?.id).toBe(activeUser.id)
    expect(attempts).toBe(2)
  })
})
