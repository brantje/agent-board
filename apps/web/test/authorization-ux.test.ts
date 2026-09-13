import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ConfigManager from '../app/components/ConfigManager.vue'
import IssueDetail from '../app/components/IssueDetail.vue'
import IssueRelationships from '../app/components/IssueRelationships.vue'
import ProjectBoard from '../app/components/ProjectBoard.vue'
import QuestionPanel from '../app/components/QuestionPanel.vue'
import ReviewDetail from '../app/components/ReviewDetail.vue'
import { question, reviewDetail } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const projectIssue = {
  id: 'AB-1',
  number: 1,
  projectId: 'p',
  title: 'Viewer issue',
  description: 'Read-only work',
  status: 'TODO',
  priority: 0,
  assignedAgentId: null,
  createdAt: '',
  updatedAt: '',
  currentBranch: null,
  lastEvent: null
}

const global = {
  stubs: {
    ...uiStubs,
    IssueCard: { props: ['issue'], template: '<article>{{ issue.title }}</article>' },
    IssueEditor: { template: '<form>Issue editor</form>' },
    WorkspaceBranch: { props: ['branch'], template: '<span>{{ branch }}</span>' },
    RunStatus: { props: ['status', 'label'], template: '<span>{{ label || status }}</span>' },
    RunChangedFilesCard: { template: '<section>Changed files</section>' },
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('Project role presentation', () => {
  it('keeps the Board readable but hides Issue creation for viewers', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/agents')) return new Response('[]')
      if (path.endsWith('/issues')) return new Response(JSON.stringify([projectIssue]))
      return new Response(JSON.stringify({ id: 'p', name: 'Viewer Project' }))
    }))

    const wrapper = mount(ProjectBoard, { props: { projectId: 'p', canMutate: false }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Viewer Project / Board')
    expect(wrapper.text()).toContain('Viewer issue')
    expect(wrapper.findAll('button').some(button => button.text() === 'New issue')).toBe(false)
  })

  it('keeps Issue details readable but hides edit and assignment controls for viewers', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/agents') || path.endsWith('/runs')) return new Response('[]')
      return new Response(JSON.stringify(projectIssue))
    }))

    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: 'AB-1', canMutate: false }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Read-only work')
    expect(wrapper.text()).toContain('Priority 0')
    expect(wrapper.findAll('button').some(button => button.text() === 'Edit issue')).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text() === 'Assign Agent')).toBe(false)
    expect(wrapper.text()).not.toContain('Assignment')
  })

  it('shows Questions to viewers without answer controls', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([question()]))))
    const wrapper = mount(QuestionPanel, { props: { projectId: 'project-a', issueId: 'AB-1', canMutate: false }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Which strategy?')
    expect(wrapper.text()).toContain('read-only')
    expect(wrapper.findAll('button').some(button => button.text() === 'Answer')).toBe(false)
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('shows Issue relationships without add or remove controls for viewers', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/relationships')) {
        return new Response(JSON.stringify([{ id: 'rel-1', type: 'blocks', targetIssueId: 'AB-2' }]))
      }
      return new Response(JSON.stringify([projectIssue, { ...projectIssue, id: 'AB-2', title: 'Target issue' }]))
    }))

    const wrapper = mount(IssueRelationships, { props: { projectId: 'p', issueId: 'AB-1', canMutate: false }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Target issue')
    expect(wrapper.findAll('button').some(button => button.text() === 'Remove')).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text() === 'Add relationship')).toBe(false)
  })

  it('shows Review evidence without decision controls for viewers', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(reviewDetail()))))
    const wrapper = mount(ReviewDetail, {
      props: { projectId: 'project-a', reviewId: 'review-1', canMutate: false },
      global
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Candidate')
    expect(wrapper.text()).toContain('read-only')
    expect(wrapper.findAll('button').some(button => button.text() === 'Approve')).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text() === 'Request changes')).toBe(false)
  })

  it('makes Project configuration inspectable but not editable for non-admins', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([{
      id: 'provider-1',
      projectId: 'p',
      name: 'Project Provider',
      kind: 'openai',
      baseUrl: 'https://example.test',
      enabled: true,
      healthStatus: 'HEALTHY'
    }]))))

    const wrapper = mount(ConfigManager, { props: { kind: 'providers', projectId: 'p', canMutate: false }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Project Provider')
    expect(wrapper.findAll('button').some(button => button.text() === 'New provider')).toBe(false)
    const view = wrapper.findAll('button').find(button => button.text() === 'View')
    expect(view).toBeDefined()
    await view!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Read-only Project configuration')
    expect(wrapper.findAll('button').some(button => button.text() === 'Save')).toBe(false)
    expect(wrapper.text()).not.toContain('Set or replace API key')
  })
})
