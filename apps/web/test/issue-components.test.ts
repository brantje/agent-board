import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectBoard from '../app/components/ProjectBoard.vue'
import IssueDetail from '../app/components/IssueDetail.vue'
import IssueEditor from '../app/components/IssueEditor.vue'
import IssueCard from '../app/components/IssueCard.vue'
import IdentityAvatar from '../app/components/IdentityAvatar.vue'
import QuestionPanel from '../app/components/QuestionPanel.vue'
import RunStatus from '../app/components/RunStatus.vue'
import { event, MockEventSource, question } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const issue = {
  id: 'AB-12',
  number: 12,
  projectId: 'p',
  title: 'Fix scheduler',
  description: 'Persist leases',
  status: 'TODO',
  priority: 0,
  assignedTo: null,
  createdBy: null,
  createdAt: '',
  updatedAt: '2026-09-13T14:59:00.000Z',
  currentBranch: null,
  lastEvent: null
}
const run = {
  id: 'r',
  projectId: 'p',
  issueId: issue.id,
  workspaceId: 'w',
  agentId: 'a',
  attempt: 1,
  status: 'QUEUED',
  queueReason: 'capacity',
  failureReason: null,
  createdAt: '',
  startedAt: null,
  completedAt: null,
  updatedAt: '',
  currentBranch: null
}
const global = {
  stubs: {
    ...uiStubs,
    IssueCard,
    IdentityAvatar,
    IssueEditor,
    IssueRelationships: { template: '<section>Relationships</section>' },
    QuestionPanel: { props: ['projectId', 'issueId', 'runId'], template: '<section>Questions</section>' },
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
  },
  components: { RunStatus }
}
const button = (wrapper: ReturnType<typeof mount>, label: string) => wrapper.findAll('button').find(value => value.text() === label)!
const hasButton = (wrapper: ReturnType<typeof mount>, label: string) => wrapper.findAll('button').some(value => value.text() === label)
const formForField = (wrapper: ReturnType<typeof mount>, name: string) => wrapper.findAll('form').find(form => form.find(`[data-field=${name}]`).exists())!
const statusOptions = (wrapper: ReturnType<typeof mount>) => wrapper.get('[data-field=status]').findAll('option').map(option => option.text())
const allStatusOptions = ['Select…', 'Backlog', 'Todo', 'In Progress', 'Blocked', 'Review', 'Done']

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  MockEventSource.reset()
})

