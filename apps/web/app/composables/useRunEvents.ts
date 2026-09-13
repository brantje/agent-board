import { onBeforeUnmount, onMounted, ref, shallowRef, toValue, watch, type MaybeRefOrGetter } from 'vue'
import type { EventEvidence, Issue, Question, Review, Run, RunEvidence, RunUsageEvidence } from '../types/api'
import { apiPath, apiQuery, apiRequest, type ApiError } from '../utils/api'
import { maxSequence, mergeEvents, parseEventMessage, refetchTargets, applyCurrentBranchToRun } from '../utils/events'
import { openSSE, type SSESource } from '../utils/sse'

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
  let usageRequest = 0
  let source: SSESource | undefined
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
      if (type === 'model.usage') {
        const currentUsageRequest = ++usageRequest
        const usage = await apiRequest<RunUsageEvidence | null>(`${apiPath('runs', ids.projectId, ids.runId)}/usage`, { signal: controller?.signal })
        if (currentUsageRequest === usageRequest && evidence.value) {
          evidence.value = { ...evidence.value, usage }
        }
      }
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
    closeSource()
    connection.value = connection.value === 'reconnecting' ? 'reconnecting' : 'connecting'
    const afterSequence = maxSequence(events.value)
    source = openSSE(apiQuery(`${apiPath('runs', ids.projectId, ids.runId)}/events`, { afterSequence: String(afterSequence) }), {
      onOpen: () => {
        if (current === generation) connection.value = 'live'
      },
      onMessage: data => {
        if (current !== generation) return
        const incoming = parseEventMessage(data)
        if (!incoming) return
        events.value = mergeEvents(events.value, [incoming])
        if (evidence.value?.run) {
          const patched = applyCurrentBranchToRun(evidence.value.run, incoming)
          if (patched) evidence.value = { ...evidence.value, run: patched }
        }
        void refetchFor(incoming.type, ids)
      },
      onError: () => {
        if (current !== generation) return
        source?.close()
        source = undefined
        connection.value = 'reconnecting'
        reconnectTimer = setTimeout(() => {
          if (current === generation) openStream(ids, current)
        }, SSE_RECONNECT_MS)
      }
    })
    if (current === generation) connection.value = 'live'
  }

  async function refresh() {
    const current = ++generation
    usageRequest++
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
    usageRequest++
    closeSource()
    controller?.abort()
    connection.value = 'disconnected'
  })

  return { evidence, events, questions, reviews, issue, connection, pending, error, refresh }
}
