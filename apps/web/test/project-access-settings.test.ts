import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectAccessSettings from '../app/components/ProjectAccessSettings.vue'
import { uiStubs } from './ui-stubs'

const projectId = '11111111-1111-4111-8111-111111111111'
const userId = '22222222-2222-4222-8222-222222222222'
const candidateId = '33333333-3333-4333-8333-333333333333'

const global = { stubs: uiStubs }

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
}

afterEach(() => vi.unstubAllGlobals())

describe('ProjectAccessSettings', () => {
  it('shows effective role and disabled direct users', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/effective-role')) return json({ role: 'admin' })
      if (path.endsWith('/access/users')) return json([{ id: userId, username: 'alice', email: 'alice@example.com', displayName: 'Alice', status: 'disabled', role: 'admin' }])
      if (path.endsWith('/access/groups')) return json([])
      throw new Error(`unexpected ${path}`)
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(ProjectAccessSettings, { props: { projectId }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Your effective role is admin')
    expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).toContain('Disabled')
  })

  it('changes roles and surfaces the final-direct-admin conflict', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path.endsWith('/effective-role')) return json({ role: 'admin' })
      if (path.endsWith('/access/users') && options.method === 'GET') return json([{ id: userId, username: 'alice', email: 'alice@example.com', displayName: 'Alice', status: 'active', role: 'admin' }])
      if (path.endsWith('/access/groups') && options.method === 'GET') return json([])
      if (path.endsWith(`/access/users/${userId}`) && options.method === 'PUT') return json({ role: 'member' })
      if (path.endsWith(`/access/users/${userId}`) && options.method === 'DELETE') return json({ error: { code: 'last_project_admin' } }, 409)
      throw new Error(`unexpected ${path} ${options.method}`)
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(ProjectAccessSettings, { props: { projectId }, global })
    await flushPromises()
    await wrapper.get(`[data-testid="user-role-${userId}"] select`).setValue('member')
    await flushPromises()
    expect(fetch.mock.calls.some(([path, options]) => String(path).endsWith(`/access/users/${userId}`) && options?.method === 'PUT' && String(options.body).includes('member'))).toBe(true)

    await wrapper.get(`[data-testid="remove-user-${userId}"]`).trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Every Project must retain at least one active direct User administrator')
  })

  it('searches active users and adds a selected role', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path.endsWith('/effective-role')) return json({ role: 'admin' })
      if (path.endsWith('/access/users') && options.method === 'GET') return json([])
      if (path.endsWith('/access/groups') && options.method === 'GET') return json([])
      if (path.includes('/access/directory/users?q=bob')) return json([{ id: candidateId, username: 'bob', email: 'bob@example.com', displayName: 'Bob' }])
      if (path.endsWith(`/access/users/${candidateId}`) && options.method === 'PUT') return json({ role: 'viewer' })
      throw new Error(`unexpected ${path} ${options.method}`)
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(ProjectAccessSettings, { props: { projectId }, global })
    await flushPromises()
    await wrapper.get('[data-testid="user-search"] input').setValue('bob')
    await wrapper.get('[data-testid="search-users"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Bob')
    await wrapper.get(`[data-testid="add-user-${candidateId}"]`).trigger('click')
    await flushPromises()
    expect(fetch.mock.calls.some(([path, options]) => String(path).endsWith(`/access/users/${candidateId}`) && options?.method === 'PUT')).toBe(true)
  })
})
