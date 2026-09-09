import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import IssueCard from '../app/components/IssueCard.vue'
import ProjectBoard from '../app/components/ProjectBoard.vue'
import IssueDetail from '../app/components/IssueDetail.vue'
import WorkspaceBranch from '../app/components/WorkspaceBranch.vue'
import {
  applyCurrentBranchToIssue,
  applyCurrentBranchToIssues,
  applyCurrentBranchToRun,
  currentBranchFromEvent,
  isBoardActivityEvent,
  refetchTargets
} from '../app/utils/events'
import { event, MockEventSource } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const issue = {
  id: 'AB-12',
  number: 12,
  projectId: 'p',
  title: 'Fix scheduler',
  description: 'Persist leases',
  status: 'TODO',
  priority: 0,
  assignedAgentId: null,
  createdAt: '',
  updatedAt: '',
  currentBranch: 'agent-board/AB-12',
  lastEvent: null
}

const global = {
  stubs: {
    ...uiStubs,
    IssueCard,
    WorkspaceBranch,
    IssueRelationships: { template: '<section>Relationships</section>' },
    QuestionPanel: { props: ['projectId', 'issueId', 'runId'], template: '<section>Questions</section>' },
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  MockEventSource.reset()
})

describe('live workspace branch helpers', () => {
  it('extracts branch checkout payloads without board refetch targets', () => {
    const checkout = event({
      id: 'git-1',
      type: 'git.branch_checked_out',
      payload: { branch: 'feat/foo', previousBranch: 'agent-board/AB-12', issueKey: 'AB-12' }
    })
    expect(currentBranchFromEvent(checkout)).toBe('feat/foo')
    expect(refetchTargets('git.branch_checked_out')).toEqual({ run: false, questions: false, reviews: false, issue: false })
    expect(isBoardActivityEvent('git.branch_checked_out')).toBe(false)
  })

  it('patches issue and run read models from checkout events', () => {
    const checkout = event({
      id: 'git-1',
      type: 'git.branch_checked_out',
      runId: 'run-1',
      payload: { branch: 'feat/foo', previousBranch: 'agent-board/AB-12', issueKey: 'AB-12' }
    })
    expect(applyCurrentBranchToIssue(issue, checkout)?.currentBranch).toBe('feat/foo')
    expect(applyCurrentBranchToIssues([issue], checkout)[0]?.currentBranch).toBe('feat/foo')
    expect(applyCurrentBranchToRun({
      id: 'run-1',
      projectId: 'p',
      issueId: 'AB-12',
      workspaceId: 'w',
      agentId: null,
      attempt: 1,
      status: 'RUNNING',
      queueReason: null,
      failureReason: null,
      createdAt: '',
      startedAt: null,
      completedAt: null,
      updatedAt: '',
      currentBranch: 'agent-board/AB-12'
    }, checkout)?.currentBranch).toBe('feat/foo')
  })
})

describe('workspace branch presentation', () => {
  it('renders branch before priority on issue cards and hides when absent', async () => {
    const wrapper = mount(IssueCard, { props: { issue }, global })
    expect(wrapper.find('.issue-branch').text()).toContain('agent-board/AB-12')
    const text = wrapper.text()
    expect(text.indexOf('agent-board/AB-12')).toBeLessThan(text.indexOf('Low'))

    await wrapper.setProps({ issue: { ...issue, currentBranch: null } })
    expect(wrapper.find('.issue-branch').exists()).toBe(false)
  })

  it('patches board issues from project SSE without refetching issues', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/issues')) return new Response(JSON.stringify([issue]), { status: 200 })
      if (url.includes('/agents')) return new Response(JSON.stringify([]), { status: 200 })
      if (url.endsWith('/projects/p')) return new Response(JSON.stringify({ id: 'p', name: 'Demo', issuePrefix: 'AB', repositoryPath: '/repo', defaultBranch: 'main', workflowSettings: {}, allowInternalRunner: true, createdAt: '', updatedAt: '' }), { status: 200 })
      return new Response('[]', { status: 200 })
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('EventSource', MockEventSource)

    const wrapper = mount(ProjectBoard, { props: { projectId: 'p' }, global: { ...global, stubs: { ...global.stubs, IssueCard } } })
    await flushPromises()
    const initialIssueCalls = fetchMock.mock.calls.filter(([url]) => String(url).includes('/issues')).length

    MockEventSource.instances[0]?.emit(event({
      id: 'git-1',
      type: 'git.branch_checked_out',
      payload: { branch: 'feat/foo', previousBranch: 'agent-board/AB-12', issueKey: 'AB-12' }
    }))
    await flushPromises()

    expect(wrapper.text()).toContain('feat/foo')
    expect(fetchMock.mock.calls.filter(([url]) => String(url).includes('/issues')).length).toBe(initialIssueCalls)
  })

  it('patches issue detail from project SSE without refetching the issue', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/issues/AB-12')) return new Response(JSON.stringify(issue), { status: 200 })
      if (url.includes('/runs')) return new Response(JSON.stringify([]), { status: 200 })
      if (url.includes('/agents')) return new Response(JSON.stringify([]), { status: 200 })
      if (url.includes('/questions')) return new Response(JSON.stringify([]), { status: 200 })
      return new Response('[]', { status: 200 })
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('EventSource', MockEventSource)

    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: 'AB-12' }, global })
    await flushPromises()
    const initialIssueCalls = fetchMock.mock.calls.filter(([url]) => String(url).includes('/issues/AB-12')).length

    MockEventSource.instances[0]?.emit(event({
      id: 'git-1',
      type: 'git.branch_checked_out',
      payload: { branch: 'feat/foo', previousBranch: 'agent-board/AB-12', issueKey: 'AB-12' }
    }))
    await flushPromises()

    expect(wrapper.text()).toContain('feat/foo')
    expect(fetchMock.mock.calls.filter(([url]) => String(url).includes('/issues/AB-12')).length).toBe(initialIssueCalls)
  })
})
