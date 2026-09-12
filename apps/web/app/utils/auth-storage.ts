import type { StoredAuth } from '../types/auth'

export const AUTH_STORAGE_KEY = 'agent-board.auth'

export type StorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

function parseStored(value: string | null): StoredAuth | null {
  if (!value) return null
  try {
    const parsed = JSON.parse(value) as Partial<StoredAuth>
    if (!parsed.accessToken || !parsed.refreshToken || !parsed.accessTokenExpiresAt || !parsed.refreshTokenExpiresAt) return null
    return parsed as StoredAuth
  } catch {
    return null
  }
}

export function readAuthStorage(local: StorageLike, session: StorageLike) {
  const persistent = parseStored(local.getItem(AUTH_STORAGE_KEY))
  if (persistent) return { credentials: persistent, persistent: true }
  const temporary = parseStored(session.getItem(AUTH_STORAGE_KEY))
  if (temporary) return { credentials: temporary, persistent: false }
  return { credentials: null, persistent: false }
}

export function writeAuthStorage(local: StorageLike, session: StorageLike, credentials: StoredAuth, persistent: boolean) {
  local.removeItem(AUTH_STORAGE_KEY)
  session.removeItem(AUTH_STORAGE_KEY)
  const target = persistent ? local : session
  target.setItem(AUTH_STORAGE_KEY, JSON.stringify(credentials))
}

export function clearAuthStorage(local: StorageLike, session: StorageLike) {
  local.removeItem(AUTH_STORAGE_KEY)
  session.removeItem(AUTH_STORAGE_KEY)
}
