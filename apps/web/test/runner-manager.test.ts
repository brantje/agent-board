import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RunnerManager from '../app/components/RunnerManager.vue'
import { uiStubs } from './ui-stubs'

const registeredAt = '2026-09-10T12:00:00Z'
const initialRunners = [
  {
    id: 'runner-external', name: 'build-host', internal: false, managed: false, deletable: true,
    connected: true, registeredAt, revokedAt: null, lastSeenAt: null,
    capabilities: { engines: ['opencode', 'scripted'], max_active_sessions: 10 },
    activeSessions: 2, reservedSessions: 2, maxActiveSessions: 10, createdAt: '', updatedAt: ''
  },
  {
    id: 'runner-internal', name: 'Internal', internal: true, managed: true, deletable: false,
    connected: true, registeredAt, revokedAt: null, lastSeenAt: null,
    capabilities: { engines: ['scripted'] }, activeSessions: 0, reservedSessions: 0, maxActiveSessions: 10,
    createdAt: '', updatedAt: ''
  }
]

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

describe('RunnerManager', () => {
  it('creates a pending runner without a name, shows its token, and only renames registered runners', async () => {
    const runners = [...initialRunners]
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const method = init?.method ?? 'GET'
      if (path === '/api/runners' && method === 'POST') {
        runners.push({
          id: 'runner-pending', name: null, internal: false, managed: false, deletable: true,
          connected: false, registeredAt: null, revokedAt: null, lastSeenAt: null, capabilities: {},
          activeSessions: null, reservedSessions: 0, maxActiveSessions: 10, createdAt: '', updatedAt: ''
        })
        return new Response(JSON.stringify({ runner: { id: 'runner-pending' }, registrationToken: 'one-time-registration-token' }), { status: 201 })
      }
      if (path === '/api/runners/runner-external' && method === 'PATCH') {
        expect(JSON.parse(String(init?.body))).toEqual({ name: 'renamed-host' })
        return new Response(JSON.stringify({ ...runners[0], name: 'renamed-host' }))
      }
      if (path === '/api/runners' && method === 'GET') {
        return new Response(JSON.stringify(runners))
      }
      return new Response('{}', { status: 404 })
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(RunnerManager, { global })
    await flushPromises()

    expect(wrapper.text()).toContain('build-host')
    expect(wrapper.text()).toContain('opencode')
    expect(wrapper.text()).toContain('scripted')
    expect(wrapper.get('[data-testid="runner-session-summary"]').text()).toBe('Sessions: 2 / 10 active')
    expect(wrapper.findAll('button').filter(button => button.text() === 'Edit')).toHaveLength(1)
    expect(wrapper.find('input').exists()).toBe(false)

    const createButton = wrapper.findAll('button').find(button => button.text() === 'Create runner')
    expect(createButton).toBeDefined()
    await createButton!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="runner-registration-token"]').text()).toBe('one-time-registration-token')
    expect(wrapper.text()).toContain('Pending registration')
    expect(wrapper.text()).toContain('runner-pending')
    expect(wrapper.findAll('button').some(button => button.text() === 'Copy')).toBe(true)
    expect(wrapper.findAll('button').filter(button => button.text() === 'Edit')).toHaveLength(1)
    const createCall = fetch.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(createCall?.[0]).toBe('/api/runners')
    expect(createCall?.[1]?.body).toBeUndefined()

    const editButton = wrapper.findAll('button').find(button => button.text() === 'Edit')
    expect(editButton).toBeDefined()
    await editButton!.trigger('click')
    const input = wrapper.get('input')
    expect(input.element.value).toBe('build-host')
    await input.setValue('renamed-host')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(fetch).toHaveBeenCalledWith('/api/runners/runner-external', expect.objectContaining({ method: 'PATCH' }))
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('shows revoked pending runners and reports create failures', async () => {
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const method = init?.method ?? 'GET'
      if (path === '/api/runners' && method === 'GET') {
        return new Response(JSON.stringify([{
          id: 'runner-pending', name: null, internal: false, managed: false, deletable: true,
          connected: false, registeredAt: null, revokedAt: '2026-09-10T12:00:00Z', lastSeenAt: null,
          capabilities: {}, activeSessions: null, reservedSessions: 0, maxActiveSessions: 10, createdAt: '', updatedAt: ''
        }]))
      }
      if (path === '/api/runners' && method === 'POST') {
        return new Response(JSON.stringify({ error: { code: 'conflict', message: 'create failed' } }), {
          status: 409,
          headers: { 'Content-Type': 'application/json' }
        })
      }
      return new Response('{}', { status: 404 })
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(RunnerManager, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('Revoked')
    expect(wrapper.text()).toContain('Pending registration')
    expect(wrapper.findAll('button').filter(button => button.text() === 'Edit')).toHaveLength(0)

    const createButton = wrapper.findAll('button').find(button => button.text() === 'Create runner')
    expect(createButton).toBeDefined()
    await createButton!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Unable to create runner')
    expect(wrapper.find('[data-testid="runner-registration-token"]').exists()).toBe(false)
  })
})
