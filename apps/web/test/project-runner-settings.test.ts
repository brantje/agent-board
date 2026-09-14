import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectRunnerSettings from '../app/components/ProjectRunnerSettings.vue'
import RunnerManager from '../app/components/RunnerManager.vue'
import { uiStubs } from './ui-stubs'

const runner = (id: string, projectId: string | null, name: string) => ({
  id, projectId, name, internal: false, managed: false, deletable: true,
  connected: true, registeredAt: '2026-09-14T00:00:00Z', revokedAt: null,
  lastSeenAt: '2026-09-14T00:00:00Z', capabilities: { engines: ['opencode'] },
  activeSessions: 0, reservedSessions: 0, maxActiveSessions: 5, createdAt: '', updatedAt: ''
})

const global = {
  stubs: {
    ...uiStubs,
    UAlert: {
      props: ['title', 'description'],
      template: '<div role="alert">{{ title }} {{ description }}<slot name="actions" /></div>'
    }
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('project runner settings', () => {
  it('loads project context for the dedicated runner settings page', async () => {
    const fetch = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path === '/api/projects/project-1') {
        return new Response(JSON.stringify({ id: 'project-1', allowInternalRunner: false }))
      }
      if (path === '/api/projects/project-1/access/effective-role') {
        return new Response(JSON.stringify({ role: 'admin' }))
      }
      return new Response('{}', { status: 404 })
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(ProjectRunnerSettings, {
      props: { projectId: 'project-1' },
      global: {
        stubs: {
          ...global.stubs,
          RunnerManager: {
            props: ['projectId', 'canAdmin', 'allowInternalRunner'],
            template: '<div data-runner-manager :data-project="projectId" :data-admin="String(canAdmin)" :data-internal="String(allowInternalRunner)" />'
          }
        }
      }
    })
    await flushPromises()

    expect(wrapper.get('h1').text()).toBe('Runners')
    const manager = wrapper.get('[data-runner-manager]')
    expect(manager.attributes('data-project')).toBe('project-1')
    expect(manager.attributes('data-admin')).toBe('true')
    expect(manager.attributes('data-internal')).toBe('false')
  })

  it('separates dedicated, deployment and internal fallback capacity and uses project-scoped mutations', async () => {
    const settings = {
      runnerIds: [] as string[],
      projectRunners: [runner('owned-1', 'project-1', 'owned-host')],
      sharedRunners: [runner('shared-1', null, 'shared-host')]
    }
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const method = init?.method ?? 'GET'
      if (path === '/api/projects/project-1/runners' && method === 'GET') {
        return new Response(JSON.stringify(settings))
      }
      if (path === '/api/projects/project-1/runners' && method === 'POST') {
        return new Response(JSON.stringify({ runner: { id: 'pending-1' }, registrationToken: 'project-registration' }), { status: 201 })
      }
      if (path === '/api/projects/project-1/runners' && method === 'PUT') {
        settings.runnerIds = ['shared-1']
        return new Response(JSON.stringify(settings))
      }
      if (path === '/api/projects/project-1' && method === 'PATCH') {
        return new Response(JSON.stringify({ id: 'project-1', allowInternalRunner: true }))
      }
      return new Response('{}', { status: 404 })
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(RunnerManager, {
      props: { projectId: 'project-1', canAdmin: true, allowInternalRunner: false },
      global
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Project runners')
    expect(wrapper.text()).toContain('Deployment runners')
    expect(wrapper.text()).toContain('Shared external runners')
    expect(wrapper.text()).toContain('Internal runner fallback')
    expect(wrapper.text()).toContain('owned-host')
    const internal = wrapper.get('[data-testid="internal-runner-capacity"]')
    expect(internal.text()).toContain('Internal runner')
    expect(internal.text()).toContain('Fallback disabled')

    await wrapper.find('input[type="checkbox"]').setValue(true)
    const savePolicy = wrapper.findAll('button').find(button => button.text() === 'Save shared runner policy')
    await savePolicy!.trigger('click')
    await flushPromises()
    const put = fetch.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(put?.[0]).toBe('/api/projects/project-1/runners')
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({ runnerIds: ['shared-1'] })

    await wrapper.get('input[role="switch"]').setValue(true)
    await flushPromises()
    expect(internal.text()).toContain('Fallback enabled')
    const patch = fetch.mock.calls.find(([, init]) => init?.method === 'PATCH')
    expect(patch?.[0]).toBe('/api/projects/project-1')
    expect(JSON.parse(String(patch?.[1]?.body))).toEqual({ allowInternalRunner: true })

    const create = wrapper.findAll('button').find(button => button.text() === 'Create runner')
    await create!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="runner-registration-token"]').text()).toBe('project-registration')
  })

  it('manages a project-owned runner through project-scoped lifecycle routes', async () => {
    const settings = {
      runnerIds: [] as string[],
      projectRunners: [runner('owned-1', 'project-1', 'owned-host')],
      sharedRunners: [] as ReturnType<typeof runner>[]
    }
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const method = init?.method ?? 'GET'
      if (path === '/api/projects/project-1/runners' && method === 'GET') {
        return new Response(JSON.stringify(settings))
      }
      if (path === '/api/projects/project-1/runners/owned-1' && method === 'PATCH') {
        const body = JSON.parse(String(init?.body)) as { name: string, maxActiveSessions: number }
        settings.projectRunners[0] = { ...settings.projectRunners[0], ...body }
        return new Response(JSON.stringify(settings.projectRunners[0]))
      }
      if (path === '/api/projects/project-1/runners/owned-1/rotate-token' && method === 'POST') {
        return new Response(JSON.stringify({ runner: settings.projectRunners[0], runnerToken: 'rotated-project-token' }))
      }
      if (path === '/api/projects/project-1/runners/owned-1/revoke' && method === 'POST') {
        return new Response(JSON.stringify({ ...settings.projectRunners[0], connected: false, revokedAt: '2026-09-14T01:00:00Z' }))
      }
      if (path === '/api/projects/project-1/runners/owned-1' && method === 'DELETE') {
        settings.projectRunners = []
        return new Response(null, { status: 204 })
      }
      return new Response('{}', { status: 404 })
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(RunnerManager, {
      props: { projectId: 'project-1', canAdmin: true, allowInternalRunner: false },
      global
    })
    await flushPromises()

    const edit = wrapper.findAll('button').find(button => button.text() === 'Edit')
    await edit!.trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    await dialog.get('input[type="text"]').setValue('renamed-owned-host')
    await dialog.get('form').trigger('submit')
    await flushPromises()

    const editCall = fetch.mock.calls.find(([input, init]) => String(input) === '/api/projects/project-1/runners/owned-1' && init?.method === 'PATCH')
    expect(editCall?.[0]).toBe('/api/projects/project-1/runners/owned-1')
    expect(JSON.parse(String(editCall?.[1]?.body))).toEqual({ name: 'renamed-owned-host', maxActiveSessions: 5 })

    const rotate = wrapper.findAll('button').find(button => button.text() === 'Rotate token')
    await rotate!.trigger('click')
    await flushPromises()
    const rotateCall = fetch.mock.calls.find(([input, init]) => String(input) === '/api/projects/project-1/runners/owned-1/rotate-token' && init?.method === 'POST')
    expect(rotateCall?.[0]).toBe('/api/projects/project-1/runners/owned-1/rotate-token')
    expect(rotateCall?.[1]?.body).toBeUndefined()
    expect(wrapper.get('[data-testid="runner-token"]').text()).toBe('rotated-project-token')

    const revoke = wrapper.findAll('button').find(button => button.text() === 'Revoke')
    await revoke!.trigger('click')
    await flushPromises()
    const revokeCall = fetch.mock.calls.find(([input, init]) => String(input) === '/api/projects/project-1/runners/owned-1/revoke' && init?.method === 'POST')
    expect(revokeCall?.[0]).toBe('/api/projects/project-1/runners/owned-1/revoke')
    expect(revokeCall?.[1]?.body).toBeUndefined()

    const remove = wrapper.findAll('button').find(button => button.text() === 'Delete')
    await remove!.trigger('click')
    await flushPromises()
    const deleteCall = fetch.mock.calls.find(([input, init]) => String(input) === '/api/projects/project-1/runners/owned-1' && init?.method === 'DELETE')
    expect(deleteCall?.[0]).toBe('/api/projects/project-1/runners/owned-1')
    expect(deleteCall?.[1]?.body).toBeUndefined()
  })

  it('keeps project runner controls read-only without project admin permission', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
      runnerIds: [], projectRunners: [runner('owned-1', 'project-1', 'owned-host')], sharedRunners: []
    }))))
    const wrapper = mount(RunnerManager, {
      props: { projectId: 'project-1', canAdmin: false, allowInternalRunner: true },
      global
    })
    await flushPromises()
    expect(wrapper.findAll('button').some(button => button.text() === 'Create runner')).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text() === 'Edit')).toBe(false)
    expect(wrapper.get('[data-testid="internal-runner-capacity"]').text()).toContain('Fallback enabled')
    expect(wrapper.get('input[role="switch"]').attributes('disabled')).toBeDefined()
  })
})
