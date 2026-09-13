import { ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useAuth } from '../app/composables/useAuth'
import { AUTH_STORAGE_KEY } from '../app/utils/auth-storage'

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

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
  sessionStorage.clear()
})

describe('auth generation concurrency regressions', () => {
  it('keeps auth cleared when an older refresh resolves after password change', async () => {
    installState()
    const refreshResponse = deferred<Response>()
    const refreshStarted = deferred<void>()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') return json(tokens('old'))
      if (path === '/api/auth/refresh') {
        refreshStarted.resolve()
        return refreshResponse.promise
      }
      if (path === '/api/auth/me/password') return json(user)
      if (path === '/api/auth/logout') return new Response(null, { status: 204 })
      throw new Error(`unexpected fetch ${path}`)
    }))

    const auth = useAuth()
    await auth.login('admin', 'long-enough-password', true)
    const refresh = auth.refresh()
    await refreshStarted.promise

    await auth.changePassword('long-enough-password', 'replacement-long-password')
    expect(auth.user.value).toBeNull()
    expect(auth.credentials.value).toBeNull()
    expect(auth.isAuthenticated.value).toBe(false)
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull()
    expect(sessionStorage.getItem(AUTH_STORAGE_KEY)).toBeNull()

    refreshResponse.resolve(json(tokens('old-rotated')))
    expect(await refresh).toBe(false)
    expect(auth.user.value).toBeNull()
    expect(auth.credentials.value).toBeNull()
    expect(auth.isAuthenticated.value).toBe(false)
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull()
    expect(sessionStorage.getItem(AUTH_STORAGE_KEY)).toBeNull()
  })

  it('keeps a newer explicit login authoritative over an older refresh', async () => {
    installState()
    const refreshResponse = deferred<Response>()
    const refreshStarted = deferred<void>()
    let loginCount = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') {
        loginCount++
        return json(tokens(loginCount === 1 ? 'old' : 'new'))
      }
      if (path === '/api/auth/refresh') {
        refreshStarted.resolve()
        return refreshResponse.promise
      }
      if (path === '/api/auth/logout') return new Response(null, { status: 204 })
      throw new Error(`unexpected fetch ${path}`)
    }))

    const auth = useAuth()
    await auth.login('admin', 'old-password-value', true)
    const refresh = auth.refresh()
    await refreshStarted.promise

    await auth.login('admin', 'new-password-value', true)
    expect(auth.credentials.value?.accessToken).toBe('test-access-new')
    expect(auth.credentials.value?.refreshToken).toBe('test-refresh-new')

    refreshResponse.resolve(json(tokens('old-rotated')))
    expect(await refresh).toBe(false)
    expect(auth.credentials.value?.accessToken).toBe('test-access-new')
    expect(auth.credentials.value?.refreshToken).toBe('test-refresh-new')
    expect(auth.isAuthenticated.value).toBe(true)
  })
})
