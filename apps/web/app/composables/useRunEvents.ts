import { onBeforeUnmount, onMounted, ref, shallowRef, toValue, watch, type MaybeRefOrGetter } from 'vue'
import type { EventEvidence, Issue, Question, Review, Run, RunEvidence } from '../types/api'
import { apiPath, apiQuery, apiRequest, type ApiError } from '../utils/api'
import { maxSequence, mergeEvents, parseEventMessage, refetchTargets } from '../utils/events'

export const SSE_RECONNECT_MS = 2000
export type RunEventConnection = 'connecting' | 'live' | 'reconnecting' | 'disconnected' | 'error'

export function useRunEvents(projectId: MaybeRefOrGetter<string>, runId: MaybeRefOrGetter<string>) {
  const evidence = shallowRef<RunEvidence>()
  const events = shallowRef<EventEvidence[]>([])
  const questions = shallowRef<Question[]>([])
  const reviews = shallowRef<Review[]>([])
  const issue = shallowRef<Issue>()
  const connection = ref<RunEventConnection>('connecting')
  const pending = ref(true)
  const error = shallowRef<ApiError>()
  let generation = 0
  let source: EventSource | undefined
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined
  let controller: AbortController | undefined

  function currentIds() {
    return { projectId: toValue(projectId), runId: toValue(runId) }
  }

  function closeSource() {
    source?.close()
    source = undefined
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = undefined
    }
  }

  async function refetchFor(type: string, ids: { projectId: string; runId: string }) {
    const targets = refetchTargets(type)
    try {
      if (targets.run) {
        const run = await apiRequest<Run>(apiPath('runs', ids.projectId, ids.runId), { signal: controller?.signal })
        if (evidence.value) evidence.value = { ...evidence.value, run }
      }
      if (targets.questions) {
        questions.value = await apiRequest<Question[]>(apiQuery(apiPath('questions', ids.projectId), { runId: ids.runId, status: 'OPEN' }), { signal: controller?.signal })
      }
      if (targets.reviews && evidence.value?.run.issueId) {
        reviews.value = await apiRequest<Review[]>(apiQuery(apiPath('reviews', ids.projectId), { issueId: evidence.value.run.issueId }), { signal: controller?.signal })
      }
      if (targets.issue && evidence.value?.run.issueId) {
        issue.value = await apiRequest<Issue>(apiPath('issues', ids.projectId, evidence.value.run.issueId), { signal: controller?.signal })
      }
    } catch {
      // Live timeline stays Event-driven; read-model refetch failures are non-fatal.
    }
  }

  function openStream(ids: { projectId: string; runId: string }, current: number) {
    if (typeof EventSource === 'undefined') return
    closeSource()
    connection.value = connection.value === 'reconnecting' ? 'reconnecting' : 'connecting'
    const afterSequence = maxSequence(events.value)
    source = new EventSource(apiQuery(`${apiPath('runs', ids.projectId, ids.runId)}/events`, { afterSequence: String(afterSequence) }))
    source.onopen = () => {
      if (current === generation) connection.value = 'live'
    }
    if (current === generation) connection.value = 'live'
    source.onmessage = message => {
      if (current !== generation) return
      const incoming = parseEventMessage(message.data)
      if (!incoming) return
      events.value = mergeEvents(events.value, [incoming])
      void refetchFor(incoming.type, ids)
    }
    source.onerror = () => {
      if (current !== generation) return
      source?.close()
      source = undefined
      connection.value = 'reconnecting'
      reconnectTimer = setTimeout(() => {
        if (current === generation) openStream(ids, current)
      }, SSE_RECONNECT_MS)
    }
  }

  async function refresh() {
    const current = ++generation
    const ids = currentIds()
    closeSource()
    controller?.abort()
    controller = new AbortController()
    pending.value = evidence.value === undefined
    error.value = undefined
    if (!evidence.value) connection.value = 'connecting'
    try {
      const snapshot = await apiRequest<RunEvidence>(`${apiPath('runs', ids.projectId, ids.runId)}/evidence`, { signal: controller.signal })
      if (current !== generation) return
      evidence.value = snapshot
      events.value = mergeEvents([], snapshot.events || [])
      pending.value = false
      if (snapshot.run.issueId) {
        try {
          reviews.value = await apiRequest<Review[]>(apiQuery(apiPath('reviews', ids.projectId), { issueId: snapshot.run.issueId }), { signal: controller.signal })
        } catch {
          reviews.value = []
        }
      }
      if (current !== generation) return
      openStream(ids, current)
    } catch (failure) {
      if (current !== generation) return
      error.value = failure as ApiError
      evidence.value = undefined
      events.value = []
      connection.value = 'error'
      pending.value = false
    }
  }

  onMounted(refresh)
  watch(() => `${toValue(projectId)}/${toValue(runId)}`, () => {
    evidence.value = undefined
    events.value = []
    void refresh()
  })
  onBeforeUnmount(() => {
    generation++
    closeSource()
    controller?.abort()
    connection.value = 'disconnected'
  })

  return { evidence, events, questions, reviews, issue, connection, pending, error, refresh }
}
