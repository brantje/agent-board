import { afterEach, describe, expect, it, vi } from 'vitest'
import { useAuth } from '../app/composables/useAuth'
import { AUTH_STORAGE_KEY } from '../app/utils/auth-storage'
import { activeAuthUser, authTokens, installAuthState, jsonResponse } from './auth-test-helpers'

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
    installAuthState()
    const refreshResponse = deferred<Response>()
    const refreshStarted = deferred<void>()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') return jsonResponse(authTokens('old'))
      if (path === '/api/auth/refresh') {
        refreshStarted.resolve()
        return refreshResponse.promise
      }
      if (path === '/api/auth/me/password') return jsonResponse(activeAuthUser)
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

    refreshResponse.resolve(jsonResponse(authTokens('old-rotated')))
    expect(await refresh).toBe(false)
    expect(auth.user.value).toBeNull()
    expect(auth.credentials.value).toBeNull()
    expect(auth.isAuthenticated.value).toBe(false)
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull()
    expect(sessionStorage.getItem(AUTH_STORAGE_KEY)).toBeNull()
  })

  it('keeps a newer explicit login authoritative over an older refresh', async () => {
    installAuthState()
    const refreshResponse = deferred<Response>()
    const refreshStarted = deferred<void>()
    let loginCount = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') {
        loginCount++
        return jsonResponse(authTokens(loginCount === 1 ? 'old' : 'new'))
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

    refreshResponse.resolve(jsonResponse(authTokens('old-rotated')))
    expect(await refresh).toBe(false)
    expect(auth.credentials.value?.accessToken).toBe('test-access-new')
    expect(auth.credentials.value?.refreshToken).toBe('test-refresh-new')
    expect(auth.isAuthenticated.value).toBe(true)
  })
})
