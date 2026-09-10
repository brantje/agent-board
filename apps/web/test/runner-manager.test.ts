import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RunnerManager from '../app/components/RunnerManager.vue'
import { uiStubs } from './ui-stubs'

const runners = [
  {
    id: 'runner-external', name: 'build-host', internal: false, managed: false, deletable: true,
    connected: true, revokedAt: null, lastSeenAt: null, capabilities: {}, createdAt: '', updatedAt: ''
  },
  {
    id: 'runner-internal', name: 'Internal', internal: true, managed: true, deletable: false,
    connected: true, revokedAt: null, lastSeenAt: null, capabilities: {}, createdAt: '', updatedAt: ''
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
  it('creates only a one-time token, then allows renaming a registered external runner', async () => {
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const method = init?.method ?? 'GET'
      if (path === '/api/runners' && method === 'POST') {
        return new Response(JSON.stringify({ token: 'one-time-registration-token' }), { status: 201 })
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
    expect(wrapper.findAll('button').filter(button => button.text() === 'Edit')).toHaveLength(1)
    expect(wrapper.find('input').exists()).toBe(false)

    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="runner-registration-token"]').text()).toBe('one-time-registration-token')
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
})
