import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectSettings from '../app/components/ProjectSettings.vue'
import { uiStubs } from './ui-stubs'

const projectId = '11111111-1111-4111-8111-111111111111'
const project = {
  id: projectId,
  name: 'Private Project',
  issuePrefix: 'PRV',
  sourceType: 'local' as const,
  cloneUrl: null,
  sourceRef: null,
  repositoryPath: '/repo/private',
  defaultBranch: 'main',
  workflowSettings: {},
  allowInternalRunner: true,
  createdAt: '2026-09-13T00:00:00Z',
  updatedAt: '2026-09-13T00:00:00Z'
}

const global = { stubs: uiStubs }

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
}

afterEach(() => vi.unstubAllGlobals())

function projectSettingsFetch(role: 'viewer' | 'member' | 'admin') {
  return vi.fn(async (path: string, options: RequestInit = {}) => {
    if (path === `/api/projects/${projectId}` && options.method === 'GET') return json(project)
    if (path === `/api/projects/${projectId}/access/effective-role` && options.method === 'GET') return json({ role })
    if (role === 'admin' && path.endsWith('/access/users') && options.method === 'GET') return json([])
    if (role === 'admin' && path.endsWith('/access/groups') && options.method === 'GET') return json([])
    throw new Error(`unexpected ${path} ${options.method}`)
  })
}

describe('ProjectSettings role presentation', () => {
  it.each(['viewer', 'member'] as const)('shows %s a read-only settings state with no mutation controls', async (role) => {
    const fetch = projectSettingsFetch(role)
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(ProjectSettings, { props: { projectId }, global })
    await flushPromises()

    expect(wrapper.get('[data-testid="project-settings-readonly"]').text()).toContain(`read-only for your ${role} role`)
    expect(wrapper.text()).toContain('Private Project')
    expect(wrapper.text()).not.toContain('Save project')
    expect(wrapper.find('[data-testid="access-management"]').exists()).toBe(false)
    expect(fetch.mock.calls.some(([path, options]) => String(path) === `/api/projects/${projectId}` && options?.method === 'PATCH')).toBe(false)
  })

  it('shows Project admins editable settings and access management', async () => {
    const fetch = projectSettingsFetch('admin')
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(ProjectSettings, { props: { projectId }, global })
    await flushPromises()

    expect(wrapper.find('[data-testid="project-settings-readonly"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Save project')
    expect(wrapper.get('[data-testid="access-management"]').exists()).toBe(true)
    expect(fetch.mock.calls.filter(([path]) => String(path).endsWith('/access/effective-role'))).toHaveLength(1)
  })
})
