import { afterEach, describe, expect, it, vi } from 'vitest'
import { openSSE } from '../app/utils/sse'
import { AUTH_STORAGE_KEY } from '../app/utils/auth-storage'

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
  sessionStorage.clear()
})

function storeCredentials(accessToken: string) {
  sessionStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify({
    accessToken,
    accessTokenExpiresAt: '2026-09-13T09:00:00Z',
    refreshToken: 'refresh-token',
    refreshTokenExpiresAt: '2026-10-13T09:00:00Z'
  }))
}

describe('authenticated SSE transport', () => {
  it('streams message and named event frames with the stored bearer token', async () => {
    storeCredentials('sse-access')
    const encoder = new TextEncoder()
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(encoder.encode(': heartbeat\n\nid: evt-1\ndata: {"id":"evt-1"}\n\nevent: resync\ndata: {}\n\n'))
        controller.close()
      }
    })
    const fetch = vi.fn().mockResolvedValue(new Response(body, {
      status: 200,
      headers: { 'Content-Type': 'text/event-stream' }
    }))
    vi.stubGlobal('fetch', fetch)

    const messages: string[] = []
    const named: string[] = []
    let opened = false
    let ended = false
    openSSE('/api/projects/project-a/events', {
      onOpen: () => { opened = true },
      onMessage: data => messages.push(data),
      onEvent: (name, data) => named.push(`${name}:${data}`),
      onError: () => { ended = true }
    })

    await vi.waitFor(() => expect(ended).toBe(true))
    expect(opened).toBe(true)
    expect(messages).toEqual(['{"id":"evt-1"}'])
    expect(named).toEqual(['resync:{}'])
    expect(fetch).toHaveBeenCalledWith('/api/projects/project-a/events', expect.objectContaining({
      credentials: 'same-origin',
      headers: expect.objectContaining({
        Accept: 'text/event-stream',
        Authorization: 'Bearer sse-access'
      })
    }))
  })

  it('aborts an authenticated stream without reporting a reconnect error', async () => {
    storeCredentials('sse-access')
    let aborted = false
    const fetch = vi.fn((_path: string, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => {
        aborted = true
        reject(new DOMException('Aborted', 'AbortError'))
      })
    }))
    vi.stubGlobal('fetch', fetch)
    const onError = vi.fn()

    const source = openSSE('/api/projects/project-a/events', { onError })
    source.close()
    await vi.waitFor(() => expect(aborted).toBe(true))
    expect(onError).not.toHaveBeenCalled()
  })
})
