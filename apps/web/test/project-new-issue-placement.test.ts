import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectEditor from '../app/components/ProjectEditor.vue'
import ProjectSettings from '../app/components/ProjectSettings.vue'
import { projectNewIssuePlacement } from '../app/utils/project-settings'
import { uiStubs } from './ui-stubs'

const projectId = '11111111-1111-4111-8111-111111111111'
const project = {
  id: projectId,
  name: 'Project',
  issuePrefix: 'PRJ',
  sourceType: 'local' as const,
  cloneUrl: null,
  sourceRef: null,
  repositoryPath: '/repo',
  defaultBranch: 'main',
  workflowSettings: {},
  allowInternalRunner: true,
  createdAt: '',
  updatedAt: ''
}
const global = { stubs: uiStubs }

afterEach(() => vi.unstubAllGlobals())

describe('new issue placement project setting', () => {
  it('defaults absent settings to bottom', () => {
    expect(projectNewIssuePlacement(undefined)).toBe('bottom')
    expect(projectNewIssuePlacement({})).toBe('bottom')
    expect(projectNewIssuePlacement({ newIssuePlacement: 'top' })).toBe('top')
  })

  it('persists an explicit top selection without replacing unrelated workflow settings', async () => {
    const current = { ...project, workflowSettings: { custom: true } }
    const fetch = vi.fn(async (_path: string, options: RequestInit = {}) => {
      if (options.method === 'PATCH') return new Response(JSON.stringify(current))
      return new Response(JSON.stringify({ defaultRepositoryPath: '', repositoryRoots: [] }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { props: { project: current }, global })

    await wrapper.get('[data-field=newIssuePlacement] select').setValue('top')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const patch = fetch.mock.calls.find(([, options]) => options.method === 'PATCH')?.[1]
    expect(JSON.parse(String(patch?.body)).workflowSettings).toEqual({ custom: true, newIssuePlacement: 'top' })
  })

  it('shows the effective default to read-only project roles', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/access/effective-role')) return new Response(JSON.stringify({ role: 'viewer' }))
      return new Response(JSON.stringify(project))
    }))
    const wrapper = mount(ProjectSettings, { props: { projectId }, global })
    await flushPromises()

    expect(wrapper.get('[data-testid="project-settings-readonly"]').text()).toContain('New issue placement')
    expect(wrapper.get('[data-testid="project-settings-readonly"]').text()).toContain('bottom')
  })
})
