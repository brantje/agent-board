import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import InboxView from '../app/components/InboxView.vue'
import RunStatus from '../app/components/RunStatus.vue'
import { composeInbox } from '../app/utils/inbox'
import { event, MockEventSource, project, question, review, run } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const global = { stubs: { ...uiStubs, NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } }, components: { RunStatus } }

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  MockEventSource.reset()
})

describe('inbox composition', () => {
  it('keeps blocking OPEN questions, pending reviews, and attention Runs as distinct items', () => {
    const items = composeInbox([{
      project,
      questions: [question(), question({ id: 'q-open', blocking: false, prompt: 'Non-blocking' }), question({ id: 'q-done', status: 'ANSWERED' })],
      reviews: [review(), review({ id: 'r-done', status: 'APPROVED' })],
      runs: [
        { ...run, id: 'fail', status: 'FAILED', failureReason: 'engine' },
        { ...run, id: 'wait', status: 'WAITING_FOR_INPUT' },
        { ...run, id: 'review', status: 'READY_FOR_REVIEW' },
        { ...run, id: 'ok', status: 'RUNNING' }
      ]
    }])
    expect(items.map(item => [item.kind, item.id, item.to])).toEqual([
      ['question', 'question-1', '/projects/project-a/issues/AB-1'],
      ['review', 'review-1', '/projects/project-a/reviews/review-1'],
      ['run', 'fail', '/projects/project-a/runs/fail'],
      ['run', 'wait', '/projects/project-a/runs/wait'],
      ['run', 'review', '/projects/project-a/runs/review']
    ])
    expect(items.some(item => item.id === 'ok' || item.id === 'q-open' || item.id === 'q-done')).toBe(false)
  })
})

describe('InboxView', () => {
  it('composes across projects, keeps partial failures visible, and uses refresh rather than a durable table', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/projects') return new Response(JSON.stringify([project, { ...project, id: 'project-b', name: 'Other' }]))
      if (path.startsWith('/api/projects/project-b/')) return new Response(JSON.stringify({ error: { code: 'project_not_found' } }), { status: 404 })
      if (path.includes('/questions')) return new Response(JSON.stringify([question()]))
      if (path.includes('/reviews')) return new Response(JSON.stringify([review()]))
      if (path.includes('/runs')) return new Response(JSON.stringify([{ ...run, status: 'FAILED', failureReason: 'engine' }]))
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(InboxView, { global })
    await flushPromises()

    expect(wrapper.get('a[href="/projects/project-a/issues/AB-1"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/projects/project-a/reviews/review-1"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/projects/project-a/runs/run-1"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Other')
    expect(wrapper.text()).toContain('unavailable or belongs to another project')
    expect(wrapper.text()).toContain('Which strategy?')
    expect(fetch.mock.calls.some(([path]) => path.includes('status=OPEN'))).toBe(true)
    expect(fetch.mock.calls.some(([path]) => path.includes('status=PENDING'))).toBe(true)
    wrapper.unmount()
  })

  it('shows an empty state when every project returns no attention items', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => new Response(JSON.stringify(path === '/api/projects' ? [project] : []))))
    const wrapper = mount(InboxView, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('Nothing needs attention')
    expect(wrapper.text()).toContain('Blocking questions, pending Reviews, and Runs that need a decision will appear here.')
    expect(wrapper.text()).not.toContain('New work will appear here when it is created.')
    wrapper.unmount()
  })

  it('shows an error when the Project list cannot be loaded', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'project_not_found' } }), { status: 404 })))
    const wrapper = mount(InboxView, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('unavailable or belongs to another project')
    wrapper.unmount()
  })

  it('refreshes composed inbox from Project SSE without a loading skeleton', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    let questions: ReturnType<typeof question>[] = []
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/projects') return new Response(JSON.stringify([project]))
      if (path.includes('/questions')) return new Response(JSON.stringify(questions))
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(InboxView, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('Nothing needs attention')
    expect(MockEventSource.instances[0]?.url).toBe('/api/projects/project-a/events')

    questions = [question()]
    MockEventSource.instances[0]?.emit(event({ id: 'evt-q', type: 'question.created', sequence: null }))
    await flushPromises()
    expect(wrapper.text()).not.toContain('Loading')
    expect(wrapper.text()).not.toContain('Nothing needs attention')
    expect(wrapper.get('a[href="/projects/project-a/issues/AB-1"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Which strategy?')
    wrapper.unmount()
  })
})
