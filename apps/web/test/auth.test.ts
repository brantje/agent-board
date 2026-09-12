import { describe, expect, it } from 'vitest'
import { AUTH_STORAGE_KEY, clearAuthStorage, readAuthStorage, writeAuthStorage, type StorageLike } from '../app/utils/auth-storage'
import { authRedirect } from '../app/utils/auth-route'
import type { StoredAuth } from '../app/types/auth'

class MemoryStorage implements StorageLike {
  values = new Map<string, string>()
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

const credentials: StoredAuth = {
  accessToken: 'access-secret',
  accessTokenExpiresAt: '2026-09-12T13:00:00Z',
  refreshToken: 'refresh-secret',
  refreshTokenExpiresAt: '2026-10-12T12:00:00Z'
}

describe('auth credential persistence', () => {
  it('keeps normal login credentials in browser-session storage only', () => {
    const local = new MemoryStorage()
    const session = new MemoryStorage()
    writeAuthStorage(local, session, credentials, false)
    expect(local.getItem(AUTH_STORAGE_KEY)).toBeNull()
    expect(readAuthStorage(local, session)).toEqual({ credentials, persistent: false })
  })

  it('persists both access and refresh credentials only for stay logged in', () => {
    const local = new MemoryStorage()
    const session = new MemoryStorage()
    writeAuthStorage(local, session, credentials, true)
    expect(session.getItem(AUTH_STORAGE_KEY)).toBeNull()
    expect(readAuthStorage(local, session)).toEqual({ credentials, persistent: true })
    const raw = local.getItem(AUTH_STORAGE_KEY) ?? ''
    expect(raw).toContain('access-secret')
    expect(raw).toContain('refresh-secret')
    clearAuthStorage(local, session)
    expect(readAuthStorage(local, session).credentials).toBeNull()
  })

  it('ignores malformed or incomplete stored credentials', () => {
    const local = new MemoryStorage()
    const session = new MemoryStorage()
    local.setItem(AUTH_STORAGE_KEY, '{not-json')
    session.setItem(AUTH_STORAGE_KEY, JSON.stringify({ accessToken: 'only-one-token' }))
    expect(readAuthStorage(local, session).credentials).toBeNull()
  })
})

describe('auth route policy', () => {
  it('blocks normal application flow during forced password change', () => {
    expect(authRedirect('/projects', true, true, false)).toBe('/account/password')
    expect(authRedirect('/account/password', true, true, false)).toBeNull()
  })

  it('requires authentication and keeps admin settings admin-only', () => {
    expect(authRedirect('/projects', false, false, false)).toBe('/login')
    expect(authRedirect('/login', false, false, false)).toBeNull()
    expect(authRedirect('/settings/users', true, false, false)).toBe('/account')
    expect(authRedirect('/settings/users', true, false, true)).toBeNull()
  })
})
