import { afterEach, describe, expect, it, vi } from 'vitest'
import { useAuth } from '../app/composables/useAuth'
import { AUTH_STORAGE_KEY } from '../app/utils/auth-storage'
import { activeAuthUser, authTokens, installAuthState, jsonResponse, seedPersistentAuth } from './auth-test-helpers'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
  sessionStorage.clear()
})

describe('authentication recovery regressions', () => {
  it('sends currentPassword and newPassword for ordinary self password changes', async () => {
    installAuthState()
    let passwordBody: unknown
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/auth/login') return jsonResponse(authTokens('login'))
      if (path === '/api/auth/me/password') {
        passwordBody = JSON.parse(String(init?.body ?? '{}'))
        return jsonResponse(activeAuthUser)
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
    ['HTTP 5xx', () => Promise.resolve(jsonResponse({ error: { code: 'temporary' } }, 503))]
  ])('preserves stored credentials and retryability after /me %s', async (_name, response) => {
    installAuthState()
    seedPersistentAuth()
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
    ['HTTP 5xx', () => Promise.resolve(jsonResponse({ error: { code: 'temporary' } }, 503))]
  ])('preserves credentials after refresh %s', async (_name, response) => {
    installAuthState()
    seedPersistentAuth()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/me') return jsonResponse(activeAuthUser)
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
    installAuthState()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') return jsonResponse(authTokens('one'))
      if (path === '/api/auth/refresh') return jsonResponse({ error: { code: 'authentication_failed' } }, 401)
      throw new Error(`unexpected fetch ${path}`)
    }))

    const auth = useAuth()
    await auth.login('admin', 'long-enough-password', true)
    expect(await auth.refresh()).toBe(false)
    expect(auth.credentials.value).toBeNull()
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull()
  })

  it('retries initialization successfully after a transient failure', async () => {
    installAuthState()
    seedPersistentAuth()
    let attempts = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) !== '/api/auth/me') throw new Error(`unexpected fetch ${String(input)}`)
      attempts++
      if (attempts === 1) throw new Error('temporary network failure')
      return jsonResponse(activeAuthUser)
    }))

    const auth = useAuth()
    await auth.initialize()
    expect(auth.initialized.value).toBe(false)
    expect(auth.credentials.value?.refreshToken).toBe('test-refresh-stored')

    await auth.initialize()
    expect(auth.initialized.value).toBe(true)
    expect(auth.user.value?.id).toBe(activeAuthUser.id)
    expect(attempts).toBe(2)
  })
})
