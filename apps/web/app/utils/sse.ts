import { browserAuthorizationHeaders } from './api'

export type SSESource = { close: () => void }

export type SSEHandlers = {
  onOpen?: () => void
  onMessage?: (data: string) => void
  onEvent?: (name: string, data: string) => void
  onError?: (error?: unknown) => void
}

function dispatchFrame(frame: string, handlers: SSEHandlers) {
  let eventName = 'message'
  const data: string[] = []
  for (const rawLine of frame.split(/\r?\n/)) {
    if (!rawLine || rawLine.startsWith(':')) continue
    const separator = rawLine.indexOf(':')
    const field = separator === -1 ? rawLine : rawLine.slice(0, separator)
    let value = separator === -1 ? '' : rawLine.slice(separator + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') eventName = value || 'message'
    if (field === 'data') data.push(value)
  }
  if (data.length === 0) return
  const payload = data.join('\n')
  if (eventName === 'message') handlers.onMessage?.(payload)
  else handlers.onEvent?.(eventName, payload)
}

function takeFrame(buffer: string): [string | undefined, string] {
  const match = /\r?\n\r?\n/.exec(buffer)
  if (!match || match.index === undefined) return [undefined, buffer]
  const frame = buffer.slice(0, match.index)
  return [frame, buffer.slice(match.index + match[0].length)]
}

function nativeEventSource(path: string, handlers: SSEHandlers): SSESource {
  const source = new EventSource(path)
  source.onopen = () => handlers.onOpen?.()
  source.onmessage = message => handlers.onMessage?.(message.data)
  source.addEventListener('resync', event => handlers.onEvent?.('resync', (event as MessageEvent).data))
  source.onerror = error => handlers.onError?.(error)
  return { close: () => source.close() }
}

function authenticatedFetchStream(path: string, authorization: Record<string, string>, handlers: SSEHandlers): SSESource {
  const controller = new AbortController()
  let closed = false

  void (async () => {
    try {
      const response = await fetch(path, {
        method: 'GET',
        credentials: 'same-origin',
        signal: controller.signal,
        headers: { Accept: 'text/event-stream', ...authorization }
      })
      if (!response.ok) throw new Error(`SSE request failed with status ${response.status}`)
      if (!response.body) throw new Error('SSE response has no readable body')
      handlers.onOpen?.()

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      while (!closed) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        while (true) {
          const [frame, rest] = takeFrame(buffer)
          if (frame === undefined) break
          buffer = rest
          dispatchFrame(frame, handlers)
        }
      }
      if (!closed && !controller.signal.aborted) handlers.onError?.(new Error('SSE stream ended'))
    } catch (error) {
      if (!closed && !controller.signal.aborted) handlers.onError?.(error)
    }
  })()

  return {
    close() {
      if (closed) return
      closed = true
      controller.abort()
    }
  }
}

export function openSSE(path: string, handlers: SSEHandlers): SSESource {
  const authorization = browserAuthorizationHeaders()
  if (!authorization.Authorization) {
    if (typeof EventSource !== 'undefined') return nativeEventSource(path, handlers)
    return { close: () => {} }
  }
  return authenticatedFetchStream(path, authorization, handlers)
}
