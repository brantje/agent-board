import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import IssueDetail from '../app/components/IssueDetail.vue'
import { uiStubs } from './ui-stubs'

const issue = {
  id: 'AB-12',
  number: 12,
  projectId: 'p',
  title: 'Fix scheduler',
  description: 'Persist leases',
  status: 'TODO',
  priority: 0,
  assignedTo: null as null | { type: 'AGENT'; id: string; name: string },
  createdBy: null,
  createdAt: '',
  updatedAt: '',
  currentBranch: null,
  lastEvent: null
}
const agent = { type: 'AGENT' as const, id: 'a', name: 'Coder' }
const queuedRun = {
  id: 'run-new',
  projectId: 'p',
  issueId: issue.id,
  workspaceId: 'w',
  agentId: agent.id,
  attempt: 2,
  status: 'QUEUED',
  queueReason: 'capacity',
  failureReason: null,
  createdAt: '',
  startedAt: null,
  completedAt: null,
  updatedAt: '',
  currentBranch: null
}
const historicalRun = {
  ...queuedRun,
  id: 'run-old',
  attempt: 1,
  status: 'COMPLETED',
  queueReason: null,
  completedAt: '2026-09-14T18:00:00Z'
}
const global = {
  stubs: {
    ...uiStubs,
    IdentityAvatar: { props: ['name'], template: '<span>{{ name }}</span>' },
    IssueEditor: { template: '<div />' },
    IssueRelationships: { template: '<section>Relationships</section>' },
    QuestionPanel: { template: '<section>Questions</section>' },
    RunStatus: { props: ['label'], template: '<span>{{ label }}</span>' },
    WorkspaceBranch: { props: ['branch'], template: '<span>{{ branch }}</span>' },
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
  }
}

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status })
const assignmentForm = (wrapper: ReturnType<typeof mount>) => wrapper.findAll('form').find(form => form.find('[data-field=assignee]').exists())!

async function assignAgent(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('[data-field=assignee] select').setValue('AGENT:a')
  await assignmentForm(wrapper).trigger('submit')
  await flushPromises()
}

async function mountDetail(fetch: ReturnType<typeof vi.fn>) {
  vi.stubGlobal('fetch', fetch)
  const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('Issue Agent assignment execution state', () => {
  it('keeps the generic empty Runs state for an Issue with no assignment action', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) return json([])
      return json(issue)
    })
    const wrapper = await mountDetail(fetch)

    expect(wrapper.text()).toContain('No Runs yet')
    expect(wrapper.text()).not.toContain('No new execution attempt was created')
    expect(wrapper.text()).not.toContain('Run creation could not be confirmed')
    wrapper.unmount()
  })

  it('explains successful Agent assignment when no execution attempt is created', async () => {
    let current = { ...issue }
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path.endsWith('/assignment') && options.method === 'POST') {
        current = { ...issue, assignedTo: agent }
        return json({ issue: current })
      }
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) return json([])
      return json(current)
    })
    const wrapper = await mountDetail(fetch)

    await assignAgent(wrapper)

    const text = wrapper.text()
    expect(text).toContain('Assignment accepted')
    expect(text).toContain('Coder')
    expect(text).toContain('Ownership updated.')
    expect(text).toContain('No new execution attempt was created')
    expect(text).toContain('assignment did not start execution')
    expect(text).toContain('Start a Run to validate and begin execution')
    expect(text).not.toContain('Queue reason')
    expect(text).not.toContain('Provider')
    expect(text).not.toContain('Model Profile')
    expect(text).not.toContain('not runnable')
    wrapper.unmount()
  })

  it('shows an assignment-created queued Run and suppresses the no-execution explanation', async () => {
    let current = { ...issue }
    let runHistory: (typeof queuedRun)[] = []
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path.endsWith('/assignment') && options.method === 'POST') {
        current = { ...issue, assignedTo: agent }
        runHistory = [queuedRun]
        return json({ issue: current })
      }
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) return json(runHistory)
      return json(current)
    })
    const wrapper = await mountDetail(fetch)

    await assignAgent(wrapper)

    const text = wrapper.text()
    expect(text).toContain('Assignment accepted')
    expect(text).toContain('Attempt 2')
    expect(text).toContain('Queued')
    expect(text).toContain('Queue reason: capacity')
    expect(text).not.toContain('No new execution attempt was created')
    expect(text).not.toContain('Run creation could not be confirmed')
    wrapper.unmount()
  })

  it('does not mistake historical Run history for a new assignment execution attempt', async () => {
    let current = { ...issue }
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path.endsWith('/assignment') && options.method === 'POST') {
        current = { ...issue, assignedTo: agent }
        return json({ issue: current })
      }
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) return json([historicalRun])
      return json(current)
    })
    const wrapper = await mountDetail(fetch)

    await assignAgent(wrapper)

    expect(wrapper.text()).toContain('Attempt 1')
    expect(wrapper.text()).toContain('No new execution attempt was created')
    wrapper.unmount()
  })

  it('keeps assignment successful when the follow-up Runs refresh fails', async () => {
    let current = { ...issue }
    let runReads = 0
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path.endsWith('/assignment') && options.method === 'POST') {
        current = { ...issue, assignedTo: agent }
        return json({ issue: current })
      }
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) {
        runReads++
        return runReads === 1
          ? json([])
          : json({ error: { code: 'internal_error', message: 'runs unavailable' } }, 500)
      }
      return json(current)
    })
    const wrapper = await mountDetail(fetch)

    await assignAgent(wrapper)

    const text = wrapper.text()
    expect(text).toContain('Assignment accepted')
    expect(text).toContain('Coder')
    expect(text).toContain('Ownership updated.')
    expect(text).toContain('Run creation could not be confirmed from the current Run state')
    expect(text).not.toContain('Unable to update assignee')
    expect(text).not.toContain('No new execution attempt was created')
    wrapper.unmount()
  })
})
