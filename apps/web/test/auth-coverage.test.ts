import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../app/utils/api'
import { AUTH_STORAGE_KEY } from '../app/utils/auth-storage'
import { useAuth } from '../app/composables/useAuth'
import AccountPage from '../app/pages/account.vue'
import UsersPage from '../app/pages/settings/users.vue'
import { uiStubs } from './ui-stubs'

const activeUser = {
  id: '00000000-0000-0000-0000-000000000001',
  username: 'admin',
  email: 'admin@example.com',
  displayName: 'Admin',
  deploymentRole: 'admin' as const,
  status: 'active' as const,
  forcePasswordChange: false
}

const memberUser = {
  ...activeUser,
  id: '00000000-0000-0000-0000-000000000002',
  username: 'member',
  email: 'member@example.com',
  displayName: 'Member',
  deploymentRole: 'member' as const
}

const disabledUser = { ...memberUser, id: '00000000-0000-0000-0000-000000000003', username: 'disabled', status: 'disabled' as const }
const pendingUser = { ...memberUser, id: '00000000-0000-0000-0000-000000000004', username: 'pending', status: 'pending' as const }

function authTokens(suffix: string) {
  return {
    accessToken: `test-access-${suffix}`,
    accessTokenExpiresAt: '2026-09-12T14:00:00Z',
    refreshToken: `test-refresh-${suffix}`,
    refreshTokenExpiresAt: '2026-10-12T12:00:00Z',
    user: activeUser
  }
}

function stored(suffix: string) {
  const tokens = authTokens(suffix)
  return {
    accessToken: tokens.accessToken,
    accessTokenExpiresAt: tokens.accessTokenExpiresAt,
    refreshToken: tokens.refreshToken,
    refreshTokenExpiresAt: tokens.refreshTokenExpiresAt
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
  return states
}

const global = { stubs: { ...uiStubs, SettingsShell: { template: '<div data-settings-shell><slot /></div>' } } }

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.resetModules()
  localStorage.clear()
  sessionStorage.clear()
})

describe('auth composable alternate paths', () => {
  it('rejects protected operations without credentials and clears failed refreshes', async () => {
    installState()
    const auth = useAuth()
    await expect(auth.refresh()).resolves.toBe(false)
    await expect(auth.request('/api/protected')).rejects.toBeInstanceOf(ApiError)
    await expect(auth.logoutOthers()).rejects.toBeInstanceOf(ApiError)
    await expect(auth.logout()).resolves.toBeUndefined()

    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') return json(authTokens('one'))
      if (path === '/api/auth/refresh') return json({ error: { code: 'authentication_failed', message: 'expired' } }, 401)
      throw new Error(`unexpected fetch ${path}`)
    }))
    await auth.login('admin', 'test-password-value', true)
    await expect(auth.refresh()).resolves.toBe(false)
    expect(auth.credentials.value).toBeNull()
  })

  it('does not retry non-401 failures and shares an in-flight refresh', async () => {
    installState()
    let releaseRefresh!: () => void
    let refreshCalls = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') return json(authTokens('one'))
      if (path === '/api/failure') return json({ error: { code: 'server_error', message: 'failed' } }, 500)
      if (path === '/api/auth/refresh') {
        refreshCalls++
        await new Promise<void>((resolve) => { releaseRefresh = resolve })
        return json(authTokens('two'))
      }
      throw new Error(`unexpected fetch ${path}`)
    }))
    const auth = useAuth()
    await auth.login('admin', 'test-password-value', false)
    await expect(auth.request('/api/failure')).rejects.toMatchObject({ status: 500 })

    const first = auth.refresh()
    await Promise.resolve()
    const second = auth.refresh()
    releaseRefresh()
    await expect(first).resolves.toBe(true)
    await expect(second).resolves.toBe(true)
    expect(refreshCalls).toBe(1)
    expect(auth.credentials.value?.accessToken).toBe('test-access-two')
  })

  it('initializes stored credentials, refreshes 401s and clears non-auth failures', async () => {
    localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(stored('stored')))
    installState()
    const successfulFetch = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === '/api/auth/me') return json(activeUser)
      throw new Error(`unexpected fetch ${String(input)}`)
    })
    vi.stubGlobal('fetch', successfulFetch)
    const auth = useAuth()
    await auth.initialize()
    await auth.initialize()
    expect(auth.user.value?.id).toBe(activeUser.id)
    expect(auth.persistent.value).toBe(true)
    expect(successfulFetch).toHaveBeenCalledTimes(1)

    vi.unstubAllGlobals()
    sessionStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(stored('temporary')))
    localStorage.clear()
    installState()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/me') return json({ error: { code: 'authentication_failed' } }, 401)
      if (path === '/api/auth/refresh') return json(authTokens('rotated'))
      throw new Error(`unexpected fetch ${path}`)
    }))
    const refreshed = useAuth()
    await refreshed.initialize()
    expect(refreshed.user.value?.id).toBe(activeUser.id)
    expect(refreshed.persistent.value).toBe(false)
    expect(refreshed.credentials.value?.accessToken).toBe('test-access-rotated')

    vi.unstubAllGlobals()
    localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(stored('bad')))
    sessionStorage.clear()
    installState()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === '/api/auth/me') return json({ error: { code: 'server_error', message: 'failed' } }, 500)
      throw new Error(`unexpected fetch ${String(input)}`)
    }))
    const failed = useAuth()
    await failed.initialize()
    expect(failed.credentials.value).toBeNull()
    expect(failed.user.value).toBeNull()
  })

  it('clears local auth even when server logout fails', async () => {
    installState()
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/auth/login') return json(authTokens('one'))
      if (path === '/api/auth/logout') return json({ error: { code: 'server_error', message: 'failed' } }, 500)
      throw new Error(`unexpected fetch ${path}`)
    }))
    const auth = useAuth()
    await auth.login('admin', 'test-password-value', true)
    await expect(auth.logout()).rejects.toMatchObject({ status: 500 })
    expect(auth.user.value).toBeNull()
    expect(auth.credentials.value).toBeNull()
  })
})

