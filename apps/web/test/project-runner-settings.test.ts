import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RunnerManager from '../app/components/RunnerManager.vue'
import { uiStubs } from './ui-stubs'

const runner = (id: string, projectId: string | null, name: string) => ({
  id, projectId, name, internal: false, managed: false, deletable: true,
  connected: true, registeredAt: '2026-09-14T00:00:00Z', revokedAt: null,
  lastSeenAt: '2026-09-14T00:00:00Z', capabilities: { engines: ['opencode'] },
  activeSessions: 0, reservedSessions: 0, maxActiveSessions: 5, createdAt: '', updatedAt: ''
})

const global = { stubs: { ...uiStubs } }

afterEach(() => vi.unstubAllGlobals())

describe('project runner settings', () => {
  it('separates dedicated, shared and internal fallback and uses project-scoped mutations', async () => {
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
        expect(JSON.parse(String(init?.body))).toEqual({ runnerIds: ['shared-1'] })
        settings.runnerIds = ['shared-1']
        return new Response(JSON.stringify(settings))
      }
      if (path === '/api/projects/project-1' && method === 'PATCH') {
        expect(JSON.parse(String(init?.body))).toEqual({ allowInternalRunner: true })
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
    expect(wrapper.text()).toContain('Shared runners')
    expect(wrapper.text()).toContain('Internal runner fallback')
    expect(wrapper.text()).toContain('owned-host')

    await wrapper.find('input[type="checkbox"]').setValue(true)
    const savePolicy = wrapper.findAll('button').find(button => button.text() === 'Save shared runner policy')
    await savePolicy!.trigger('click')
    await flushPromises()
    expect(fetch).toHaveBeenCalledWith('/api/projects/project-1/runners', expect.objectContaining({ method: 'PUT' }))

    await wrapper.get('input[role="switch"]').setValue(true)
    const saveFallback = wrapper.findAll('button').find(button => button.text() === 'Save fallback')
    await saveFallback!.trigger('click')
    await flushPromises()
    expect(fetch).toHaveBeenCalledWith('/api/projects/project-1', expect.objectContaining({ method: 'PATCH' }))

    const create = wrapper.findAll('button').find(button => button.text() === 'Create runner')
    await create!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="runner-registration-token"]').text()).toBe('project-registration')
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
    expect(wrapper.get('input[role="switch"]').attributes('disabled')).toBeDefined()
  })
})
