import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import IssueDiscussionTimeline from '../app/components/IssueDiscussionTimeline.vue'
import { uiStubs } from './ui-stubs'

const root = {
  id: '11111111-1111-4111-8111-111111111111',
  issueId: 'AB-1',
  parentCommentId: null,
  author: { type: 'HUMAN' as const, id: 'user-1', name: 'Alex' },
  body: 'Root **comment**',
  createdAt: '2026-09-19T08:00:00Z',
  updatedAt: '2026-09-19T08:00:00Z'
}

const reply = {
  id: '22222222-2222-4222-8222-222222222222',
  issueId: 'AB-1',
  parentCommentId: root.id,
  author: { type: 'HUMAN' as const, id: 'user-2', name: 'Sam' },
  body: 'Reply with `code`',
  createdAt: '2026-09-19T08:01:00Z',
  updatedAt: '2026-09-19T08:01:00Z'
}

const activity = {
  id: '33333333-3333-4333-8333-333333333333',
  schemaVersion: 1,
  type: 'issue.updated',
  occurredAt: '2026-09-19T07:59:00Z',
  projectId: 'p',
  issueId: '77777777-7777-4777-8777-777777777777',
  runId: null,
  sequence: null,
  agentId: null,
  workspaceId: null,
  runtimeInstanceId: null,
  correlationId: null,
  parentEventId: null,
  actor: {},
  payload: { message: 'Issue updated' }
}

afterEach(() => vi.unstubAllGlobals())

describe('IssueDiscussionTimeline', () => {
  it('renders activity, threaded comments, posts replies, and reloads durable state', async () => {
    let timeline = [
      { kind: 'activity' as const, id: activity.id, occurredAt: activity.occurredAt, comment: null, activity },
      { kind: 'comment' as const, id: root.id, occurredAt: root.createdAt, comment: root, activity: null },
      { kind: 'comment' as const, id: reply.id, occurredAt: reply.createdAt, comment: reply, activity: null }
    ]
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (String(path).endsWith('/comments') && options.method === 'POST') {
        const input = JSON.parse(String(options.body)) as { body: string, parentCommentId: string | null }
        const created = {
          ...reply,
          id: '44444444-4444-4444-8444-444444444444',
          parentCommentId: input.parentCommentId,
          body: input.body
        }
        timeline = [...timeline, { kind: 'comment' as const, id: created.id, occurredAt: created.createdAt, comment: created, activity: null }]
        return new Response(JSON.stringify(created), { status: 201 })
      }
      if (String(path).endsWith('/timeline')) return new Response(JSON.stringify(timeline))
      throw new Error(`unexpected request ${path}`)
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: {
        stubs: {
          ...uiStubs,
          IssueDiscussionTimeline: false,
          IdentityAvatar: { props: ['name'], template: '<span>{{ name }}</span>' }
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Issue updated')
    expect(wrapper.text()).toContain('Alex')
    expect(wrapper.text()).toContain('Root comment')
    expect(wrapper.text()).toContain('Replying to Alex')
    expect(wrapper.html()).toContain('<strong>comment</strong>')
    expect(wrapper.html()).toContain('<code>code</code>')

    const replyButton = wrapper.findAll('button').find(button => button.text() === 'Reply')
    expect(replyButton).toBeTruthy()
    await replyButton!.trigger('click')
    expect(wrapper.text()).toContain('Replying to Alex')

    await wrapper.get('textarea').setValue('Follow-up')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const post = fetch.mock.calls.find(([path, options]) => String(path).endsWith('/comments') && options?.method === 'POST')
    expect(post).toBeTruthy()
    expect(JSON.parse(String(post![1]?.body))).toEqual({ body: 'Follow-up', parentCommentId: root.id })
    expect(wrapper.text()).toContain('Follow-up')
    expect(fetch.mock.calls.filter(([path]) => String(path).endsWith('/timeline')).length).toBeGreaterThanOrEqual(2)
    wrapper.unmount()
  })

  it('shows an empty read-only state without mutation controls', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([]))))
    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1', canMutate: false },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false } }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('No discussion yet')
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text().includes('Post'))).toBe(false)
    wrapper.unmount()
  })
})
