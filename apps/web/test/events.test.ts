import { mount, flushPromises } from '@vue/test-utils'
import { ref, defineComponent, h } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { eventTitle, mergeEvents, maxSequence, refetchTargets, isBoardActivityEvent, compareEvents } from '../app/utils/events'
import { useRunEvents } from '../app/composables/useRunEvents'
import { useProjectEvents } from '../app/composables/useProjectEvents'
import ActivityTimeline from '../app/components/ActivityTimeline.vue'
import { event, evidence, MockEventSource } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const global = { stubs: uiStubs }

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  MockEventSource.reset()
})

describe('event timeline projection', () => {
  it('orders by sequence, ignores duplicate ids, and keeps later unique events', () => {
    const first = event({ id: 'a', type: 'run.started', sequence: 2 })
    const second = event({ id: 'b', type: 'agent.message', sequence: 1, payload: { kind: 'plan', message: 'Plan first' } })
    const duplicate = event({ id: 'a', type: 'run.started', sequence: 2 })
    const third = event({ id: 'c', type: 'agent.message', sequence: 3, payload: { kind: 'message', message: 'Working' } })
    const merged = mergeEvents([first], [second, duplicate, third])
    expect(merged.map(item => item.id)).toEqual(['b', 'a', 'c'])
    expect(maxSequence(merged)).toBe(3)
    expect(compareEvents(
      event({ id: 'later', type: 'issue.updated', sequence: 1, occurredAt: '2026-01-01T00:00:02.000Z' }),
      event({ id: 'earlier', type: 'issue.updated', sequence: 1, occurredAt: '2026-01-01T00:00:01.000Z' })
    )).toBeGreaterThan(0)
  })

  it('labels semantic agent.message kinds without calling them thoughts or inferring rationale from tools', () => {
    const kinds = ['message', 'plan', 'rationale', 'progress', 'discovery', 'summary'] as const
    expect(kinds.map(kind => eventTitle(event({ id: kind, type: 'agent.message', payload: { kind, message: 'visible' } })))).toEqual([
      'Message', 'Plan', 'Rationale', 'Progress', 'Discovery', 'Summary'
    ])
    expect(eventTitle(event({ id: 'tool', type: 'tool.completed', payload: { name: 'edit' } }))).toBe('Tool Completed')
    expect(eventTitle(event({ id: 'thoughts', type: 'agent.message', payload: { kind: 'thoughts', message: 'hidden' } }))).toBe('Message')
    expect(eventTitle(event({ id: 'x', type: 'future.unknown', occurredAt: '2026-01-01T00:00:00.000Z', payload: { raw: true } }))).toBe('future.unknown')
    expect(JSON.stringify(kinds.map(kind => eventTitle(event({ id: kind, type: 'agent.message', payload: { kind } }))))).not.toMatch(/thought/i)
  })

  it('requests matching read models for run, question, review, and issue events', () => {
    expect(refetchTargets('run.waiting_for_input')).toEqual({ run: true, questions: false, reviews: false, issue: false })
    expect(refetchTargets('question.created')).toEqual({ run: false, questions: true, reviews: false, issue: false })
    expect(refetchTargets('review.approved')).toEqual({ run: false, questions: false, reviews: true, issue: false })
    expect(refetchTargets('issue.status_changed')).toEqual({ run: false, questions: false, reviews: false, issue: true })
    expect(refetchTargets('agent.message')).toEqual({ run: false, questions: false, reviews: false, issue: false })
    expect(isBoardActivityEvent('question.created')).toBe(true)
    expect(isBoardActivityEvent('decision.recorded')).toBe(true)
    expect(isBoardActivityEvent('project.resync')).toBe(true)
    expect(isBoardActivityEvent('tool.completed')).toBe(false)
    expect(isBoardActivityEvent('agent.message')).toBe(false)
  })
})