describe('Issue workflow components', () => {
  it('renders the authoritative card hierarchy and safe links for assigned/unassigned Issues', async () => {
    vi.useFakeTimers()
    try {
      vi.setSystemTime(new Date('2026-09-13T15:00:00.000Z'))
      const wrapper = mount(IssueCard, { props: { issue }, global })
      expect(wrapper.get('a').attributes('href')).toBe('/projects/p/issues/AB-12')
      const text = wrapper.text()
      expect(text.indexOf('AB-12')).toBeLessThan(text.indexOf('Fix scheduler'))
      expect(wrapper.find('[aria-label="Low"]').exists()).toBe(true)
      expect(wrapper.find('[data-icon="i-lucide-signal-low"]').exists()).toBe(true)
      expect(text).not.toContain('Low')
      expect(text).toContain('Updated 1m ago')
      expect(text).not.toContain('Unassigned')
      expect(text).not.toContain('TODO')
      expect(wrapper.find('[data-icon="i-lucide-bot"]').exists()).toBe(false)
      expect(wrapper.find('[data-issue-run-status]').exists()).toBe(false)

      await wrapper.setProps({
        issue: {
          ...issue,
          lastEvent: event({ id: 'evt-q', type: 'question.created', sequence: null, payload: { prompt: 'Choose' } })
        }
      })
      expect(wrapper.text()).not.toContain('Question Created')
      expect(wrapper.text()).not.toContain('TODO')

      await wrapper.setProps({ issue: { ...issue, assignedTo: null }, runStatus: 'RUNNING' })
      expect(wrapper.get('[data-issue-run-status]').text()).toBe('Running')
      expect(wrapper.find('.issue-run-spinner .animate-spin').exists()).toBe(true)

      await wrapper.setProps({ issue: { ...issue, assignedTo: { type: 'AGENT', id: 'a', name: 'Agent' }, priority: 3, description: '' }, runStatus: 'QUEUED' })
      expect(wrapper.find('[aria-label="High"]').exists()).toBe(true)
      expect(wrapper.text()).not.toContain('Persist leases')
      expect(wrapper.find('[aria-label="Agent"]').exists()).toBe(true)
      expect(wrapper.find('[data-icon="i-lucide-bot"]').exists()).toBe(true)
      expect(wrapper.get('[data-issue-run-status]').text()).toBe('Queued')

      await wrapper.setProps({ issue: { ...issue, assignedTo: { type: 'AGENT', id: 'a', name: 'Coder' } }, runStatus: 'RUNNING' })
      expect(wrapper.find('[aria-label="Coder"]').exists()).toBe(true)
      expect(wrapper.text()).toContain('Coder')
      expect(wrapper.get('[data-issue-run-status]').text()).toBe('Running')
      expect(wrapper.find('.issue-run-spinner [data-icon="i-lucide-bot"]').exists()).toBe(true)
      expect(wrapper.find('.issue-run-spinner .animate-spin').exists()).toBe(true)
      expect(wrapper.find('.ring-primary\\/35').exists()).toBe(true)

      await wrapper.setProps({ runStatus: 'FAILED' })
      expect(wrapper.get('[data-issue-run-status]').attributes('data-color')).toBe('error')
      expect(wrapper.get('[data-issue-run-status]').text()).toBe('Failed')
      expect(wrapper.find('.issue-run-spinner').exists()).toBe(false)
    } finally {
      vi.useRealTimers()
    }
  })

  it('renders the public User assignee without an Agent lookup', () => {
    const wrapper = mount(IssueCard, {
      props: { issue: { ...issue, assignedTo: { type: 'USER', id: 'user-1', name: 'Alex' } } }, global
    })
    expect(wrapper.text()).toContain('Alex')
    expect(wrapper.find('[aria-label="Alex"]').exists()).toBe(true)
    expect(wrapper.find('[data-icon="i-lucide-bot"]').exists()).toBe(false)
  })

  it('creates valid Issues and allows every persisted Board status without a client transition graph', async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify(issue)))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueEditor, { props: { projectId: 'p' }, global })

    expect(statusOptions(wrapper)).toEqual(allStatusOptions)
    await wrapper.get('form').trigger('submit')
    expect(fetch).not.toHaveBeenCalled()
    await wrapper.get('input').setValue('New task')
    await wrapper.get('textarea').setValue('Details')
    await wrapper.get('[data-field=status] select').setValue('TODO')
    await wrapper.get('[data-field=priority] select').setValue('3')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.emitted('saved')?.[0]).toEqual([issue])
    expect(fetch.mock.calls[0]?.[1]).toMatchObject({
      method: 'POST',
      body: JSON.stringify({ title: 'New task', description: 'Details', status: 'TODO', priority: 3 })
    })
    await button(wrapper, 'Cancel').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)

    const done = mount(IssueEditor, { props: { projectId: 'p', issue: { ...issue, status: 'DONE' } }, global })
    expect(statusOptions(done)).toEqual(allStatusOptions)
    expect(done.text()).not.toContain('Reopen into Todo')
    await done.get('[data-field=status] select').setValue('IN_PROGRESS')
    await done.get('form').trigger('submit')
    await flushPromises()
    expect(fetch.mock.calls.at(-1)?.[1]).toMatchObject({
      method: 'PATCH',
      body: JSON.stringify({ title: issue.title, description: issue.description, status: 'IN_PROGRESS', priority: 0 })
    })

    const review = mount(IssueEditor, { props: { projectId: 'p', issue: { ...issue, status: 'REVIEW' } }, global })
    expect(statusOptions(review)).toEqual(allStatusOptions)
    expect(review.text()).not.toContain('Review decision')
    await review.get('[data-field=status] select').setValue('DONE')

    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'conflict', message: 'raw detail' } }), { status: 409 })))
    await review.get('form').trigger('submit')
    await flushPromises()
    expect(review.text()).toContain('conflicts with existing state')
    expect(review.text()).not.toContain('raw detail')
  })

  it('renders all Board columns, works without Agents, filters Issues, and refreshes state after creation', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (options.method === 'POST') return new Response(JSON.stringify({ ...issue, id: 'created', title: 'Created' }), { status: 201 })
      if (path.endsWith('/issues')) return new Response(JSON.stringify([issue]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Workspace' }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Workspace / Board')
    expect(wrapper.findAll('header h2')).toHaveLength(6)
    const columns = wrapper.findAll('[data-status]')
    expect(columns.map(column => column.attributes('data-status'))).toEqual(['BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE'])
    expect(columns.map(column => column.get('[data-icon]').attributes('data-icon'))).toEqual([
      'i-lucide-inbox',
      'i-lucide-circle',
      'i-lucide-play',
      'i-lucide-octagon-alert',
      'i-lucide-scan-eye',
      'i-lucide-circle-check'
    ])
    expect(columns.map(column => column.classes().filter(name => name.startsWith('board-column')).sort().join(' '))).toEqual([
      'board-column board-column-backlog',
      'board-column board-column-todo',
      'board-column board-column-in-progress',
      'board-column board-column-blocked',
      'board-column board-column-review',
      'board-column board-column-done'
    ])
    expect(columns.every(column => !column.classes().includes('bg-muted/40'))).toBe(true)
    expect(wrapper.text()).toContain('Backlog')
    expect(wrapper.text()).toContain('Blocked')
    expect(wrapper.text()).toContain('Fix scheduler')
    expect(fetch.mock.calls.some(([path]) => String(path).endsWith('/agents'))).toBe(false)
    await wrapper.get('input').setValue('absent')
    expect(wrapper.text()).not.toContain('Fix scheduler')
    expect(wrapper.text()).toContain('No matching issues')

    await button(wrapper, 'New issue').trigger('click')
    expect(statusOptions(wrapper)).toEqual(allStatusOptions)
    await button(wrapper, 'Cancel').trigger('click')
    expect(wrapper.find('[role=dialog]').exists()).toBe(false)

    await button(wrapper, 'New issue').trigger('click')
    await wrapper.get('[data-field=title] input').setValue('Created')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role=dialog]').exists()).toBe(false)
    expect(fetch.mock.calls.filter(([, options]) => options.method === 'GET').length).toBeGreaterThanOrEqual(6)
    wrapper.unmount()
  })

  it('refreshes persisted Board state on Run and Issue events without projecting Run lifecycle into status', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    let current = {
      ...issue,
      lastEvent: event({ id: 'evt-1', type: 'issue.created', sequence: null })
    }
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/issues')) return new Response(JSON.stringify([current]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Workspace' }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Loading')
    expect(wrapper.get('[data-status=TODO]').text()).toContain('Fix scheduler')

    MockEventSource.instances[0]?.emit(event({ id: 'evt-run', type: 'run.created', sequence: null, issueId: issue.id, runId: run.id }))
    await flushPromises()
    expect(wrapper.get('[data-status=TODO]').text()).toContain('Fix scheduler')
    expect(wrapper.get('[data-status=IN_PROGRESS]').text()).not.toContain('Fix scheduler')

    current = {
      ...issue,
      status: 'IN_PROGRESS',
      lastEvent: event({ id: 'evt-2', type: 'issue.status_changed', sequence: null })
    }
    MockEventSource.instances[0]?.emit(current.lastEvent)
    await flushPromises()
    expect(wrapper.text()).not.toContain('Loading')
    expect(wrapper.get('[data-status=IN_PROGRESS]').text()).toContain('Fix scheduler')
    expect(wrapper.get('[data-status=TODO]').text()).not.toContain('Fix scheduler')
    wrapper.unmount()
  })

  it('keeps the board visible when run status fails to load', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/runs')) {
        return new Response(JSON.stringify({ error: { code: 'internal_error', message: 'runs unavailable' } }), { status: 500 })
      }
      if (path.endsWith('/issues')) return new Response(JSON.stringify([issue]))
      return new Response(JSON.stringify({ id: 'p', name: 'Workspace' }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Workspace / Board')
    expect(wrapper.text()).toContain('Fix scheduler')
    expect(wrapper.text()).toContain('Run status unavailable')
    expect(wrapper.text()).not.toContain('Unable to load')
    wrapper.unmount()
  })

  it('treats Project/Issue reads as scoped Board state and offers retry on failure', async () => {
    let failProject = true
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/projects/p' && failProject) {
        return new Response(JSON.stringify({ error: { code: 'project_not_found', message: 'private path' } }), { status: 404 })
      }
      if (path.endsWith('/issues')) return new Response(JSON.stringify([]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Workspace' }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('unavailable or belongs to another project')
    expect(wrapper.text()).not.toContain('private path')
    failProject = false
    await button(wrapper, 'Retry').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Workspace / Board')
    expect(wrapper.findAll('header h2')).toHaveLength(6)
    wrapper.unmount()
  })

  it('uses the generic assignee directory and preserves automatic Run creation from backend policy', async () => {
    let assignedTo: { type: 'USER' | 'AGENT'; id: string; name: string } | null = null
    let runHistory: typeof run[] = []
    const directory = [
      { type: 'USER' as const, id: 'u', name: 'Alex' },
      { type: 'AGENT' as const, id: 'a', name: 'Coder' }
    ]
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (path.endsWith('/assignment') && options.method === 'POST') {
        const body = JSON.parse(options.body as string) as { assignedTo: { type: 'USER' | 'AGENT'; id: string } | null }
        const selected = body.assignedTo && directory.find(value => value.type === body.assignedTo?.type && value.id === body.assignedTo.id)
        assignedTo = selected ? { ...body.assignedTo!, name: selected.name } : null
        if (body.assignedTo?.type === 'AGENT' && !runHistory.length) runHistory = [run]
        return new Response(JSON.stringify({ issue: { ...issue, assignedTo } }), { status: 200 })
      }
      if (path.endsWith('/assignees')) return new Response(JSON.stringify(directory))
      if (path.endsWith('/runs')) return new Response(JSON.stringify(runHistory))
      return new Response(JSON.stringify({ ...issue, assignedTo }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Priority 0')
    expect(wrapper.text()).toContain('No Runs yet')
    expect(wrapper.get('[data-field=assignee]').findAll('option').map(option => option.text())).toEqual([
      'Select…',
      'Unassigned',
      'Alex · User',
      'Coder · Agent'
    ])
    expect(fetch.mock.calls.some(([path]) => String(path).endsWith('/agents'))).toBe(false)
    await formForField(wrapper, 'assignee').trigger('submit')
    expect(fetch.mock.calls.filter(([path, options]) => String(path).endsWith('/assignment') && options.method === 'POST')).toHaveLength(0)

    await wrapper.get('[data-field=assignee] select').setValue('AGENT:a')
    await formForField(wrapper, 'assignee').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Assignment accepted')
    expect(wrapper.text()).toContain('Board status: Todo')
    expect(wrapper.text()).toContain('Ownership updated.')
    let assignmentCall = fetch.mock.calls.filter(([path, options]) => String(path).endsWith('/assignment') && options.method === 'POST').at(-1)!
    expect(JSON.parse(assignmentCall[1].body as string)).toEqual({ assignedTo: { type: 'AGENT', id: 'a' } })
    expect(wrapper.text()).toContain('Attempt 1')
    expect(wrapper.text()).toContain('Queued')
    expect(wrapper.text()).toContain('Queue reason: capacity')
    expect(hasButton(wrapper, 'Run again')).toBe(true)

    await wrapper.get('[data-field=assignee] select').setValue('USER:u')
    await formForField(wrapper, 'assignee').trigger('submit')
    await flushPromises()
    assignmentCall = fetch.mock.calls.filter(([path, options]) => String(path).endsWith('/assignment') && options.method === 'POST').at(-1)!
    expect(JSON.parse(assignmentCall[1].body as string)).toEqual({ assignedTo: { type: 'USER', id: 'u' } })
    expect(wrapper.text()).toContain('Alex')
    expect(wrapper.text()).toContain('Attempt 1')
    expect(hasButton(wrapper, 'Run again')).toBe(false)

    await wrapper.get('[data-field=assignee] select').setValue('__UNASSIGNED__')
    await formForField(wrapper, 'assignee').trigger('submit')
    await flushPromises()
    assignmentCall = fetch.mock.calls.filter(([path, options]) => String(path).endsWith('/assignment') && options.method === 'POST').at(-1)!
    expect(JSON.parse(assignmentCall[1].body as string)).toEqual({ assignedTo: null })
    expect(wrapper.text()).toContain('Unassigned')
    expect(wrapper.text()).toContain('Attempt 1')

    await button(wrapper, 'Edit issue').trigger('click')
    await button(wrapper, 'Cancel').trigger('click')
    await button(wrapper, 'Edit issue').trigger('click')
    await wrapper.findAll('form').at(-1)!.trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role=dialog]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps Agent ownership visible when explicit execution configuration is invalid', async () => {
    let current = { ...issue, assignedTo: null as null | { type: 'AGENT'; id: string; name: string } }
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (path.endsWith('/assignment') && options.method === 'POST') {
        current = { ...issue, assignedTo: { type: 'AGENT', id: 'a', name: 'Coder' } }
        return new Response(JSON.stringify({ issue: current }))
      }
      if (path.endsWith(`/issues/${issue.id}/runs`) && options.method === 'POST') {
        return new Response(JSON.stringify({ error: { code: 'execution_configuration_invalid', message: 'unsafe backend detail' } }), { status: 422 })
      }
      if (path.endsWith('/assignees')) return new Response(JSON.stringify([{ type: 'AGENT', id: 'a', name: 'Coder' }]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify(current))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
    await flushPromises()

    await wrapper.get('[data-field=assignee] select').setValue('AGENT:a')
    await formForField(wrapper, 'assignee').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Coder')
    expect(wrapper.text()).toContain('No Runs yet')
    expect(hasButton(wrapper, 'Start Run')).toBe(true)

    await button(wrapper, 'Start Run').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Unable to start Run')
    expect(wrapper.text()).toContain('selected execution configuration is not runnable')
    expect(wrapper.text()).not.toContain('unsafe backend detail')
    expect(wrapper.text()).toContain('Coder')
    expect(wrapper.text()).toContain('Board status')
    expect(wrapper.text()).toContain('Todo')
    wrapper.unmount()
  })

  it('shows Start Run only for mutable non-Backlog Issues currently assigned to an Agent', async () => {
    const cases = [
      ['BACKLOG', { type: 'AGENT', id: 'a', name: 'Coder' }, false],
      ['TODO', { type: 'AGENT', id: 'a', name: 'Coder' }, true],
      ['IN_PROGRESS', { type: 'AGENT', id: 'a', name: 'Coder' }, true],
      ['BLOCKED', { type: 'AGENT', id: 'a', name: 'Coder' }, true],
      ['REVIEW', { type: 'AGENT', id: 'a', name: 'Coder' }, true],
      ['DONE', { type: 'AGENT', id: 'a', name: 'Coder' }, true],
      ['TODO', { type: 'USER', id: 'u', name: 'Alex' }, false],
      ['TODO', null, false]
    ] as const

    for (const [status, assignedTo, expected] of cases) {
      const current = { ...issue, status, assignedTo }
      vi.stubGlobal('fetch', vi.fn(async (path: string) => {
        if (path.endsWith('/assignees')) return new Response(JSON.stringify([]))
        if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
        return new Response(JSON.stringify(current))
      }))
      const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
      await flushPromises()
      expect(hasButton(wrapper, 'Start Run')).toBe(expected)
      wrapper.unmount()
    }

    const readonly = { ...issue, assignedTo: { type: 'AGENT' as const, id: 'a', name: 'Coder' } }
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return new Response(JSON.stringify([]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify(readonly))
    }))
    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id, canMutate: false }, global })
    await flushPromises()
    expect(hasButton(wrapper, 'Start Run')).toBe(false)
    wrapper.unmount()
  })

  it('starts a Run without choosing execution resources and renders queued scheduler waiting as success', async () => {
    let runHistory: typeof run[] = []
    const current = { ...issue, assignedTo: { type: 'AGENT' as const, id: 'a', name: 'Coder' } }
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (path.endsWith(`/issues/${issue.id}/runs`) && options.method === 'POST') {
        runHistory = [run]
        return new Response(JSON.stringify(run))
      }
      if (path.endsWith('/assignees')) return new Response(JSON.stringify([{ type: 'AGENT', id: 'a', name: 'Coder' }]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify(runHistory))
      return new Response(JSON.stringify(current))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
    await flushPromises()

    expect(hasButton(wrapper, 'Start Run')).toBe(true)
    await button(wrapper, 'Start Run').trigger('click')
    await flushPromises()

    const startCall = fetch.mock.calls.find(([path, options]) => String(path).endsWith(`/issues/${issue.id}/runs`) && options.method === 'POST')!
    expect(startCall[1].body).toBeUndefined()
    expect(wrapper.text()).toContain('Run accepted')
    expect(wrapper.text()).toContain('Queued')
    expect(wrapper.text()).toContain('Queue reason: capacity')
    expect(wrapper.text()).toContain('Board status')
    expect(wrapper.text()).toContain('Todo')
    expect(hasButton(wrapper, 'Run again')).toBe(true)
    wrapper.unmount()
  })

  it('renders concurrent Runs for different Agents while current ownership stays independent', async () => {
    const current = { ...issue, assignedTo: { type: 'AGENT' as const, id: 'b', name: 'Agent B' } }
    const runA = { ...run, id: 'run-a', agentId: 'a', attempt: 1, status: 'RUNNING', queueReason: null }
    const runB = { ...run, id: 'run-b', agentId: 'b', attempt: 2, status: 'STARTING', queueReason: null }
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return new Response(JSON.stringify([
        { type: 'AGENT', id: 'a', name: 'Agent A' },
        { type: 'AGENT', id: 'b', name: 'Agent B' }
      ]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([runA, runB]))
      return new Response(JSON.stringify(current))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Agent B')
    expect(text).toContain('Attempt 2')
    expect(text).toContain('Attempt 1')
    expect(text.indexOf('Attempt 2')).toBeLessThan(text.indexOf('Attempt 1'))
    expect(wrapper.findAll('button').filter(value => value.text() === 'Open Run')).toHaveLength(2)
    expect(hasButton(wrapper, 'Run again')).toBe(true)
    wrapper.unmount()
  })

  it('allows Agent assignment while DONE and leaves Board status unchanged', async () => {
    const doneIssue = { ...issue, status: 'DONE' }
    let current = doneIssue
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (path.endsWith('/assignment') && options.method === 'POST') {
        current = { ...doneIssue, assignedTo: { type: 'AGENT', id: 'a', name: 'Coder' } } as typeof current
        return new Response(JSON.stringify({ issue: current }))
      }
      if (path.endsWith('/assignees')) return new Response(JSON.stringify([{ type: 'AGENT', id: 'a', name: 'Coder' }]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify(current))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
    await flushPromises()

    expect(button(wrapper, 'Update assignee').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).not.toContain('Reopen this Issue')
    await wrapper.get('[data-field=assignee] select').setValue('AGENT:a')
    expect(button(wrapper, 'Update assignee').attributes('disabled')).toBeUndefined()
    await formForField(wrapper, 'assignee').trigger('submit')
    await flushPromises()

    const assignmentCall = fetch.mock.calls.find(([path, options]) => String(path).endsWith('/assignment') && options.method === 'POST')!
    expect(JSON.parse(assignmentCall[1].body as string)).toEqual({ assignedTo: { type: 'AGENT', id: 'a' } })
    expect(wrapper.text()).toContain('Board status: Done')
    expect(wrapper.text()).toContain('Done')
    expect(hasButton(wrapper, 'Start Run')).toBe(true)
    wrapper.unmount()
  })

  it('refreshes Issue detail and open questions from Project SSE without a loading skeleton', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    let current = { ...issue, title: 'Fix scheduler' }
    let questions = [] as ReturnType<typeof question>[]
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return new Response(JSON.stringify([]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      if (path.includes('/questions')) return new Response(JSON.stringify(questions))
      return new Response(JSON.stringify(current))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueDetail, {
      props: { projectId: 'p', issueId: issue.id },
      global: {
        stubs: {
          ...global.stubs,
          QuestionPanel: false
        },
        components: { QuestionPanel, RunStatus }
      }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Fix scheduler')
    expect(wrapper.text()).not.toContain('Which strategy?')
    expect(MockEventSource.instances[0]?.url).toBe('/api/projects/p/events')

    current = { ...issue, title: 'Live title', status: 'BLOCKED' }
    questions = [question({ issueId: issue.id })]
    MockEventSource.instances[0]?.emit(event({ id: 'evt-q', type: 'question.created', sequence: null }))
    await flushPromises()
    expect(wrapper.text()).not.toContain('Loading')
    expect(wrapper.text()).toContain('Live title')
    expect(wrapper.text()).toContain('Blocked')
    expect(wrapper.text()).toContain('Which strategy?')
    wrapper.unmount()
  })
})