describe('account alternate paths', () => {
  it('manages profile and session actions and surfaces request failures', async () => {
    const sessions = vi.fn(async () => [{
      id: '00000000-0000-0000-0000-000000000111',
      createdAt: '2026-09-12T12:00:00Z',
      expiresAt: '2026-10-12T12:00:00Z'
    }])
    const updateProfile = vi.fn()
      .mockRejectedValueOnce(new Error('profile failed'))
      .mockRejectedValueOnce('non-error failure')
      .mockResolvedValue(activeUser)
    const revokeSession = vi.fn(async () => undefined)
    const logoutOthers = vi.fn(async () => undefined)
    const logout = vi.fn(async () => undefined)
    vi.stubGlobal('useAuth', () => ({
      user: ref(activeUser), sessions, updateProfile,
      changePassword: vi.fn(async () => activeUser), revokeSession, logoutOthers, logout
    }))
    const navigateTo = vi.fn(async () => undefined)
    vi.stubGlobal('navigateTo', navigateTo)

    const wrapper = mount(AccountPage, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('Active sessions')
    expect(wrapper.text()).toContain('Session 00000000')

    const profileForm = wrapper.findAll('form')[0]!
    await profileForm.trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('profile failed')
    await profileForm.trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('The request failed.')
    await profileForm.trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Profile updated.')

    const revoke = wrapper.findAll('button').find(button => button.text().includes('Revoke'))
    await revoke!.trigger('click')
    await flushPromises()
    expect(revokeSession).toHaveBeenCalledWith('00000000-0000-0000-0000-000000000111')
    expect(wrapper.text()).toContain('Session revoked.')

    const others = wrapper.findAll('button').find(button => button.text().includes('Log out all other sessions'))
    await others!.trigger('click')
    await flushPromises()
    expect(logoutOthers).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Other sessions logged out.')

    const logoutButton = wrapper.findAll('button').find(button => button.text() === 'Log out')
    await logoutButton!.trigger('click')
    await flushPromises()
    expect(logout).toHaveBeenCalled()
    expect(navigateTo).toHaveBeenCalledWith('/auth/login')
  })
})

describe('user administration alternate paths', () => {
  it('handles setup/reset, direct password and both status transitions', async () => {
    const users = vi.fn(async () => [pendingUser, memberUser, disabledUser])
    const passwordToken = vi.fn(async (_id: string, purpose: 'setup' | 'reset') => ({
      token: `test-${purpose}-token`, expiresAt: '2026-09-13T12:00:00Z'
    }))
    const setUserDisabled = vi.fn(async (_id: string, disabled: boolean) => disabled ? disabledUser : memberUser)
    const setUserPassword = vi.fn(async () => ({ ...memberUser, forcePasswordChange: true }))
    vi.stubGlobal('useAuth', () => ({
      users,
      createUser: vi.fn(async () => ({ user: pendingUser, setupToken: 'created-token', setupTokenExpiresAt: '2026-09-13T12:00:00Z' })),
      passwordToken, setUserDisabled, setUserPassword
    }))

    const wrapper = mount(UsersPage, { global })
    await flushPromises()
    const button = (text: string) => wrapper.findAll('button').find(item => item.text() === text)!
    const buttons = (text: string) => wrapper.findAll('button').filter(item => item.text() === text)

    await button('New setup token').trigger('click')
    await flushPromises()
    expect(passwordToken).toHaveBeenCalledWith(pendingUser.id, 'setup')
    expect(wrapper.text()).toContain('Setup token for pending')

    await button('Reset token').trigger('click')
    await flushPromises()
    expect(passwordToken).toHaveBeenCalledWith(memberUser.id, 'reset')
    expect(wrapper.text()).toContain('Reset token for member')

    await buttons('Disable')[1]!.trigger('click')
    await flushPromises()
    expect(setUserDisabled).toHaveBeenCalledWith(memberUser.id, true)
    await button('Re-enable').trigger('click')
    await flushPromises()
    expect(setUserDisabled).toHaveBeenCalledWith(disabledUser.id, false)

    await buttons('Set password')[1]!.trigger('click')
    await wrapper.get('input[type="password"]').setValue('test-password-value')
    const passwordForm = wrapper.findAll('form').find(form => form.text().includes('Assign and require change'))!
    await passwordForm.trigger('submit')
    await flushPromises()
    expect(setUserPassword).toHaveBeenCalledWith(memberUser.id, 'test-password-value')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)

    await buttons('Set password')[1]!.trigger('click')
    await flushPromises()
    await button('Cancel').trigger('click')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
  })

  it('shows admin action errors from ordinary Error objects', async () => {
    vi.stubGlobal('useAuth', () => ({
      users: vi.fn(async () => [memberUser]),
      createUser: vi.fn(), passwordToken: vi.fn(), setUserPassword: vi.fn(),
      setUserDisabled: vi.fn(async () => { throw new Error('disable failed') })
    }))
    const wrapper = mount(UsersPage, { global })
    await flushPromises()
    const disable = wrapper.findAll('button').find(button => button.text() === 'Disable')!
    await disable.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('disable failed')
  })
})

