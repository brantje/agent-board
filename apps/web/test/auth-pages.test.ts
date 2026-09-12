import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AuthPage from '../app/pages/auth/[mode].vue'
import AccountPage from '../app/pages/account.vue'
import UsersPage from '../app/pages/settings/users.vue'
import AuthenticationPage from '../app/pages/settings/authentication.vue'
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
const pendingUser = {
  ...activeUser,
  id: '00000000-0000-0000-0000-000000000002',
  username: 'member',
  email: 'member@example.com',
  displayName: 'Member',
  deploymentRole: 'member' as const,
  status: 'pending' as const
}
const settings = {
  accessTokenLifetimeSeconds: 3600,
  refreshTokenLifetimeSeconds: 2592000,
  minimumPasswordLength: 12,
  requireUppercase: false,
  requireLowercase: false,
  requireNumber: false,
  requireSymbol: false
}
const global = { stubs: { ...uiStubs, SettingsShell: { template: '<div data-settings-shell><slot /></div>' } } }

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('authentication pages', () => {
  it('submits login with stay-logged-in through the centralized auth client', async () => {
    const login = vi.fn(async () => activeUser)
    vi.stubGlobal('useRoute', () => ({ params: { mode: 'login' }, query: {} }))
    vi.stubGlobal('useAuth', () => ({ bootstrapAvailable: vi.fn(async () => false), login }))
    const navigateTo = vi.fn(async () => undefined)
    vi.stubGlobal('navigateTo', navigateTo)

    const wrapper = mount(AuthPage, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('Sign in')
    expect(wrapper.text()).toContain('Stay logged in')
    const inputs = wrapper.findAll('input')
    await inputs[0]!.setValue('admin@example.com')
    await inputs[1]!.setValue('test-password-value')
    await inputs[2]!.setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(login).toHaveBeenCalledWith('admin@example.com', 'test-password-value', true)
    expect(navigateTo).toHaveBeenCalledWith('/account')
  })

  it('submits bootstrap registration through Nuxt UI AuthForm fields', async () => {
    const register = vi.fn(async () => activeUser)
    vi.stubGlobal('useRoute', () => ({ params: { mode: 'register' }, query: {} }))
    vi.stubGlobal('useAuth', () => ({ register }))
    const navigateTo = vi.fn(async () => undefined)
    vi.stubGlobal('navigateTo', navigateTo)

    const wrapper = mount(AuthPage, { global })
    expect(wrapper.text()).toContain('Create first administrator')
    const inputs = wrapper.findAll('input')
    await inputs[0]!.setValue('Administrator')
    await inputs[1]!.setValue('admin')
    await inputs[2]!.setValue('admin@example.com')
    await inputs[3]!.setValue('test-password-value')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(register).toHaveBeenCalledWith({
      displayName: 'Administrator',
      username: 'admin',
      email: 'admin@example.com',
      password: 'test-password-value'
    })
    expect(navigateTo).toHaveBeenCalledWith('/auth/login')
  })

  it('clears setup password plaintext after consuming a one-time token', async () => {
    const completePasswordToken = vi.fn(async () => activeUser)
    vi.stubGlobal('useRoute', () => ({ params: { mode: 'setup' }, query: { token: 'test-one-time-value' } }))
    vi.stubGlobal('useAuth', () => ({ completePasswordToken }))
    const navigateTo = vi.fn(async () => undefined)
    vi.stubGlobal('navigateTo', navigateTo)

    const wrapper = mount(AuthPage, { global })
    const password = wrapper.get('input[type="password"]')
    await password.setValue('test-password-value')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(completePasswordToken).toHaveBeenCalledWith('setup', 'test-one-time-value', 'test-password-value')
    expect((wrapper.get('input[type="password"]').element as HTMLInputElement).value).toBe('')
    expect(navigateTo).toHaveBeenCalledWith('/auth/login')
  })
})

describe('account and deployment admin pages', () => {
  it('blocks profile/session UI during forced password change and clears the submitted password', async () => {
    const changePassword = vi.fn(async () => ({ ...activeUser, forcePasswordChange: false }))
    const sessions = vi.fn(async () => [])
    vi.stubGlobal('useAuth', () => ({
      user: ref({ ...activeUser, forcePasswordChange: true }),
      sessions,
      changePassword,
      updateProfile: vi.fn(), revokeSession: vi.fn(), logoutOthers: vi.fn(), logout: vi.fn()
    }))
    const navigateTo = vi.fn(async () => undefined)
    vi.stubGlobal('navigateTo', navigateTo)

    const wrapper = mount(AccountPage, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('Password change required')
    expect(wrapper.text()).not.toContain('Active sessions')
    expect(wrapper.text()).not.toContain('Save profile')
    expect(sessions).not.toHaveBeenCalled()

    await wrapper.get('input[type="password"]').setValue('test-password-updated')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(changePassword).toHaveBeenCalledWith('test-password-updated')
    expect((wrapper.get('input[type="password"]').element as HTMLInputElement).value).toBe('')
    expect(navigateTo).toHaveBeenCalledWith('/auth/login')
  })

  it('shows one-time user setup material only until dismissed and keeps pending users on setup-only actions', async () => {
    const users = vi.fn(async () => [pendingUser])
    const createUser = vi.fn(async () => ({
      user: pendingUser,
      setupToken: 'test-setup-display-value',
      setupTokenExpiresAt: '2026-09-13T12:00:00Z'
    }))
    vi.stubGlobal('useAuth', () => ({
      users, createUser,
      passwordToken: vi.fn(), setUserDisabled: vi.fn(), setUserPassword: vi.fn()
    }))

    const wrapper = mount(UsersPage, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('New setup token')
    expect(wrapper.text()).not.toContain('Reset token')
    expect(wrapper.text()).not.toContain('Set password')
    expect(wrapper.text()).not.toContain('Disable')
    expect(wrapper.text()).not.toContain('Re-enable')

    const inputs = wrapper.findAll('input').slice(0, 3)
    await inputs[0]!.setValue('Member')
    await inputs[1]!.setValue('member')
    await inputs[2]!.setValue('member@example.com')
    await wrapper.findAll('form')[0]!.trigger('submit')
    await flushPromises()
    expect(wrapper.findAll('input').some(input => (input.element as HTMLInputElement).value === 'test-setup-display-value')).toBe(true)

    const dismiss = wrapper.findAll('button').find(button => button.text().includes('Dismiss'))
    expect(dismiss).toBeDefined()
    await dismiss!.trigger('click')
    expect(wrapper.findAll('input').some(input => (input.element as HTMLInputElement).value === 'test-setup-display-value')).toBe(false)
  })

  it('loads and saves the authoritative authentication settings', async () => {
    const updateSettings = vi.fn(async (value: typeof settings) => ({ ...value, minimumPasswordLength: 14 }))
    vi.stubGlobal('useAuth', () => ({ settings: vi.fn(async () => settings), updateSettings }))
    const wrapper = mount(AuthenticationPage, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('Authentication / Security')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
      accessTokenLifetimeSeconds: 3600,
      refreshTokenLifetimeSeconds: 2592000,
      minimumPasswordLength: 12
    }))
    expect(wrapper.text()).toContain('Authentication settings saved.')
  })
})
