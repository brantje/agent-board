import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import IssueDetail from '../app/components/IssueDetail.vue'
import { uiStubs } from './ui-stubs'

const issue = {
  id: 'AB-12', number: 12, projectId: 'p', title: 'Fix scheduler', description: 'Persist leases', status: 'TODO', priority: 0,
  assignedTo: null as null | { type: 'AGENT'; id: string; name: string }, createdBy: null, createdAt: '', updatedAt: '', currentBranch: null, lastEvent: null
}
const agent = { type: 'AGENT' as const, id: 'a', name: 'Coder' }
const queuedRun = {
  id: 'run-new', projectId: 'p', issueId: issue.id, workspaceId: 'w', agentId: agent.id, attempt: 2, status: 'QUEUED',
  queueReason: 'workspace_occupied', failureReason: null, createdAt: '', startedAt: null, completedAt: null, updatedAt: '', currentBranch: null
}
const global = { stubs: { ...uiStubs, IdentityAvatar: { props: ['name'], template: '<span>{{ name }}</span>' }, IssueEditor: { template: '<div />' }, IssueRelationships: { template: '<section>Relationships</section>' }, QuestionPanel: { template: '<section>Questions</section>' }, RunStatus: { props: ['label'], template: '<span>{{ label }}</span>' }, WorkspaceBranch: { props: ['branch'], template: '<span>{{ branch }}</span>' }, NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status })
const assignmentForm = (wrapper: ReturnType<typeof mount>) => wrapper.findAll('form').find(form => form.find('[data-field=assignee]').exists())!

async function mountDetail(fetch: ReturnType<typeof vi.fn>, current = issue) {
  vi.stubGlobal('fetch', fetch)
  const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: current.id }, global })
  await flushPromises()
  return wrapper
}
afterEach(() => vi.unstubAllGlobals())

describe('Issue Agent execution state', () => {
  it('leaves User/unassigned Board usage free of execution messaging', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) return json([])
      if (path.endsWith('/execution')) return json({ state: 'NOT_AGENT_OWNED', canStart: false, activeRun: null })
      return json(issue)
    })
    const wrapper = await mountDetail(fetch)
    expect(wrapper.text()).toContain('No Runs yet')
    expect(wrapper.text()).not.toContain('Execution unavailable')
    expect(wrapper.text()).not.toContain('Execution available')
    wrapper.unmount()
  })

  it('shows invalid Agent configuration on a fresh load', async () => {
    const current = { ...issue, assignedTo: agent }
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) return json([])
      if (path.endsWith('/execution')) return json({ state: 'CONFIGURATION_UNAVAILABLE', canStart: false, activeRun: null })
      return json(current)
    })
    const wrapper = await mountDetail(fetch, current)
    expect(wrapper.text()).toContain('Execution unavailable')
    expect(wrapper.text()).toContain('execution configuration prevents a new Run')
    expect(wrapper.findAll('button').some(button => button.text().includes('Start Run'))).toBe(false)
    wrapper.unmount()
  })

  it('refreshes authoritative execution state after assignment instead of inferring from Run IDs', async () => {
    let current = { ...issue }
    let state = { state: 'NOT_AGENT_OWNED', canStart: false, activeRun: null as typeof queuedRun | null }
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path.endsWith('/assignment') && options.method === 'POST') {
        current = { ...issue, assignedTo: agent }
        state = { state: 'CONFIGURATION_UNAVAILABLE', canStart: false, activeRun: null }
        return json({ issue: current })
      }
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) return json([])
      if (path.endsWith('/execution')) return json(state)
      return json(current)
    })
    const wrapper = await mountDetail(fetch)
    await wrapper.get('[data-field=assignee] select').setValue('AGENT:a')
    await assignmentForm(wrapper).trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Assignment accepted')
    expect(wrapper.text()).toContain('Ownership updated.')
    expect(wrapper.text()).toContain('Execution unavailable')
    expect(wrapper.text()).not.toContain('No new execution attempt was created')
    wrapper.unmount()
  })

  it('shows a queued scheduler wait as active execution, not invalid configuration', async () => {
    const current = { ...issue, assignedTo: agent }
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) return json([queuedRun])
      if (path.endsWith('/execution')) return json({ state: 'ACTIVE', canStart: false, activeRun: queuedRun })
      return json(current)
    })
    const wrapper = await mountDetail(fetch, current)
    expect(wrapper.text()).toContain('Execution queued')
    expect(wrapper.text()).toContain('Queue reason: workspace_occupied')
    expect(wrapper.text()).not.toContain('Execution unavailable')
    wrapper.unmount()
  })

  it('presents Start Run from backend eligibility', async () => {
    const current = { ...issue, assignedTo: agent }
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return json([agent])
      if (path.endsWith('/runs')) return json([])
      if (path.endsWith('/execution')) return json({ state: 'READY', canStart: true, activeRun: null })
      return json(current)
    })
    const wrapper = await mountDetail(fetch, current)
    expect(wrapper.text()).toContain('Execution available')
    expect(wrapper.findAll('button').some(button => button.text().includes('Start Run'))).toBe(true)
    wrapper.unmount()
  })
})
