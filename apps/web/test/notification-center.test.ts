import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import NotificationCenter from '../app/components/NotificationCenter.vue'
import { event, MockEventSource } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const first = {
  id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  kind: 'COMMENT_REPLY' as const,
  projectId: 'project-a',
  projectName: 'Workspace',
  issueId: '11111111-1111-4111-8111-111111111111',
  issueKey: 'AB-1',
  issueTitle: 'Fix the board',
  sourceCommentId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
  commentAuthorType: 'HUMAN' as const,
  commentAuthorId: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc',
  commentAuthorName: 'Sam',
  preview: 'Please review this change.',
  createdAt: '2026-09-25T10:00:00Z',
  readAt: null
}

const read = { ...first, id: 'dddddddd-dddd-4ddd-8ddd-dddddddddddd', kind: 'ISSUE_COMMENT' as const, readAt: '2026-09-25T09:00:00Z', sourceCommentId: 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee' }

const global = {
  stubs: {
    ...uiStubs,
    UPopover: { props: ['open'], template: '<div><slot/><div v-if="open"><slot name="content"/></div></div>' },
    NuxtLink: { props: ['to'], template: '<a :href="to" @click="$emit(\'click\')"><slot/></a>', emits: ['click'] }
  }
}

function auth() {
  vi.stubGlobal('useAuth', () => ({ user: ref({ id: 'user-1' }) }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  MockEventSource.reset()
})

describe('NotificationCenter', () => {
  it('shows all notifications, tracks unread state, marks individual items and links to the exact comment anchor', async () => {
    auth()
    vi.stubGlobal('EventSource', MockEventSource)
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path === '/api/notifications') return new Response(JSON.stringify({ notifications: [first, read], unreadCount: 1 }))
      if (path.startsWith('/api/notifications/') && options.method === 'PATCH') return new Response(null, { status: 204 })
      throw new Error(`unexpected request ${options.method || 'GET'} ${path}`)
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(NotificationCenter, { global })
    await flushPromises()
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Sam replied to your comment')
    expect(wrapper.text()).toContain('Sam commented on AB-1')
    const source = wrapper.get('a[href="/projects/project-a/issues/AB-1#comment-bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"]')
    expect(source.exists()).toBe(true)
    await source.trigger('click')
    await flushPromises()
    expect(fetch.mock.calls.some(([path, options]) => path === `/api/notifications/${first.id}` && options?.method === 'PATCH')).toBe(true)
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    await wrapper.get('a[href="/projects/project-a/issues/AB-1#comment-eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    const markUnread = wrapper.findAll('button').find(button => button.text() === 'Mark unread')
    expect(markUnread).toBeDefined()
    await markUnread!.trigger('click')
    await flushPromises()
    expect(fetch.mock.calls.some(([path, options]) => path.startsWith('/api/notifications/') && options?.method === 'PATCH' && options.body === JSON.stringify({ read: false }))).toBe(true)
    wrapper.unmount()
  })

  it('marks all notifications read and refreshes from Project SSE without hiding the list', async () => {
    auth()
    vi.stubGlobal('EventSource', MockEventSource)
    let current = { notifications: [first], unreadCount: 1 }
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path === '/api/notifications') return new Response(JSON.stringify(current))
      if (path.endsWith('/read-all') && options.method === 'POST') {
        current = { notifications: [{ ...first, readAt: '2026-09-25T11:00:00Z' }], unreadCount: 0 }
        return new Response(JSON.stringify({ updated: 1 }))
      }
      throw new Error(`unexpected request ${options.method || 'GET'} ${path}`)
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(NotificationCenter, { global })
    await flushPromises()
    expect(MockEventSource.instances[0]?.url).toBe('/api/projects/project-a/events')
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'Mark all read')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Sam replied to your comment')
    expect(wrapper.text()).not.toContain('Unread notifications')
    await wrapper.findAll('button').find(button => button.text() === 'Mark all read')!.trigger('click')
    // The list stays available after the mutation and a comment Event reloads it.
    current = { notifications: [{ ...first, id: 'ffffffff-ffff-4fff-8fff-ffffffffffff', readAt: null }], unreadCount: 1 }
    MockEventSource.instances[0]?.emit(event({ id: 'event-status', type: 'issue.status_changed' }))
    MockEventSource.instances[0]?.emit(event({ id: 'event-comment', type: 'issue.comment_created' }))
    await flushPromises()
    expect(wrapper.text()).toContain('Sam replied to your comment')
    wrapper.unmount()
  })

  it('refreshes when the inbox opens so the first notification is not missed', async () => {
    auth()
    let requests = 0
    const fetch = vi.fn(async (path: string) => {
      if (path !== '/api/notifications') throw new Error(`unexpected request ${path}`)
      requests++
      return new Response(JSON.stringify(requests === 1 ? { notifications: [], unreadCount: 0 } : { notifications: [first], unreadCount: 1 }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(NotificationCenter, { global })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Sam replied to your comment')
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Sam replied to your comment')
    expect(requests).toBe(2)
    wrapper.unmount()
  })

  it('renders a recoverable load error', async () => {
    auth()
    let failed = true
    const fetch = vi.fn(async () => failed
      ? new Response(JSON.stringify({ error: { code: 'internal_error' } }), { status: 500 })
      : new Response(JSON.stringify({ notifications: [], unreadCount: 0 })))
    vi.stubGlobal('fetch', fetch)
    const retryGlobal = {
      ...global,
      stubs: {
        ...global.stubs,
        UAlert: { props: ['title', 'description', 'actions'], template: '<div role="alert">{{title}} {{description}}<button v-for="action in actions" :key="action.label" @click="action.onClick()">{{action.label}}</button></div>' }
      }
    }
    const wrapper = mount(NotificationCenter, { global: retryGlobal })
    await flushPromises()
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    expect(wrapper.text()).toContain('Unable to load notifications')
    failed = false
    await wrapper.findAll('button').find(button => button.text() === 'Retry')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('No notifications yet.')
    wrapper.unmount()
  })

  it('does not keep notification state after authentication is cleared', async () => {
    const user = ref<{ id: string } | null>(null)
    vi.stubGlobal('useAuth', () => ({ user }))
    vi.stubGlobal('EventSource', MockEventSource)
    const fetch = vi.fn(async () => new Response(JSON.stringify({ notifications: [first], unreadCount: 1 })))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(NotificationCenter, { global })
    await flushPromises()
    expect(wrapper.find('[data-testid="notification-center-trigger"]').exists()).toBe(false)
    user.value = { id: 'user-1' }
    await flushPromises()
    expect(wrapper.find('[data-testid="notification-center-trigger"]').exists()).toBe(true)
    user.value = null
    await flushPromises()
    expect(wrapper.find('[data-testid="notification-center-trigger"]').exists()).toBe(false)
    MockEventSource.instances[0]?.emit(event({ id: 'event-after-logout', type: 'issue.comment_created' }))
    await flushPromises()
    wrapper.unmount()
  })

  it('shows the large unread badge, empty state and keeps navigation usable when marking read fails', async () => {
    auth()
    vi.stubGlobal('EventSource', MockEventSource)
    let empty = false
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path === '/api/notifications') return new Response(JSON.stringify(empty ? { notifications: [], unreadCount: 0 } : { notifications: [first], unreadCount: 101 }))
      if (path.startsWith('/api/notifications/') && options.method === 'PATCH') return new Response(JSON.stringify({ error: { code: 'internal_error' } }), { status: 500 })
      throw new Error(`unexpected request ${options.method || 'GET'} ${path}`)
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(NotificationCenter, { global })
    await flushPromises()
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    expect(wrapper.text()).toContain('99+')
    await wrapper.get('a[href="/projects/project-a/issues/AB-1#comment-bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"]').trigger('click')
    await flushPromises()
    wrapper.unmount()

    empty = true
    const emptyWrapper = mount(NotificationCenter, { global })
    await flushPromises()
    await emptyWrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    expect(emptyWrapper.text()).toContain('No notifications yet.')
    emptyWrapper.unmount()
  })

  it('shows a loading state while the initial notification request is pending', async () => {
    auth()
    let release!: (response: Response) => void
    const request = new Promise<Response>(resolve => { release = resolve })
    vi.stubGlobal('fetch', vi.fn(() => request))
    const wrapper = mount(NotificationCenter, { global })
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    expect(wrapper.text()).toContain('Loading notifications…')
    release(new Response(JSON.stringify({ notifications: [], unreadCount: 0 })))
    await flushPromises()
    expect(wrapper.text()).toContain('No notifications yet.')
    wrapper.unmount()
  })

  it('ignores a stale notification response after the authenticated user changes', async () => {
    const user = ref<{ id: string }>({ id: 'user-1' })
    vi.stubGlobal('useAuth', () => ({ user }))
    let release!: (response: Response) => void
    let calls = 0
    const firstRequest = new Promise<Response>(resolve => { release = resolve })
    vi.stubGlobal('fetch', vi.fn(() => {
      calls++
      return calls === 1 ? firstRequest : Promise.resolve(new Response(JSON.stringify({ notifications: [], unreadCount: 0 })))
    }))
    const wrapper = mount(NotificationCenter, { global })
    user.value = { id: 'user-2' }
    await flushPromises()
    release(new Response(JSON.stringify({ notifications: [first], unreadCount: 1 })))
    await flushPromises()
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    expect(wrapper.text()).toContain('No notifications yet.')
    expect(calls).toBe(3)
    wrapper.unmount()
  })

  it('does not apply a bulk-read response after the authenticated user changes', async () => {
    const user = ref<{ id: string } | null>({ id: 'user-1' })
    vi.stubGlobal('useAuth', () => ({ user }))
    let resolveReadAll!: (response: Response) => void
    const readAll = new Promise<Response>(resolve => { resolveReadAll = resolve })
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path === '/api/notifications') return new Response(JSON.stringify({ notifications: [first], unreadCount: 1 }))
      if (path.endsWith('/read-all') && options.method === 'POST') return readAll
      throw new Error(`unexpected request ${options.method || 'GET'} ${path}`)
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(NotificationCenter, { global })
    await flushPromises()
    await wrapper.get('[data-testid="notification-center-trigger"]').trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Mark all read')!.trigger('click')
    user.value = { id: 'user-2' }
    await flushPromises()
    resolveReadAll(new Response(JSON.stringify({ updated: 1 })))
    await flushPromises()
    expect(wrapper.text()).toContain('Mark read')
    expect(wrapper.text()).not.toContain('Mark unread')
    wrapper.unmount()
  })
})
