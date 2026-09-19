import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import IssueDetail from '../app/components/IssueDetail.vue'
import { MockEventSource, event } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

afterEach(() => {
  MockEventSource.reset()
  vi.unstubAllGlobals()
})

it('refreshes the Issue discussion projection on comment create and lifecycle Events', async () => {
  vi.stubGlobal('EventSource', MockEventSource)
  vi.stubGlobal('fetch', vi.fn(async (path: string) => {
    const url = String(path)
    if (url.endsWith('/assignees')) return new Response(JSON.stringify([]))
    if (url.endsWith('/runs')) return new Response(JSON.stringify([]))
    if (url.endsWith('/execution')) return new Response(JSON.stringify({ state: 'NOT_AGENT_OWNED', canStart: false, executionAgent: null, activeRun: null }))
    if (url.endsWith('/relationships')) return new Response(JSON.stringify([]))
    if (url.endsWith('/timeline')) return new Response(JSON.stringify([]))
    if (url.includes('/issues?') || url.endsWith('/issues')) return new Response(JSON.stringify([]))
    return new Response(JSON.stringify({
      id: 'AB-1', projectId: 'p', number: 1, title: 'Issue', description: '', status: 'TODO', priority: 0,
      assignedTo: null, createdBy: null, createdAt: '2026-09-19T08:00:00Z', updatedAt: '2026-09-19T08:00:00Z',
      currentBranch: null, lastEvent: null
    }))
  }))

  const refresh = vi.fn(async () => {})
  const DiscussionStub = defineComponent({
    setup(_, { expose }) {
      expose({ refresh })
      return () => h('div', { 'data-discussion': '' })
    }
  })

  const wrapper = mount(IssueDetail, {
    props: { projectId: 'p', issueId: 'AB-1' },
    global: { stubs: { ...uiStubs, IssueDiscussionTimeline: DiscussionStub } }
  })
  await flushPromises()
  refresh.mockClear()

  MockEventSource.instances[0]?.emit(event({
    id: 'comment-event',
    type: 'issue.comment_created',
    issueId: '77777777-7777-4777-8777-777777777777',
    sequence: null
  }))
  await flushPromises()

  expect(refresh).toHaveBeenCalledTimes(1)

  MockEventSource.instances[0]?.emit(event({
    id: 'comment-change-event',
    type: 'issue.comment_changed',
    issueId: '77777777-7777-4777-8777-777777777777',
    sequence: null
  }))
  await flushPromises()

  expect(refresh).toHaveBeenCalledTimes(2)
  wrapper.unmount()
})
