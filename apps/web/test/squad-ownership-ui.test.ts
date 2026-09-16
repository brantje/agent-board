import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import IdentityAvatar from '../app/components/IdentityAvatar.vue'
import IssueCard from '../app/components/IssueCard.vue'
import IssueDetail from '../app/components/IssueDetail.vue'
import { uiStubs } from './ui-stubs'

const squadIssue = {
  id: 'AB-136',
  number: 136,
  projectId: 'p',
  title: 'Squad-owned work',
  description: '',
  status: 'TODO',
  priority: 0,
  assignedTo: { type: 'SQUAD' as const, id: 'squad-1', name: 'Backend' },
  createdBy: null,
  createdAt: '',
  updatedAt: '',
  currentBranch: null,
  lastEvent: null
}

const global = {
  stubs: {
    ...uiStubs,
    IdentityAvatar,
    IssueRelationships: { template: '<section />' },
    QuestionPanel: { template: '<section />' },
    IssueEditor: { template: '<section />' },
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('Squad Issue ownership presentation', () => {
  it('shows Squad ownership on Issue cards with the Squad identity icon', () => {
    const wrapper = mount(IssueCard, { props: { issue: squadIssue }, global })
    expect(wrapper.text()).toContain('Backend · Squad')
    expect(wrapper.find('[data-icon="i-lucide-users"]').exists()).toBe(true)
    expect(wrapper.find('[data-icon="i-lucide-bot"]').exists()).toBe(false)
  })

  it('labels Squad assignment and ownership without describing it as a direct Agent assignment', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return new Response(JSON.stringify([squadIssue.assignedTo]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      if (path.endsWith('/execution')) return new Response(JSON.stringify({ state: 'READY', canStart: true, activeRun: null }))
      return new Response(JSON.stringify(squadIssue))
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: squadIssue.id }, global })
    await flushPromises()

    expect(wrapper.get('[data-field=assignee]').text()).toContain('Backend · Squad')
    expect(wrapper.text()).toContain('Backend · Squad')
    expect(wrapper.text()).toContain('User, Agent, or Squad')
    expect(wrapper.text()).toContain('Agent or Squad ownership may enqueue execution')
    expect(wrapper.text()).not.toContain('current Agent assignment')
    expect(wrapper.find('[data-icon="i-lucide-users"]').exists()).toBe(true)
  })
})
