import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RunDelegationCard from '../app/components/RunDelegationCard.vue'
import SquadContextCard from '../app/components/SquadContextCard.vue'
import { uiStubs } from './ui-stubs'

const global = {
  stubs: {
    ...uiStubs,
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('delegation inspection', () => {
  it('renders parent delegation result and links ordinary child Run evidence', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/delegations')) {
        return new Response(JSON.stringify([{
          id: 'delegation-1',
          projectId: 'p',
          issueId: 'AB-1',
          parentRunId: 'parent-run',
          parentAgentId: 'parent-agent',
          targetAgentId: 'target-agent',
          task: 'Inspect the bounded component',
          delegatedRunId: 'child-run',
          workspaceAccess: 'WRITE',
          requestKey: 'call-1',
          parentRunStatus: 'READY_FOR_REVIEW',
          delegatedRunStatus: 'COMPLETED',
          outcome: 'SUCCEEDED',
          resultSummary: 'Delegated check passed.',
          resultEventId: 'event-1',
          workspaceChangesAccepted: true,
          workspaceRevision: 'abc123',
          continuationJobId: 'job-1',
          completedAt: '2026-01-01T00:00:00Z',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z'
        }]))
      }
      if (path.endsWith('/delegation')) {
        return new Response(JSON.stringify({ error: { code: 'delegation_not_found' } }), { status: 404 })
      }
      if (path.endsWith('/agents')) {
        return new Response(JSON.stringify([{ id: 'target-agent', name: 'Implementer' }]))
      }
      return new Response(JSON.stringify([]))
    }))
    const wrapper = mount(RunDelegationCard, { props: { projectId: 'p', runId: 'parent-run' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Delegated to Implementer')
    expect(wrapper.text()).toContain('Delegated check passed.')
    expect(wrapper.text()).toContain('Workspace changes accepted: yes')
    expect(wrapper.text()).toContain('Revision: abc123')
    expect(wrapper.get('a[href="/projects/p/runs/child-run"]').text()).toContain('Open delegated Run evidence')
    wrapper.unmount()
  })

  it('renders delegated child lineage back to the authoritative parent Run', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/delegations')) {
        return new Response(JSON.stringify([]))
      }
      if (path.endsWith('/delegation')) {
        return new Response(JSON.stringify({
          id: 'delegation-2',
          projectId: 'p',
          issueId: 'AB-2',
          parentRunId: 'parent-run',
          parentAgentId: 'parent-agent',
          targetAgentId: 'child-agent',
          task: 'Inspect the delegated path',
          delegatedRunId: 'child-run',
          workspaceAccess: 'WRITE',
          requestKey: 'call-2',
          parentRunStatus: 'PAUSED',
          delegatedRunStatus: 'RUNNING',
          outcome: null,
          resultSummary: null,
          resultEventId: null,
          workspaceChangesAccepted: null,
          workspaceRevision: 'def456',
          continuationJobId: null,
          completedAt: null,
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z'
        }))
      }
      if (path.endsWith('/agents')) {
        return new Response(JSON.stringify([{ id: 'parent-agent', name: 'Lead Agent' }]))
      }
      return new Response(JSON.stringify([]))
    }))
    const wrapper = mount(RunDelegationCard, { props: { projectId: 'p', runId: 'child-run' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Delegated subtask')
    expect(wrapper.text()).toContain('Parent Agent: Lead Agent')
    expect(wrapper.text()).toContain('Inspect the delegated path')
    expect(wrapper.get('a[href="/projects/p/runs/parent-run"]').text()).toContain('Open parent Run')
    wrapper.unmount()
  })

  it('renders Squad leader and descriptive member roles without implying fan-out', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/squads/squad-1')) {
        return new Response(JSON.stringify({
          id: 'squad-1',
          projectId: 'p',
          name: 'Backend',
          leaderAgentId: 'leader-1',
          members: [
            { type: 'AGENT', id: 'member-1', role: 'implementation' },
            { type: 'USER', id: 'user-1', role: 'reviewer' }
          ],
          createdAt: '',
          updatedAt: ''
        }))
      }
      if (path.endsWith('/assignees')) {
        return new Response(JSON.stringify([
          { type: 'AGENT', id: 'leader-1', name: 'Lead Agent' },
          { type: 'AGENT', id: 'member-1', name: 'Implementer' },
          { type: 'USER', id: 'user-1', name: 'Alex' }
        ]))
      }
      return new Response(JSON.stringify([]))
    }))
    const wrapper = mount(SquadContextCard, { props: { projectId: 'p', squadId: 'squad-1' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Backend · Squad')
    expect(wrapper.text()).toContain('Authoritative leader')
    expect(wrapper.text()).toContain('Lead Agent')
    expect(wrapper.text()).toContain('Implementer · Agent')
    expect(wrapper.text()).toContain('implementation')
    expect(wrapper.text()).toContain('Alex · User')
    expect(wrapper.text()).toContain('reviewer')
    expect(wrapper.text()).toContain('delegation remains explicit')
    wrapper.unmount()
  })
})
