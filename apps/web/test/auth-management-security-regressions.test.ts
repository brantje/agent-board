import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useAuth } from '../app/composables/useAuth'
import UsersPage from '../app/pages/settings/users.vue'
import AuthenticationPage from '../app/pages/settings/authentication.vue'
import { activeAuthUser, authTokens, installAuthState, jsonResponse } from './auth-test-helpers'
import { uiStubs } from './ui-stubs'

const pendingUser = {
  ...activeAuthUser,
  id: '00000000-0000-0000-0000-000000000002',
  username: 'member',
  email: 'member@example.com',
  displayName: 'Member',
  deploymentRole: 'member' as const,
  status: 'pending' as const
}

const pageGlobal = {
  stubs: {
    ...uiStubs,
    SettingsShell: { template: '<div data-settings-shell><slot /></div>' }
  }
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
  sessionStorage.clear()
})

describe('authentication management security regressions', () => {
  it('offers pending-user password assignment and disable actions', async () => {
    vi.stubGlobal('useAuth', () => ({
      users: vi.fn(async () => [pendingUser]),
      createUser: vi.fn(),
      passwordToken: vi.fn(),
      setUserDisabled: vi.fn(),
      setUserPassword: vi.fn()
    }))

    const wrapper = mount(UsersPage, { global: pageGlobal })
    await flushPromises()

    expect(wrapper.text()).toContain('New setup token')
    expect(wrapper.text()).toContain('Set password')
    expect(wrapper.text()).toContain('Disable')
    expect(wrapper.text()).not.toContain('Reset token')
    expect(wrapper.text()).not.toContain('Re-enable')
  })

  it('does not submit fabricated authentication defaults after settings load fails', async () => {
    const updateSettings = vi.fn()
    vi.stubGlobal('useAuth', () => ({
      settings: vi.fn(async () => { throw new Error('settings load failed') }),
      updateSettings
    }))

    const wrapper = mount(AuthenticationPage, { global: pageGlobal })
    await flushPromises()
    expect(wrapper.text()).toContain('settings load failed')
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.text()).toContain('Retry')
    expect(updateSettings).not.toHaveBeenCalled()
  })

  it('keeps logout authoritative over an in-flight refresh and revokes the rotated token', async () => {
    installAuthState()
    let resolveRefresh!: (response: Response) => void
    let markRefreshStarted!: () => void
    const refreshStarted = new Promise<void>((resolve) => { markRefreshStarted = resolve })
    const refreshResponse = new Promise<Response>((resolve) => { resolveRefresh = resolve })
    const revokedRefreshTokens: string[] = []

    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path === '/api/auth/login') return jsonResponse(authTokens('one'))
      if (path === '/api/auth/refresh') {
        markRefreshStarted()
        return refreshResponse
      }
      if (path === '/api/auth/logout') {
        const body = JSON.parse(String(init?.body ?? '{}')) as { refreshToken?: string }
        if (body.refreshToken) revokedRefreshTokens.push(body.refreshToken)
        return new Response(null, { status: 204 })
      }
      throw new Error(`unexpected fetch ${path}`)
    }))

    const auth = useAuth()
    await auth.login('admin', 'test-password-value', true)
    expect(auth.credentials.value?.refreshToken).toBe('test-refresh-one')

    const refreshPromise = auth.refresh()
    await refreshStarted
    const logoutPromise = auth.logout()
    resolveRefresh(jsonResponse(authTokens('two')))

    await refreshPromise
    await logoutPromise
    await flushPromises()

    expect(revokedRefreshTokens).toContain('test-refresh-one')
    expect(revokedRefreshTokens).toContain('test-refresh-two')
    expect(auth.user.value).toBeNull()
    expect(auth.credentials.value).toBeNull()
    expect(auth.isAuthenticated.value).toBe(false)
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })
})
