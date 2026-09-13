import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useRunEvents } from '../app/composables/useRunEvents'
import { AUTH_STORAGE_KEY } from '../app/utils/auth-storage'
import { evidence } from './execution-fixtures'

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
  sessionStorage.clear()
})

describe('run event connection state', () => {
  it('stays connecting until the SSE response actually opens', async () => {
    sessionStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify({
      accessToken: 'run-events-access',
      accessTokenExpiresAt: '2026-09-13T09:00:00Z',
      refreshToken: 'run-events-refresh',
      refreshTokenExpiresAt: '2026-10-13T09:00:00Z'
    }))

    let resolveStream!: (response: Response) => void
    let streamController!: ReadableStreamDefaultController<Uint8Array>
    const streamResponse = new Promise<Response>((resolve) => { resolveStream = resolve })
    const fetch = vi.fn((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/evidence')) {
        return Promise.resolve(new Response(JSON.stringify(evidence()), {
          status: 200,
          headers: { 'Content-Type': 'application/json' }
        }))
      }
      if (path.includes('/reviews?')) {
        return Promise.resolve(new Response('[]', {
          status: 200,
          headers: { 'Content-Type': 'application/json' }
        }))
      }
      if (path.includes('/events?')) return streamResponse
      return Promise.reject(new Error(`unexpected request ${path}`))
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(defineComponent({
      setup() {
        const { connection } = useRunEvents('project-a', 'run-1')
        return () => h('span', connection.value)
      }
    }))

    await vi.waitFor(() => expect(fetch.mock.calls.some(([input]) => String(input).includes('/events?'))).toBe(true))
    expect(wrapper.text()).toBe('connecting')

    const body = new ReadableStream<Uint8Array>({
      start(controller) { streamController = controller }
    })
    resolveStream(new Response(body, {
      status: 200,
      headers: { 'Content-Type': 'text/event-stream' }
    }))

    await vi.waitFor(() => expect(wrapper.text()).toBe('live'))
    wrapper.unmount()
    streamController.close()
  })
})
