import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import IssueDetail from '../app/components/IssueDetail.vue'
import { MockEventSource } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const global = {
  stubs: {
    ...uiStubs,
    IssueEditor: { template: '<div />' },
    IssueRelationships: { template: '<section>Relationships</section>' },
    QuestionPanel: { template: '<section>Questions</section>' },
    RunStatus: { template: '<span />' },
    WorkspaceBranch: { template: '<span />' },
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' }
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  MockEventSource.reset()
})

it.each([
  [{ type: 'HUMAN', id: '00000000-0000-0000-0000-000000000001', name: 'Human Creator' }, 'Human Creator · User'],
  [{ type: 'AGENT', id: '00000000-0000-0000-0000-000000000002', name: 'Planner' }, 'Planner · Agent']
])('shows the resolved issue creator in the Properties sidebar', async (createdBy, expected) => {
  vi.stubGlobal('EventSource', MockEventSource)
  vi.stubGlobal('fetch', vi.fn(async (path: string) => {
    if (path.endsWith('/assignees') || path.endsWith('/runs')) return new Response(JSON.stringify([]))
    return new Response(JSON.stringify({
      id: 'AB-1',
      projectId: 'p',
      number: 1,
      title: 'Attributed issue',
      description: '',
      status: 'TODO',
      priority: 0,
      assignedTo: null,
      createdBy,
      createdAt: '',
      updatedAt: '',
      currentBranch: null,
      lastEvent: null
    }))
  }))

  const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: 'AB-1' }, global })
  await flushPromises()

  expect(wrapper.text()).toContain('Created by')
  expect(wrapper.text()).toContain(expected)
  expect(wrapper.text()).not.toContain(createdBy.id)
  wrapper.unmount()
})