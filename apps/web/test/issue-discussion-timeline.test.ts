import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import IssueDiscussionTimeline from '../app/components/IssueDiscussionTimeline.vue'
import { uiStubs } from './ui-stubs'

const root = {
  id: '11111111-1111-4111-8111-111111111111',
  issueId: 'AB-1',
  parentCommentId: null,
  sourceRunId: null,
  author: { type: 'HUMAN' as const, id: 'user-1', name: 'Alex' },
  body: 'Root **comment**',
  deletedAt: null,
  resolvedAt: null,
  resolvedBy: null,
  reactions: [],
  createdAt: '2026-09-19T08:00:00Z',
  updatedAt: '2026-09-19T08:00:00Z'
}

const reply = {
  id: '22222222-2222-4222-8222-222222222222',
  issueId: 'AB-1',
  parentCommentId: root.id,
  sourceRunId: null,
  author: { type: 'HUMAN' as const, id: 'user-2', name: 'Sam' },
  body: 'Reply with `code`',
  deletedAt: null,
  resolvedAt: null,
  resolvedBy: null,
  reactions: [],
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

function stubAuth(userId = 'user-1') {
  vi.stubGlobal('useAuth', () => ({ user: { value: { id: userId } } }))
}

describe('IssueDiscussionTimeline', () => {
  it('renders activity, threaded comments, posts replies, and reloads durable state', async () => {
    stubAuth()
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

  it('renders Agent authorship and links authoritative source Run provenance', async () => {
    stubAuth()
    const agentComment = {
      ...root,
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      sourceRunId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
      author: { type: 'AGENT' as const, id: 'agent-1', name: 'Implementation Agent' },
      body: 'Durable finding',
    }
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([
      { kind: 'comment', id: agentComment.id, occurredAt: agentComment.createdAt, comment: agentComment, activity: null }
    ]))))

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: {
        stubs: {
          ...uiStubs,
          IssueDiscussionTimeline: false,
          IdentityAvatar: { props: ['kind', 'name'], template: '<span :data-kind="kind">{{ name }}</span>' },
          NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Implementation Agent')
    expect(wrapper.text()).toContain('Agent')
    expect(wrapper.text()).toContain('via Run')
    expect(wrapper.find('[data-kind="agent"]').exists()).toBe(true)
    expect(wrapper.find('a').attributes('href')).toBe('/projects/p/runs/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb')
    expect(wrapper.findAll('button').some(button => button.text() === 'Edit' || button.text() === 'Delete')).toBe(false)
    wrapper.unmount()
  })
  it('surfaces submit failures and allows cancelling a reply', async () => {
    stubAuth()
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (String(path).endsWith('/timeline')) {
        return new Response(JSON.stringify([
          { kind: 'comment', id: root.id, occurredAt: root.createdAt, comment: root, activity: null }
        ]))
      }
      if (String(path).endsWith('/comments') && options.method === 'POST') {
        return new Response(JSON.stringify({ error: { code: 'internal_error', message: 'posting failed' } }), { status: 500 })
      }
      throw new Error(`unexpected request ${path}`)
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()

    const replyButton = wrapper.findAll('button').find(button => button.text() === 'Reply')
    await replyButton!.trigger('click')
    expect(wrapper.text()).toContain('Replying to Alex')
    const cancel = wrapper.findAll('button').find(button => button.text() === 'Cancel reply')
    await cancel!.trigger('click')
    expect(wrapper.text()).not.toContain('Replying to Alex')

    await wrapper.get('textarea').setValue('Will fail')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Unable to post comment')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Will fail')
    wrapper.unmount()
  })

  it('shows an empty read-only state without mutation controls', async () => {
    stubAuth()
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
  it('edits, resolves, reacts, reopens, and tombstones while preserving replies', async () => {
    stubAuth()
    let timeline = [
      { kind: 'comment' as const, id: root.id, occurredAt: root.createdAt, comment: { ...root }, activity: null },
      { kind: 'comment' as const, id: reply.id, occurredAt: reply.createdAt, comment: { ...reply }, activity: null }
    ]

    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      const url = String(path)
      const method = options.method || 'GET'
      const rootEntry = timeline[0]!
      if (url.endsWith('/timeline')) return new Response(JSON.stringify(timeline))
      if (url.endsWith('/comments/' + root.id) && method === 'PATCH') {
        const input = JSON.parse(String(options.body)) as { body: string }
        rootEntry.comment = { ...rootEntry.comment!, body: input.body, updatedAt: '2026-09-19T08:05:00Z' }
        return new Response(JSON.stringify(rootEntry.comment))
      }
      if (url.endsWith('/comments/' + root.id + '/resolution') && method === 'PUT') {
        rootEntry.comment = {
          ...rootEntry.comment!,
          resolvedAt: '2026-09-19T08:06:00Z',
          resolvedBy: { id: 'user-1', name: 'Alex' }
        }
        return new Response(JSON.stringify(rootEntry.comment))
      }
      if (url.endsWith('/comments/' + root.id + '/resolution') && method === 'DELETE') {
        rootEntry.comment = { ...rootEntry.comment!, resolvedAt: null, resolvedBy: null }
        return new Response(JSON.stringify(rootEntry.comment))
      }
      if (url.endsWith('/comments/' + root.id + '/reactions/HEART') && method === 'PUT') {
        rootEntry.comment = {
          ...rootEntry.comment!,
          reactions: [{ reaction: 'HEART' as const, count: 1, reactedByCurrentUser: true }]
        }
        return new Response(null, { status: 204 })
      }
      if (url.endsWith('/comments/' + root.id + '/reactions/HEART') && method === 'DELETE') {
        rootEntry.comment = { ...rootEntry.comment!, reactions: [] }
        return new Response(null, { status: 204 })
      }
      if (url.endsWith('/comments/' + root.id) && method === 'DELETE') {
        rootEntry.comment = {
          ...rootEntry.comment!,
          body: null,
          deletedAt: '2026-09-19T08:07:00Z',
          resolvedAt: null,
          resolvedBy: null,
          reactions: []
        }
        return new Response(null, { status: 204 })
      }
      throw new Error(`unexpected request ${method} ${url}`)
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

    await wrapper.findAll('button').find(button => button.text() === 'Edit')!.trigger('click')
    await wrapper.find('article textarea').setValue('Edited root')
    await wrapper.findAll('button').find(button => button.text() === 'Save edit')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Edited root')
    expect(wrapper.text()).toContain('edited')

    await wrapper.findAll('button').find(button => button.text() === 'Resolve')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Resolved by Alex')
    expect(wrapper.findAll('button').some(button => button.text() === 'Reopen')).toBe(true)

    const heart = wrapper.findAll('button').find(button => button.text() === '❤️')
    expect(heart).toBeTruthy()
    await heart!.trigger('click')
    await flushPromises()
    const reactedHeart = wrapper.findAll('button').find(button => button.text() === '❤️ 1')
    expect(reactedHeart).toBeTruthy()
    await reactedHeart!.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('button').some(button => button.text() === '❤️ 1')).toBe(false)

    await wrapper.findAll('button').find(button => button.text() === 'Reopen')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('Resolved by Alex')

    await wrapper.findAll('button').find(button => button.text() === 'Delete')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Comment deleted')
    expect(wrapper.text()).toContain('Reply with code')
    expect(wrapper.findAll('button').some(button => button.text() === 'Edit')).toBe(false)
    expect(wrapper.html()).not.toContain('Edited root')
    wrapper.unmount()
  })

  it('surfaces lifecycle mutation failures without discarding the edit', async () => {
    stubAuth()
    vi.stubGlobal('fetch', vi.fn(async (path: string, options: RequestInit = {}) => {
      if (String(path).endsWith('/timeline')) {
        return new Response(JSON.stringify([
          { kind: 'comment', id: root.id, occurredAt: root.createdAt, comment: root, activity: null }
        ]))
      }
      if (options.method === 'PATCH') {
        return new Response(JSON.stringify({ error: { code: 'internal_error', message: 'edit failed' } }), { status: 500 })
      }
      throw new Error(`unexpected request ${path}`)
    }))

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Edit')!.trigger('click')
    await wrapper.find('article textarea').setValue('Keep this edit')
    await wrapper.findAll('button').find(button => button.text() === 'Save edit')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Unable to update comment')
    expect((wrapper.find('article textarea').element as HTMLTextAreaElement).value).toBe('Keep this edit')
    wrapper.unmount()
  })

})