describe('ActivityTimeline', () => {
  it('renders sequence order, semantic labels, and a diagnostic fallback for unknown types', () => {
    const wrapper = mount(ActivityTimeline, {
      props: {
        events: [
          event({ id: 'u', type: 'future.unknown', sequence: 2, occurredAt: '2026-01-01T00:02:00.000Z', payload: { note: 'keep' } }),
          event({ id: 'm', type: 'agent.message', sequence: 1, payload: { kind: 'plan', message: 'Ship the candidate' } })
        ]
      },
      global
    })
    const items = wrapper.findAll('li')
    expect(items[0]?.text()).toContain('Plan')
    expect(items[0]?.text()).toContain('Ship the candidate')
    expect(items[1]?.text()).toContain('future.unknown')
    expect(items[1]?.text()).toContain('2026-01-01T00:02:00.000Z')
    expect(items[1]?.text()).toContain('"note": "keep"')
    expect(wrapper.text()).not.toMatch(/thought/i)
  })
})

describe('useRunEvents', () => {
  it('loads evidence, opens SSE after the last sequence, dedupes replay, reconnects, and never treats disconnect as FAILED', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', MockEventSource)
    const snapshot = evidence({
      events: [
        event({ id: 'one', type: 'run.started', sequence: 1 }),
        event({ id: 'two', type: 'agent.message', sequence: 2, payload: { kind: 'message', message: 'Hello' } })
      ]
    })
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/evidence')) return new Response(JSON.stringify(snapshot))
      if (path.includes('/questions')) return new Response(JSON.stringify([]))
      if (path.includes('/reviews')) return new Response(JSON.stringify([]))
      if (path.includes('/issues/')) return new Response(JSON.stringify({ id: 'AB-1', status: 'IN_PROGRESS' }))
      if (path.endsWith('/runs/run-1')) return new Response(JSON.stringify({ ...snapshot.run, status: 'WAITING_FOR_INPUT' }))
      return new Response('{}', { status: 404 })
    })
    vi.stubGlobal('fetch', fetch)

    let live!: ReturnType<typeof useRunEvents>
    const wrapper = mount(defineComponent({
      setup() {
        live = useRunEvents('project-a', 'run-1')
        return () => h('div', [
          live.connection.value,
          live.events.value.map(item => item.id).join(','),
          live.evidence.value?.run.status
        ].join('|'))
      }
    }))
    await flushPromises()
    expect(MockEventSource.instances[0]?.url).toBe('/api/projects/project-a/runs/run-1/events?afterSequence=2')
    expect(wrapper.text()).toContain('live')
    expect(wrapper.text()).toContain('RUNNING')

    MockEventSource.instances[0]?.emit(event({ id: 'two', type: 'agent.message', sequence: 2, payload: { kind: 'message', message: 'Hello' } }))
    MockEventSource.instances[0]?.emit(event({ id: 'three', type: 'run.waiting_for_input', sequence: 3 }))
    await flushPromises()
    expect(live.events.value.map(item => item.id)).toEqual(['one', 'two', 'three'])
    expect(fetch.mock.calls.some(([path]) => path === '/api/projects/project-a/runs/run-1')).toBe(true)

    MockEventSource.instances[0]?.fail()
    await flushPromises()
    expect(live.connection.value).toBe('reconnecting')
    expect(live.evidence.value?.run.status).not.toBe('FAILED')
    expect(wrapper.text()).not.toContain('FAILED')

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(MockEventSource.instances.at(-1)?.url).toBe('/api/projects/project-a/runs/run-1/events?afterSequence=3')
    expect(MockEventSource.instances[0]?.closed).toBe(true)

    wrapper.unmount()
    expect(MockEventSource.instances.at(-1)?.closed).toBe(true)
  })

  it('keeps an unknown live event without throwing and surfaces evidence load errors', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/evidence')) {
        return new Response(JSON.stringify(evidence({ events: [event({ id: 'one', type: 'run.started', sequence: 1 })] })))
      }
      return new Response(JSON.stringify([]))
    }))
    let live!: ReturnType<typeof useRunEvents>
    const wrapper = mount(defineComponent({ setup() { live = useRunEvents('project-a', 'run-1'); return () => h('div') } }))
    await flushPromises()
    expect(() => MockEventSource.instances[0]?.emit(event({ id: 'weird', type: 'not.a.real.type', sequence: 2, payload: { x: 1 } }))).not.toThrow()
    await flushPromises()
    expect(live.events.value.at(-1)?.type).toBe('not.a.real.type')
    wrapper.unmount()

    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'run_not_found' } }), { status: 404 })))
    const failed = mount(defineComponent({ setup() { live = useRunEvents('project-a', 'run-1'); return () => h('p', live.error.value?.message) } }))
    await flushPromises()
    expect(live.error.value?.status).toBe(404)
    expect(failed.text()).not.toContain('FAILED')
    failed.unmount()
  })

  it('does not pending-flash last evidence while a later refresh is in flight', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const snapshot = evidence({ events: [event({ id: 'one', type: 'run.started', sequence: 1 })] })
    const resolvers: ((response: Response) => void)[] = []
    vi.stubGlobal('fetch', vi.fn((path: string) => {
      if (String(path).endsWith('/evidence')) return new Promise<Response>(resolve => resolvers.push(resolve))
      return Promise.resolve(new Response(JSON.stringify([])))
    }))
    let live!: ReturnType<typeof useRunEvents>
    const wrapper = mount(defineComponent({
      setup() {
        live = useRunEvents('project-a', 'run-1')
        return () => h('div', live.pending.value ? 'Loading' : live.evidence.value?.run.status)
      }
    }))
    resolvers[0]!(new Response(JSON.stringify(snapshot)))
    await flushPromises()
    expect(wrapper.text()).toBe('RUNNING')
    const refresh = live.refresh()
    expect(live.pending.value).toBe(false)
    expect(live.evidence.value?.run.status).toBe('RUNNING')
    expect(wrapper.text()).toBe('RUNNING')
    resolvers[1]!(new Response(JSON.stringify({ ...snapshot, run: { ...snapshot.run, status: 'WAITING_FOR_INPUT' } })))
    await refresh
    expect(wrapper.text()).toBe('WAITING_FOR_INPUT')
    wrapper.unmount()
  })
})