describe('global auth middleware', () => {
  it('initializes auth and applies redirect and pass-through policy', async () => {
    const initialize = vi.fn(async () => undefined)
    const navigateTo = vi.fn(async () => undefined)
    vi.stubGlobal('defineNuxtRouteMiddleware', (handler: unknown) => handler)
    vi.stubGlobal('navigateTo', navigateTo)
    vi.stubGlobal('useAuth', () => ({
      initialize,
      isAuthenticated: ref(false),
      isAdmin: ref(false),
      user: ref(null)
    }))
    const { default: middleware } = await import('../app/middleware/auth.global')

    await middleware({ path: '/projects' } as never, {} as never)
    expect(initialize).toHaveBeenCalled()
    expect(navigateTo).toHaveBeenCalledWith('/auth/login')

    navigateTo.mockClear()
    vi.stubGlobal('useAuth', () => ({
      initialize,
      isAuthenticated: ref(true),
      isAdmin: ref(true),
      user: ref(activeUser)
    }))
    await middleware({ path: '/projects' } as never, {} as never)
    expect(navigateTo).not.toHaveBeenCalled()

    vi.stubGlobal('useAuth', () => ({
      initialize,
      isAuthenticated: ref(true),
      isAdmin: ref(false),
      user: ref({ ...memberUser, forcePasswordChange: true })
    }))
    await middleware({ path: '/settings' } as never, {} as never)
    expect(navigateTo).toHaveBeenCalledWith('/account')
  })
})
