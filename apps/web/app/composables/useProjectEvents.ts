import { onBeforeUnmount, onMounted, toValue, watch, type MaybeRefOrGetter } from 'vue'
import type { EventEvidence } from '../types/api'
import { apiPath, apiQuery } from '../utils/api'
import { parseEventMessage } from '../utils/events'
import { SSE_RECONNECT_MS } from './useRunEvents'

type ProjectEventHandler = (event: EventEvidence, projectId: string) => void | Promise<void>

function normalizeProjectIds(value: string | string[] | undefined) {
  if (!value) return []
  return [...new Set((Array.isArray(value) ? value : [value]).filter(Boolean))]
}

export function useProjectEvents(
  projectId: MaybeRefOrGetter<string | string[] | undefined>,
  onEvent: ProjectEventHandler
) {
  const lastIds = new Map<string, string>()
  const sources = new Map<string, EventSource>()
  const reconnectTimers = new Map<string, ReturnType<typeof setTimeout>>()
  let disposed = false
  let generation = 0
  let handlers = Promise.resolve()

  function notify(event: EventEvidence, projectId: string, current: number) {
    handlers = handlers.then(async () => {
      if (disposed || current !== generation) return
      await onEvent(event, projectId)
    }).catch(() => {})
  }

  function closeOne(id: string) {
    sources.get(id)?.close()
    sources.delete(id)
    const timer = reconnectTimers.get(id)
    if (timer) {
      clearTimeout(timer)
      reconnectTimers.delete(id)
    }
  }

  function closeAll() {
    for (const id of [...sources.keys(), ...reconnectTimers.keys()]) closeOne(id)
  }

  function openOne(id: string, current: number) {
    if (disposed || current !== generation || typeof EventSource === 'undefined') return
    closeOne(id)
    const afterId = lastIds.get(id)
    const source = new EventSource(apiQuery(`${apiPath('projects', undefined, id)}/events`, { afterId }))
    sources.set(id, source)
    source.addEventListener('resync', () => {
      if (disposed || current !== generation) return
      lastIds.delete(id)
      notify({
        id: `resync:${id}`,
        schemaVersion: 1,
        type: 'project.resync',
        occurredAt: new Date().toISOString(),
        sequence: null,
        agentId: null,
        workspaceId: null,
        runtimeInstanceId: null,
        correlationId: null,
        parentEventId: null,
        actor: { type: 'SYSTEM' },
        payload: {}
      }, id, current)
    })
    source.onmessage = message => {
      if (disposed || current !== generation) return
      const incoming = parseEventMessage(message.data)
      if (!incoming) return
      lastIds.set(id, incoming.id)
      notify(incoming, id, current)
    }
    source.onerror = () => {
      if (disposed || current !== generation) return
      closeOne(id)
      reconnectTimers.set(id, setTimeout(() => {
        reconnectTimers.delete(id)
        if (!disposed && current === generation) openOne(id, current)
      }, SSE_RECONNECT_MS))
    }
  }

  function sync() {
    const current = ++generation
    const ids = normalizeProjectIds(toValue(projectId))
    for (const id of sources.keys()) {
      if (!ids.includes(id)) closeOne(id)
    }
    for (const id of ids) openOne(id, current)
  }

  onMounted(sync)
  watch(() => normalizeProjectIds(toValue(projectId)).join(','), sync)
  onBeforeUnmount(() => {
    disposed = true
    generation++
    closeAll()
  })
}