describe('useProjectEvents', () => {
  it('opens a Project stream, reconnects with afterId, and does not leak after unmount', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', MockEventSource)
    const received: string[] = []
    const wrapper = mount(defineComponent({
      setup() {
        useProjectEvents('project-a', event => { received.push(event.id) })
        return () => h('div')
      }
    }))
    await flushPromises()
    expect(MockEventSource.instances[0]?.url).toBe('/api/projects/project-a/events')

    MockEventSource.instances[0]?.emit(event({ id: 'evt-1', type: 'issue.created', sequence: null }))
    await flushPromises()
    expect(received).toEqual(['evt-1'])

    MockEventSource.instances[0]?.fail()
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(MockEventSource.instances.at(-1)?.url).toBe('/api/projects/project-a/events?afterId=evt-1')
    expect(MockEventSource.instances[0]?.closed).toBe(true)

    wrapper.unmount()
    MockEventSource.instances.at(-1)?.emit(event({ id: 'evt-2', type: 'issue.updated', sequence: null }))
    await vi.advanceTimersByTimeAsync(2000)
    expect(received).toEqual(['evt-1'])
    expect(MockEventSource.instances.at(-1)?.closed).toBe(true)
  })

  it('opens one stream per Project and ignores overlapping reconnects after unmount', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', MockEventSource)
    const ids = ref(['project-a', 'project-b'])
    const wrapper = mount(defineComponent({
      setup() {
        useProjectEvents(ids, () => {})
        return () => h('div')
      }
    }))
    await flushPromises()
    expect(MockEventSource.instances.map(item => item.url).sort()).toEqual([
      '/api/projects/project-a/events',
      '/api/projects/project-b/events'
    ])
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(300)
    expect(MockEventSource.instances.every(item => item.closed)).toBe(true)
  })

  it('drops removed Project streams, ignores bad payloads, and cancels reconnect timers', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', MockEventSource)
    const ids = ref<string[] | undefined>(['project-a', 'project-a', '', 'project-b'])
    const received: string[] = []
    const wrapper = mount(defineComponent({
      setup() {
        useProjectEvents(ids, event => { received.push(event.id) })
        return () => h('div')
      }
    }))
    await flushPromises()
    expect(MockEventSource.instances.map(item => item.url).sort()).toEqual([
      '/api/projects/project-a/events',
      '/api/projects/project-b/events'
    ])

    MockEventSource.instances[0]?.emit('not-json')
    MockEventSource.instances[0]?.onmessage?.({ data: '{"no":"event"}' } as MessageEvent<string>)
    MockEventSource.instances[0]?.onmessage?.({ data: '{' } as MessageEvent<string>)
    await flushPromises()
    expect(received).toEqual([])

    ids.value = ['project-a']
    await flushPromises()
    expect(MockEventSource.instances.find(item => item.url.includes('project-b'))?.closed).toBe(true)

    MockEventSource.instances.find(item => !item.closed)?.fail()
    ids.value = undefined
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(received).toEqual([])
    wrapper.unmount()
  })

  it('drops afterId and refreshes on a resync control frame', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', MockEventSource)
    const received: string[] = []
    const wrapper = mount(defineComponent({
      setup() {
        useProjectEvents('project-a', event => { received.push(event.type) })
        return () => h('div')
      }
    }))
    await flushPromises()
    MockEventSource.instances[0]?.emit(event({ id: 'evt-1', type: 'issue.created', sequence: null }))
    await flushPromises()
    MockEventSource.instances[0]?.emitNamed('resync')
    await flushPromises()
    expect(received).toEqual(['issue.created', 'project.resync'])

    MockEventSource.instances[0]?.fail()
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(MockEventSource.instances.at(-1)?.url).toBe('/api/projects/project-a/events')
    wrapper.unmount()
  })

  it('serializes handlers and swallows handler failures', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', MockEventSource)
    let inflight = 0
    let max = 0
    const seen: string[] = []
    const wrapper = mount(defineComponent({
      setup() {
        useProjectEvents('project-a', async event => {
          inflight++
          max = Math.max(max, inflight)
          await new Promise(resolve => setTimeout(resolve, 50))
          inflight--
          seen.push(event.id)
          if (event.id === 'boom') throw new Error('refresh failed')
        })
        return () => h('div')
      }
    }))
    await flushPromises()
    MockEventSource.instances[0]?.emit(event({ id: 'evt-1', type: 'issue.created', sequence: null }))
    MockEventSource.instances[0]?.emit(event({ id: 'boom', type: 'issue.updated', sequence: null }))
    MockEventSource.instances[0]?.emit(event({ id: 'evt-2', type: 'issue.updated', sequence: null }))
    await vi.advanceTimersByTimeAsync(200)
    await flushPromises()
    expect(max).toBe(1)
    expect(seen).toEqual(['evt-1', 'boom', 'evt-2'])
    wrapper.unmount()
  })

  it('replaces a pending reconnect timer when onerror fires twice', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('EventSource', MockEventSource)
    const wrapper = mount(defineComponent({
      setup() {
        useProjectEvents('project-a', () => {})
        return () => h('div')
      }
    }))
    await flushPromises()
    const source = MockEventSource.instances[0]
    source?.fail()
    source?.fail()
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(MockEventSource.instances).toHaveLength(2)
    expect(MockEventSource.instances.at(-1)?.url).toBe('/api/projects/project-a/events')
    wrapper.unmount()
  })
})

