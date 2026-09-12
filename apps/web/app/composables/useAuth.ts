import { computed } from 'vue'
import { ApiError, apiRequest } from '../utils/api'
import { clearAuthStorage, readAuthStorage, writeAuthStorage } from '../utils/auth-storage'
import type { AuthSession, AuthSettings, AuthTokens, AuthUser, PasswordTokenResult, PendingUserResult, StoredAuth } from '../types/auth'

type RefreshOutcome = 'success' | 'rejected' | 'retryable' | 'stale'

function storedFrom(tokens: AuthTokens): StoredAuth {
  return {
    accessToken: tokens.accessToken,
    accessTokenExpiresAt: tokens.accessTokenExpiresAt,
    refreshToken: tokens.refreshToken,
    refreshTokenExpiresAt: tokens.refreshTokenExpiresAt
  }
}

export function useAuth() {
  const user = useState<AuthUser | null>('auth-user', () => null)
  const credentials = useState<StoredAuth | null>('auth-credentials', () => null)
  const persistent = useState('auth-persistent', () => false)
  const initialized = useState('auth-initialized', () => false)
  const refreshing = useState<Promise<RefreshOutcome> | null>('auth-refreshing', () => null)
  const generation = useState('auth-generation', () => 0)

  function store(tokens: AuthTokens, remember: boolean) {
    user.value = tokens.user
    credentials.value = storedFrom(tokens)
    persistent.value = remember
    if (import.meta.client) writeAuthStorage(localStorage, sessionStorage, credentials.value, remember)
  }

  function clear() {
    user.value = null
    credentials.value = null
    persistent.value = false
    if (import.meta.client) clearAuthStorage(localStorage, sessionStorage)
  }

  async function revokeRefreshToken(refreshToken: string) {
    try {
      await apiRequest<void>('/api/auth/logout', { method: 'POST', body: { refreshToken } })
    } catch {
      // Rotated credentials discovered after logout are revoked best-effort.
    }
  }

  async function refreshOutcome(): Promise<RefreshOutcome> {
    const refreshToken = credentials.value?.refreshToken
    if (!refreshToken) return 'rejected'
    if (refreshing.value) return refreshing.value
    const refreshGeneration = generation.value
    refreshing.value = (async () => {
      try {
        const tokens = await apiRequest<AuthTokens>('/api/auth/refresh', {
          method: 'POST', body: { refreshToken }
        })
        if (generation.value !== refreshGeneration) {
          await revokeRefreshToken(tokens.refreshToken)
          return 'stale'
        }
        store(tokens, persistent.value)
        return 'success'
      } catch (error) {
        if (generation.value === refreshGeneration && error instanceof ApiError && error.status === 401) {
          clear()
          return 'rejected'
        }
        return 'retryable'
      } finally {
        refreshing.value = null
      }
    })()
    return refreshing.value
  }

  async function refresh() {
    return await refreshOutcome() === 'success'
  }

  async function request<T>(path: string, options: { method?: string; body?: unknown } = {}, retry = true): Promise<T> {
    if (!credentials.value?.accessToken) throw new ApiError(401, 'authentication_failed', 'Sign in to continue.')
    try {
      return await apiRequest<T>(path, {
        ...options,
        headers: { Authorization: `Bearer ${credentials.value.accessToken}` }
      })
    } catch (error) {
      if (retry && error instanceof ApiError && error.status === 401 && await refresh()) {
        return request<T>(path, options, false)
      }
      throw error
    }
  }

  async function initialize() {
    if (initialized.value || import.meta.server) return
    initialized.value = true
    const stored = readAuthStorage(localStorage, sessionStorage)
    credentials.value = stored.credentials
    persistent.value = stored.persistent
    if (!credentials.value) return
    try {
      user.value = await request<AuthUser>('/api/auth/me', {}, false)
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        const outcome = await refreshOutcome()
        if (outcome === 'retryable') initialized.value = false
        return
      }
      initialized.value = false
    }
  }

  async function bootstrapAvailable() {
    return (await apiRequest<{ available: boolean }>('/api/auth/bootstrap')).available
  }

  async function register(input: { username: string; email: string; displayName: string; password: string }) {
    return apiRequest<AuthUser>('/api/auth/bootstrap/register', { method: 'POST', body: input })
  }

  async function login(loginValue: string, password: string, stayLoggedIn: boolean) {
    const tokens = await apiRequest<AuthTokens>('/api/auth/login', { method: 'POST', body: { login: loginValue, password } })
    store(tokens, stayLoggedIn)
    return tokens.user
  }

  async function completePasswordToken(purpose: 'setup' | 'reset', token: string, password: string) {
    return apiRequest<AuthUser>(`/api/auth/${purpose}/complete`, { method: 'POST', body: { token, password } })
  }

  async function logout() {
    generation.value++
    const refreshToken = credentials.value?.refreshToken
    const inFlightRefresh = refreshing.value
    clear()
    let logoutError: unknown
    try {
      if (refreshToken) {
        await apiRequest<void>('/api/auth/logout', { method: 'POST', body: { refreshToken } })
      }
    } catch (error) {
      logoutError = error
    }
    try {
      if (inFlightRefresh) await inFlightRefresh
    } finally {
      clear()
    }
    if (logoutError) throw logoutError
  }

  async function updateProfile(input: { username: string; email: string; displayName: string }) {
    const updated = await request<AuthUser>('/api/auth/me', { method: 'PATCH', body: input })
    user.value = updated
    return updated
  }

  async function changePassword(currentPassword: string | undefined, newPassword: string) {
    const updated = await request<AuthUser>('/api/auth/me/password', {
      method: 'PUT', body: { currentPassword, newPassword }
    })
    clear()
    return updated
  }

  async function sessions() {
    return request<AuthSession[]>('/api/auth/me/sessions')
  }

  async function revokeSession(id: string) {
    return request<void>(`/api/auth/me/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }

  async function logoutOthers() {
    if (!credentials.value?.refreshToken) throw new ApiError(401, 'authentication_failed', 'Sign in to continue.')
    return request<void>('/api/auth/me/sessions/logout-others', { method: 'POST', body: { refreshToken: credentials.value.refreshToken } })
  }

  async function users() { return request<AuthUser[]>('/api/auth/users') }
  async function createUser(input: { username: string; email: string; displayName: string }) {
    return request<PendingUserResult>('/api/auth/users', { method: 'POST', body: input })
  }
  async function passwordToken(userId: string, purpose: 'setup' | 'reset') {
    return request<PasswordTokenResult>(`/api/auth/users/${encodeURIComponent(userId)}/${purpose}-token`, { method: 'POST' })
  }
  async function setUserPassword(userId: string, password: string) {
    return request<AuthUser>(`/api/auth/users/${encodeURIComponent(userId)}/password`, { method: 'PUT', body: { password } })
  }
  async function setUserDisabled(userId: string, disabled: boolean) {
    return request<AuthUser>(`/api/auth/users/${encodeURIComponent(userId)}/${disabled ? 'disable' : 'enable'}`, { method: 'POST' })
  }
  async function settings() { return request<AuthSettings>('/api/auth/settings') }
  async function updateSettings(value: AuthSettings) {
    return request<AuthSettings>('/api/auth/settings', { method: 'PUT', body: value })
  }

  return {
    user, credentials, persistent, initialized,
    isAuthenticated: computed(() => Boolean(user.value && credentials.value)),
    isAdmin: computed(() => user.value?.deploymentRole === 'admin'),
    initialize, bootstrapAvailable, register, login, logout, completePasswordToken,
    request, refresh, updateProfile, changePassword, sessions, revokeSession, logoutOthers,
    users, createUser, passwordToken, setUserPassword, setUserDisabled, settings, updateSettings
  }
}
