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
  mentions: [],
  implicitTrigger: null,
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
  mentions: [],
  implicitTrigger: null,
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

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function stubAuth(userId = 'user-1') {
  vi.stubGlobal('useAuth', () => ({ user: { value: { id: userId } } }))
}

function deferredResponse() {
  let resolve!: (response: Response) => void
  const promise = new Promise<Response>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

describe('IssueDiscussionTimeline', () => {
  it('renders activity, threaded comments, posts replies, and reloads durable state', async () => {
    stubAuth()
    const requestId = '77777777-7777-4777-8777-777777777777'
    vi.stubGlobal('crypto', { randomUUID: () => requestId })
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
    expect(JSON.parse(String(post![1]?.body))).toEqual({
      body: 'Follow-up',
      parentCommentId: root.id,
      requestId,
      suppressImplicitAgentTrigger: false
    })
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


  it('previews explicit Agent selection, posts stable mention IDs, and renders queued execution', async () => {
    stubAuth()
    const requestId = '99999999-9999-4999-8999-999999999999'
    vi.stubGlobal('crypto', { randomUUID: () => requestId })

    const verifier = {
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      projectId: 'p',
      name: 'Verifier',
      roleInstructions: '',
      engine: 'opencode',
      modelProfileId: 'model-1',
      engineSettings: {},
      concurrencyLimit: 1,
      allowDelegation: true,
      state: 'ENABLED'
    }
    let timeline: unknown[] = []
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      const url = String(path)
      const method = options.method || 'GET'
      if (url.endsWith('/timeline')) return new Response(JSON.stringify(timeline))
      if (url.endsWith('/agents') && method === 'GET') return new Response(JSON.stringify([verifier]))
      if (url.endsWith('/comments/trigger-preview') && method === 'POST') {
        const input = JSON.parse(String(options.body)) as { mentionAgentIds: string[] }
        expect(input.mentionAgentIds).toEqual([verifier.id])
        return new Response(JSON.stringify({
          mentions: [{
            targetAgentId: verifier.id,
            targetAgentName: verifier.name,
            eligible: true,
            reasonCode: null
          }],
          implicit: null
        }))
      }
      if (url.endsWith('/comments') && method === 'POST') {
        const input = JSON.parse(String(options.body)) as {
          body: string
          parentCommentId: string | null
          requestId: string
          mentionAgentIds: string[]
        }
        expect(input).toEqual({
          body: 'Please verify this change',
          parentCommentId: null,
          requestId,
          mentionAgentIds: [verifier.id],
          suppressImplicitAgentTrigger: false
        })
        const created = {
          ...root,
          id: 'abababab-abab-4aba-8aba-abababababab',
          body: input.body,
          mentions: [{
            id: 'bcbcbcbc-bcbc-4bcb-8bcb-bcbcbcbcbcbc',
            targetAgentId: verifier.id,
            targetAgentName: verifier.name,
            outcome: 'QUEUED' as const,
            reasonCode: null,
            delegationId: 'cdcdcdcd-cdcd-4cdc-8dcd-cdcdcdcdcdcd',
            delegatedRunId: 'dededede-dede-4ded-8ded-dededededede'
          }]
        }
        timeline = [{ kind: 'comment', id: created.id, occurredAt: created.createdAt, comment: created, activity: null }]
        return new Response(JSON.stringify(created), { status: 201 })
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
          IdentityAvatar: true,
          NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
        }
      }
    })
    await flushPromises()

    await wrapper.findAll('button').find(button => button.text() === 'Mention Agent')!.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('button').some(button => button.text() === '@Verifier')).toBe(true)

    await wrapper.findAll('button').find(button => button.text() === '@Verifier')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('@Verifier · Eligible to request work')

    await wrapper.get('textarea').setValue('Please verify this change')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('@Verifier')
    expect(wrapper.text()).toContain('Work queued')
    expect(wrapper.text()).toContain('Open delegated Run')
    expect(wrapper.find('a').attributes('href')).toBe('/projects/p/runs/dededede-dede-4ded-8ded-dededededede')
    wrapper.unmount()
  })


  it('renders coalesced and deferred Agent work outcomes distinctly', async () => {
    stubAuth()
    const coalesced = {
      ...root,
      id: '12121212-1212-4121-8121-121212121212',
      mentions: [{
        id: '13131313-1313-4131-8131-131313131313',
        targetAgentId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        targetAgentName: 'Verifier',
        outcome: 'COALESCED' as const,
        reasonCode: null,
        delegationId: '14141414-1414-4141-8141-141414141414',
        delegatedRunId: '15151515-1515-4151-8151-151515151515'
      }]
    }
    const deferred = {
      ...reply,
      id: '16161616-1616-4161-8161-161616161616',
      implicitTrigger: {
        targetAgentId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        targetAgentName: 'Verifier',
        routingReason: 'DIRECT_AGENT_REPLY' as const,
        outcome: 'DEFERRED' as const,
        reasonCode: null,
        delegationId: null,
        delegatedRunId: null
      }
    }
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (String(path).endsWith('/timeline')) {
        return new Response(JSON.stringify([
          { kind: 'comment', id: coalesced.id, occurredAt: coalesced.createdAt, comment: coalesced, activity: null },
          { kind: 'comment', id: deferred.id, occurredAt: deferred.createdAt, comment: deferred, activity: null }
        ]))
      }
      throw new Error(`unexpected request ${path}`)
    }))

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: {
        stubs: {
          ...uiStubs,
          IssueDiscussionTimeline: false,
          IdentityAvatar: true,
          NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Folded into pending Agent work')
    expect(wrapper.text()).toContain('Follow-up saved until current Agent work finishes')
    expect(wrapper.text()).toContain('Open delegated Run')
    wrapper.unmount()
  })

  it('renders safe blocked mention outcomes without a delegated Run link', async () => {
    stubAuth()
    const blocked = {
      ...root,
      mentions: [{
        id: 'abababab-abab-4aba-8aba-abababababab',
        targetAgentId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        targetAgentName: 'Busy Agent',
        outcome: 'BLOCKED' as const,
        reasonCode: 'TARGET_BUSY' as const,
        delegationId: null,
        delegatedRunId: null
      }]
    }
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (String(path).endsWith('/timeline')) {
        return new Response(JSON.stringify([
          { kind: 'comment', id: blocked.id, occurredAt: blocked.createdAt, comment: blocked, activity: null }
        ]))
      }
      throw new Error(`unexpected request ${path}`)
    }))

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('@Busy Agent')
    expect(wrapper.text()).toContain('Agent already has active work on this Issue')
    expect(wrapper.text()).not.toContain('Open delegated Run')
    wrapper.unmount()
  })


  it('filters mention candidates, shows blocked preview reasons, and removes a selection', async () => {
    stubAuth()
    const agents = [
      {
        id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        projectId: 'p',
        name: 'Verifier',
        roleInstructions: '',
        engine: 'opencode',
        modelProfileId: 'model-1',
        engineSettings: {},
        concurrencyLimit: 1,
        allowDelegation: true,
        state: 'ENABLED'
      },
      {
        id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
        projectId: 'p',
        name: 'Builder',
        roleInstructions: '',
        engine: 'opencode',
        modelProfileId: 'model-1',
        engineSettings: {},
        concurrencyLimit: 1,
        allowDelegation: true,
        state: 'ENABLED'
      }
    ]
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      const url = String(path)
      if (url.endsWith('/timeline')) return new Response(JSON.stringify([]))
      if (url.endsWith('/agents')) return new Response(JSON.stringify(agents))
      if (url.endsWith('/comments/trigger-preview') && options.method === 'POST') {
        return new Response(JSON.stringify({
          mentions: [{
            targetAgentId: agents[0]!.id,
            targetAgentName: agents[0]!.name,
            eligible: false,
            reasonCode: 'TARGET_BUSY'
          }],
          implicit: null
        }))
      }
      throw new Error(`unexpected request ${url}`)
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()

    await wrapper.findAll('button').find(button => button.text() === 'Mention Agent')!.trigger('click')
    await flushPromises()
    const filter = wrapper.find('input[type="text"]')
    await filter.setValue('ver')
    expect(wrapper.findAll('button').some(button => button.text() === '@Verifier')).toBe(true)
    expect(wrapper.findAll('button').some(button => button.text() === '@Builder')).toBe(false)

    await wrapper.findAll('button').find(button => button.text() === '@Verifier')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('@Verifier · Agent already has active work on this Issue')

    await wrapper.findAll('button').find(button => button.text() === 'Remove @Verifier')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('Agent already has active work on this Issue')
    wrapper.unmount()
  })

  it('surfaces Agent directory and mention-preview failures without losing the draft', async () => {
    stubAuth()
    let failAgents = true
    const agent = {
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      projectId: 'p',
      name: 'Verifier',
      roleInstructions: '',
      engine: 'opencode',
      modelProfileId: 'model-1',
      engineSettings: {},
      concurrencyLimit: 1,
      allowDelegation: true,
      state: 'ENABLED'
    }
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      const url = String(path)
      if (url.endsWith('/timeline')) return new Response(JSON.stringify([]))
      if (url.endsWith('/agents')) {
        if (failAgents) {
          failAgents = false
          return new Response(JSON.stringify({ error: { code: 'internal_error' } }), { status: 500 })
        }
        return new Response(JSON.stringify([agent]))
      }
      if (url.endsWith('/comments/trigger-preview') && options.method === 'POST') {
        return new Response(JSON.stringify({ error: { code: 'internal_error' } }), { status: 500 })
      }
      throw new Error(`unexpected request ${url}`)
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()
    await wrapper.get('textarea').setValue('Keep this draft')

    await wrapper.findAll('button').find(button => button.text() === 'Mention Agent')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Unable to load Agents')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('Keep this draft')

    // Remount to exercise the preview failure independently from the lazy-load
    // guard after a failed directory request.
    wrapper.unmount()
    const second = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()
    await second.get('textarea').setValue('Still here')
    await second.findAll('button').find(button => button.text() === 'Mention Agent')!.trigger('click')
    await flushPromises()
    await second.findAll('button').find(button => button.text() === '@Verifier')!.trigger('click')
    await flushPromises()
    expect(second.text()).toContain('Trigger preview unavailable')
    expect((second.get('textarea').element as HTMLTextAreaElement).value).toBe('Still here')
    second.unmount()
  })

  it('renders queued and workflow-blocked implicit routing outcomes with their reasons', async () => {
    stubAuth()
    const queued = {
      ...root,
      id: 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee',
      body: 'Queued implicit route',
      implicitTrigger: {
        targetAgentId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        targetAgentName: 'Thread Agent',
        routingReason: 'UNIQUE_THREAD_AGENT' as const,
        outcome: 'QUEUED' as const,
        reasonCode: null,
        delegationId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
        delegatedRunId: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc'
      }
    }
    const blocked = {
      ...root,
      id: 'ffffffff-ffff-4fff-8fff-ffffffffffff',
      body: 'Blocked implicit route',
      implicitTrigger: {
        targetAgentId: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd',
        targetAgentName: '',
        routingReason: 'ISSUE_ASSIGNEE' as const,
        outcome: 'BLOCKED' as const,
        reasonCode: 'WORKFLOW_BLOCKED' as const,
        delegationId: null,
        delegatedRunId: null
      }
    }
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (String(path).endsWith('/timeline')) {
        return new Response(JSON.stringify([
          { kind: 'comment', id: queued.id, occurredAt: queued.createdAt, comment: queued, activity: null },
          { kind: 'comment', id: blocked.id, occurredAt: blocked.createdAt, comment: blocked, activity: null }
        ]))
      }
      throw new Error(`unexpected request ${path}`)
    }))

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: {
        stubs: {
          ...uiStubs,
          IssueDiscussionTimeline: false,
          IdentityAvatar: true,
          NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('@Thread Agent')
    expect(wrapper.text()).toContain('only Agent participating in this discussion')
    expect(wrapper.text()).toContain('Work queued')
    expect(wrapper.find('a').attributes('href')).toBe('/projects/p/runs/cccccccc-cccc-4ccc-8ccc-cccccccccccc')
    expect(wrapper.text()).toContain('@Unavailable Agent')
    expect(wrapper.text()).toContain('current Issue Agent assignee')
    expect(wrapper.text()).toContain('Issue workflow does not allow an implicit Agent wakeup')
    wrapper.unmount()
  })

  it('invalidates an in-flight preview immediately when the draft body changes', async () => {
    stubAuth()
    vi.useFakeTimers()
    const previewA = deferredResponse()
    const previewB = deferredResponse()
    const previewBodies: string[] = []
    vi.stubGlobal('fetch', vi.fn((path: string, options: RequestInit = {}) => {
      const url = String(path)
      if (url.endsWith('/timeline')) return Promise.resolve(new Response(JSON.stringify([])))
      if (url.endsWith('/comments/trigger-preview') && options.method === 'POST') {
        const input = JSON.parse(String(options.body)) as { body: string }
        previewBodies.push(input.body)
        if (input.body === 'draft A') return previewA.promise
        if (input.body === 'draft B') return previewB.promise
        return Promise.resolve(new Response(JSON.stringify({ mentions: [], implicit: null })))
      }
      throw new Error(`unexpected request ${url}`)
    }))

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()

    await wrapper.get('textarea').setValue('draft A')
    await vi.advanceTimersByTimeAsync(250)
    expect(previewBodies).toEqual(['draft A'])

    await wrapper.get('textarea').setValue('draft B')
    expect(wrapper.text()).not.toContain('@Draft A Agent')

    previewA.resolve(new Response(JSON.stringify({
      mentions: [],
      implicit: {
        targetAgentId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        targetAgentName: 'Draft A Agent',
        routingReason: 'ISSUE_ASSIGNEE',
        eligible: true,
        suppressed: false,
        reasonCode: null
      }
    })))
    await flushPromises()

    expect(wrapper.text()).not.toContain('@Draft A Agent')
    expect(previewBodies).toEqual(['draft A'])

    await vi.advanceTimersByTimeAsync(250)
    expect(previewBodies).toEqual(['draft A', 'draft B'])

    previewB.resolve(new Response(JSON.stringify({
      mentions: [],
      implicit: {
        targetAgentId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
        targetAgentName: 'Draft B Agent',
        routingReason: 'ISSUE_ASSIGNEE',
        eligible: true,
        suppressed: false,
        reasonCode: null
      }
    })))
    await flushPromises()

    expect(wrapper.text()).toContain('@Draft B Agent')
    expect(wrapper.text()).toContain('Will request Agent work')
    expect(wrapper.text()).not.toContain('@Draft A Agent')
    wrapper.unmount()
  })

  it('clears a direct-reply preview immediately when switching reply targets', async () => {
    stubAuth()
    const agentA = {
      ...root,
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      sourceRunId: 'abababab-abab-4aba-8aba-abababababab',
      author: { type: 'AGENT' as const, id: 'acacacac-acac-4aca-8aca-acacacacacac', name: 'Agent A' },
      body: 'Agent A response'
    }
    const agentB = {
      ...root,
      id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
      sourceRunId: 'bcbcbcbc-bcbc-4bcb-8bcb-bcbcbcbcbcbc',
      author: { type: 'AGENT' as const, id: 'bdbdbdbd-bdbd-4bdb-8bdb-bdbdbdbdbdbd', name: 'Agent B' },
      body: 'Agent B response'
    }
    const previewB = deferredResponse()
    vi.stubGlobal('fetch', vi.fn((path: string, options: RequestInit = {}) => {
      const url = String(path)
      if (url.endsWith('/timeline')) {
        return Promise.resolve(new Response(JSON.stringify([
          { kind: 'comment', id: agentA.id, occurredAt: agentA.createdAt, comment: agentA, activity: null },
          { kind: 'comment', id: agentB.id, occurredAt: agentB.createdAt, comment: agentB, activity: null }
        ])))
      }
      if (url.endsWith('/comments/trigger-preview') && options.method === 'POST') {
        const input = JSON.parse(String(options.body)) as { parentCommentId: string | null }
        const target = input.parentCommentId === agentA.id ? agentA : agentB
        if (target === agentB) return previewB.promise
        return Promise.resolve(new Response(JSON.stringify({
          mentions: [],
          implicit: {
            targetAgentId: target.author.id,
            targetAgentName: target.author.name,
            routingReason: 'DIRECT_AGENT_REPLY',
            eligible: true,
            suppressed: false,
            reasonCode: null
          }
        })))
      }
      throw new Error(`unexpected request ${url}`)
    }))

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()

    const replyButtons = wrapper.findAll('button').filter(button => button.text() === 'Reply')
    expect(replyButtons).toHaveLength(2)

    await replyButtons[0]!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('@Agent A')
    expect(wrapper.text()).toContain('Will request Agent work')

    await replyButtons[1]!.trigger('click')
    expect(wrapper.text()).toContain('Replying to Agent B')
    expect(wrapper.text()).not.toContain('@Agent A')
    expect(wrapper.text()).not.toContain('Will request Agent work')

    previewB.resolve(new Response(JSON.stringify({
      mentions: [],
      implicit: {
        targetAgentId: agentB.author.id,
        targetAgentName: agentB.author.name,
        routingReason: 'DIRECT_AGENT_REPLY',
        eligible: true,
        suppressed: false,
        reasonCode: null
      }
    })))
    await flushPromises()

    expect(wrapper.text()).toContain('@Agent B')
    expect(wrapper.text()).toContain('Will request Agent work')
    expect(wrapper.text()).not.toContain('@Agent A')
    wrapper.unmount()
  })

  it('debounces trigger previews from the current draft body', async () => {
    stubAuth()
    vi.useFakeTimers()
    const previewBodies: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (path: string, options: RequestInit = {}) => {
      const url = String(path)
      if (url.endsWith('/timeline')) return new Response(JSON.stringify([]))
      if (url.endsWith('/comments/trigger-preview') && options.method === 'POST') {
        const input = JSON.parse(String(options.body)) as { body: string }
        previewBodies.push(input.body)
        return new Response(JSON.stringify({ mentions: [], implicit: null }))
      }
      throw new Error(`unexpected request ${url}`)
    }))

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()

    await wrapper.get('textarea').setValue('first draft')
    await wrapper.get('textarea').setValue('current draft')
    expect(previewBodies).toEqual([])

    await vi.advanceTimersByTimeAsync(249)
    expect(previewBodies).toEqual([])
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()

    expect(previewBodies).toEqual(['current draft'])
    wrapper.unmount()
  })

  it('clears stale routing when the current trigger preview fails', async () => {
    stubAuth()
    const agentComment = {
      ...root,
      id: 'abababab-abab-4aba-8aba-abababababab',
      sourceRunId: 'bcbcbcbc-bcbc-4bcb-8bcb-bcbcbcbcbcbc',
      author: { type: 'AGENT' as const, id: 'cdcdcdcd-cdcd-4cdc-8dcd-cdcdcdcdcdcd', name: 'Routing Agent' },
      body: 'Agent response'
    }
    let previewCalls = 0
    vi.stubGlobal('fetch', vi.fn(async (path: string, options: RequestInit = {}) => {
      const url = String(path)
      if (url.endsWith('/timeline')) {
        return new Response(JSON.stringify([
          { kind: 'comment', id: agentComment.id, occurredAt: agentComment.createdAt, comment: agentComment, activity: null }
        ]))
      }
      if (url.endsWith('/comments/trigger-preview') && options.method === 'POST') {
        previewCalls++
        if (previewCalls === 1) {
          return new Response(JSON.stringify({
            mentions: [],
            implicit: {
              targetAgentId: agentComment.author.id,
              targetAgentName: agentComment.author.name,
              routingReason: 'DIRECT_AGENT_REPLY',
              eligible: true,
              suppressed: false,
              reasonCode: null
            }
          }))
        }
        return new Response(JSON.stringify({ error: { code: 'internal_error', message: 'preview failed' } }), { status: 500 })
      }
      throw new Error(`unexpected request ${url}`)
    }))

    const wrapper = mount(IssueDiscussionTimeline, {
      props: { projectId: 'p', issueId: 'AB-1' },
      global: { stubs: { ...uiStubs, IssueDiscussionTimeline: false, IdentityAvatar: true } }
    })
    await flushPromises()

    await wrapper.findAll('button').find(button => button.text() === 'Reply')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('@Routing Agent')
    expect(wrapper.text()).toContain('Will request Agent work')

    const suppress = wrapper.find('input[type="checkbox"]')
    await suppress.setValue(true)
    await flushPromises()

    expect(wrapper.text()).toContain('Trigger preview unavailable')
    expect(wrapper.text()).not.toContain('@Routing Agent')
    expect(wrapper.text()).not.toContain('Will request Agent work')
    wrapper.unmount()
  })

  it('previews and suppresses a direct-reply implicit Agent trigger while preserving the comment', async () => {
    stubAuth()
    const requestId = '88888888-8888-4888-8888-888888888888'
    vi.stubGlobal('crypto', { randomUUID: () => requestId })
    const agentComment = {
      ...root,
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      sourceRunId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
      author: { type: 'AGENT' as const, id: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', name: 'Builder Agent' },
      body: 'I checked the implementation.'
    }
    let timeline: unknown[] = [
      { kind: 'comment', id: agentComment.id, occurredAt: agentComment.createdAt, comment: agentComment, activity: null }
    ]
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      const url = String(path)
      const method = options.method || 'GET'
      if (url.endsWith('/timeline')) return new Response(JSON.stringify(timeline))
      if (url.endsWith('/comments/trigger-preview') && method === 'POST') {
        const input = JSON.parse(String(options.body)) as { parentCommentId: string | null, suppressImplicitAgentTrigger: boolean, mentionAgentIds: string[] }
        if (input.parentCommentId !== agentComment.id) {
          return new Response(JSON.stringify({ mentions: [], implicit: null }))
        }
        return new Response(JSON.stringify({
          mentions: [],
          implicit: {
            targetAgentId: agentComment.author.id,
            targetAgentName: agentComment.author.name,
            routingReason: 'DIRECT_AGENT_REPLY',
            eligible: !input.suppressImplicitAgentTrigger,
            suppressed: input.suppressImplicitAgentTrigger,
            reasonCode: null
          }
        }))
      }
      if (url.endsWith('/comments') && method === 'POST') {
        const input = JSON.parse(String(options.body)) as {
          body: string
          parentCommentId: string | null
          requestId: string
          suppressImplicitAgentTrigger: boolean
        }
        expect(input).toEqual({
          body: 'Thanks, no need to run again.',
          parentCommentId: agentComment.id,
          requestId,
          suppressImplicitAgentTrigger: true
        })
        const created = {
          ...reply,
          id: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd',
          parentCommentId: agentComment.id,
          body: input.body,
          implicitTrigger: {
            targetAgentId: agentComment.author.id,
            targetAgentName: agentComment.author.name,
            routingReason: 'DIRECT_AGENT_REPLY' as const,
            outcome: 'SUPPRESSED' as const,
            reasonCode: null,
            delegationId: null,
            delegatedRunId: null
          }
        }
        timeline = [...timeline, { kind: 'comment', id: created.id, occurredAt: created.createdAt, comment: created, activity: null }]
        return new Response(JSON.stringify(created), { status: 201 })
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
          IdentityAvatar: true
        }
      }
    })
    await flushPromises()

    await wrapper.findAll('button').find(button => button.text() === 'Reply')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('@Builder Agent')
    expect(wrapper.text()).toContain('replying directly to this Agent')
    expect(wrapper.text()).toContain('Will request Agent work')

    const suppress = wrapper.find('input[type="checkbox"]')
    expect(suppress.exists()).toBe(true)
    await suppress.setValue(true)
    await flushPromises()
    expect(wrapper.text()).toContain('Automatic Agent trigger suppressed')

    await wrapper.get('textarea').setValue('Thanks, no need to run again.')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Automatic Agent trigger suppressed')
    expect(wrapper.text()).not.toContain('Open delegated Run')
    wrapper.unmount()
  })

  it('selects a Squad target and keeps the resolved leader visible', async () => {
    stubAuth()
    const requestId = '99999999-9999-4999-8999-999999999999'
    const squad = {
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', projectId: 'p', name: 'Backend Squad',
      leaderAgentId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', members: [],
      createdAt: root.createdAt, updatedAt: root.updatedAt
    }
    const leader = { id: squad.leaderAgentId, projectId: 'p', name: 'Leader', roleInstructions: '', engine: 'opencode', modelProfileId: 'model', engineSettings: {}, concurrencyLimit: 1, allowDelegation: true, state: 'ENABLED' }
    let timeline: unknown[] = []
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      const url = String(path)
      if (url.endsWith('/timeline')) return new Response(JSON.stringify(timeline))
      if (url.endsWith('/agents')) return new Response(JSON.stringify([leader]))
      if (url.endsWith('/squads')) return new Response(JSON.stringify([squad]))
      if (url.endsWith('/comments/trigger-preview')) {
        const input = JSON.parse(String(options.body)) as { mentionTargets?: Array<{ type: string; id: string }> }
        expect(input.mentionTargets).toEqual([{ type: 'SQUAD', id: squad.id }])
        return new Response(JSON.stringify({ mentions: [{ targetType: 'SQUAD', targetId: squad.id, targetName: squad.name, resolvedAgentId: leader.id, resolvedAgentName: leader.name, eligible: true, reasonCode: null }], implicit: null }))
      }
      if (url.endsWith('/comments')) {
        const input = JSON.parse(String(options.body)) as { mentionTargets?: Array<{ type: string; id: string }> }
        expect(input.mentionTargets).toEqual([{ type: 'SQUAD', id: squad.id }])
        const created = { ...root, body: 'Please investigate', mentions: [{ id: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', targetType: 'SQUAD', targetId: squad.id, targetName: squad.name, resolvedAgentId: leader.id, resolvedAgentName: leader.name, outcome: 'QUEUED', reasonCode: null, delegationId: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd', delegatedRunId: 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee' }] }
        timeline = [{ kind: 'comment', id: created.id, occurredAt: created.createdAt, comment: created, activity: null }]
        return new Response(JSON.stringify(created), { status: 201 })
      }
      throw new Error(`unexpected request ${options.method || 'GET'} ${url}`)
    })
    vi.stubGlobal('fetch', fetch)
    vi.stubGlobal('crypto', { randomUUID: () => requestId })

    const wrapper = mount(IssueDiscussionTimeline, { props: { projectId: 'p', issueId: 'AB-1' }, global: { stubs: { ...uiStubs, IdentityAvatar: true } } })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Mention Agent')!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Mention Agent')!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('@Backend Squad'))!.trigger('click')
    await wrapper.findAll('button').find(button => button.text().includes('Remove @Backend Squad'))!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('@Backend Squad'))!.trigger('click')
    await wrapper.get('textarea').setValue('Please investigate')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('@Backend Squad')
    expect(wrapper.text()).toContain('leader: Leader')
    wrapper.unmount()
  })

  it('keeps Agent mention selection usable when the Squad directory is unavailable', async () => {
    stubAuth()
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (String(path).endsWith('/timeline')) return new Response(JSON.stringify([]))
      if (String(path).endsWith('/agents')) return new Response(JSON.stringify([]))
      if (String(path).endsWith('/squads')) throw new Error('Squads unavailable')
      throw new Error(`unexpected request ${path}`)
    }))
    const wrapper = mount(IssueDiscussionTimeline, { props: { projectId: 'p', issueId: 'AB-1' }, global: { stubs: { ...uiStubs, IdentityAvatar: true } } })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Mention Agent')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('Unable to load Agents')
    wrapper.unmount()
  })

  it('renders an unavailable Squad implicit target distinctly', async () => {
    stubAuth()
    const comment = {
      ...root,
      implicitTrigger: {
        targetType: 'SQUAD', targetId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', targetName: 'Backend Squad',
        resolvedAgentId: null, resolvedAgentName: '', targetAgentId: '', targetAgentName: '',
        routingReason: 'ISSUE_SQUAD_ASSIGNEE', outcome: 'BLOCKED', reasonCode: 'TARGET_UNAVAILABLE', delegationId: null, delegatedRunId: null
      }
    }
    vi.stubGlobal('fetch', vi.fn(async (path: string) => String(path).endsWith('/timeline')
      ? new Response(JSON.stringify([{ kind: 'comment', id: comment.id, occurredAt: comment.createdAt, comment, activity: null }]))
      : new Response(JSON.stringify([]))))
    const wrapper = mount(IssueDiscussionTimeline, { props: { projectId: 'p', issueId: 'AB-1' }, global: { stubs: { ...uiStubs, IdentityAvatar: true } } })
    await flushPromises()
    expect(wrapper.text()).toContain('@Backend Squad')
    expect(wrapper.text()).toContain('current Issue Squad assignee')
    wrapper.unmount()
  })

})
