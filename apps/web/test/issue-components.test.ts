import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import ProjectBoard from '../app/components/ProjectBoard.vue'
import IssueDetail from '../app/components/IssueDetail.vue'
import IssueEditor from '../app/components/IssueEditor.vue'
import IssueCard from '../app/components/IssueCard.vue'
import { useRefresh } from '../app/composables/useRefresh'
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
  updatedAt: ''
}
const agent = {
  id: 'a',
  projectId: 'p',
  name: 'Coder',
  roleInstructions: '',
  engine: 'opencode',
  modelProfileId: 'm',
  runtimeId: 'r',
  engineSettings: {},
  concurrencyLimit: 1,
  state: 'ENABLED'
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
  updatedAt: ''
}
const global = {
  stubs: {
    ...uiStubs,
    IssueCard,
    IssueEditor,
    IssueRelationships: { template: '<section>Relationships</section>' },
    QuestionPanel: { props: ['projectId', 'issueId', 'runId'], template: '<section>Questions</section>' },
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
  }
}
const button = (wrapper: ReturnType<typeof mount>, label: string) => wrapper.findAll('button').find(value => value.text() === label)!
const formForField = (wrapper: ReturnType<typeof mount>, name: string) => wrapper.findAll('form').find(form => form.find(`[data-field=${name}]`).exists())!

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('Issue workflow components', () => {
  it('renders the authoritative card hierarchy and safe links for assigned/unassigned Issues', async () => {
    const wrapper = mount(IssueCard, { props: { issue }, global })
    expect(wrapper.get('a').attributes('href')).toBe('/projects/p/issues/AB-12')
    const text = wrapper.text()
    expect(text.indexOf('AB-12')).toBeLessThan(text.indexOf('Fix scheduler'))
    expect(text.indexOf('Fix scheduler')).toBeLessThan(text.indexOf('Persist leases'))
    expect(text).toContain('Low')
    expect(text).not.toContain('Unassigned')
    expect(text).not.toContain('TODO')
    expect(wrapper.find('[aria-label="Assigned Agent"]').exists()).toBe(false)

    await wrapper.setProps({ issue: { ...issue, assignedAgentId: 'a', priority: 3, description: '' } })
    expect(wrapper.text()).toContain('High')
    expect(wrapper.text()).not.toContain('Persist leases')
    expect(wrapper.find('[aria-label="Assigned Agent"]').exists()).toBe(true)

    await wrapper.setProps({ agentName: 'Coder' })
    expect(wrapper.find('[aria-label="Coder"]').exists()).toBe(true)
    expect(wrapper.find('[aria-label="Assigned Agent"]').exists()).toBe(false)
  })

  it('creates only valid Issues, round-trips priority, and cannot submit protected Review/Done transitions', async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify(issue)))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueEditor, { props: { projectId: 'p' }, global })

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
    expect(done.get('[data-field=status]').findAll('option').map(option => option.text())).toEqual(['Select…', 'Done', 'Todo'])
    await done.get('[data-field=status] select').setValue('TODO')
    await done.get('form').trigger('submit')
    await flushPromises()
    expect(fetch.mock.calls.at(-1)?.[1]).toMatchObject({ method: 'PATCH' })

    const review = mount(IssueEditor, { props: { projectId: 'p', issue: { ...issue, status: 'REVIEW' } }, global })
    expect(review.get('[data-field=status]').findAll('option').map(option => option.text())).toEqual(['Select…', 'Review'])
    expect(review.text()).toContain('Review decision')

    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'conflict', message: 'raw detail' } }), { status: 409 })))
    await review.get('form').trigger('submit')
    await flushPromises()
    expect(review.text()).toContain('conflicts with existing state')
    expect(review.text()).not.toContain('raw detail')
  })

  it('renders all Board columns, filters Issues, and reconciles Project/Agent/Issue state after creation', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (options.method === 'POST') return new Response(JSON.stringify({ ...issue, id: 'created', title: 'Created' }), { status: 201 })
      if (path.endsWith('/agents')) return new Response(JSON.stringify([agent]))
      if (path.endsWith('/issues')) return new Response(JSON.stringify([issue]))
      return new Response(JSON.stringify({ id: 'p', name: 'Workspace' }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Workspace / Board')
    expect(wrapper.findAll('header h2')).toHaveLength(6)
    expect(wrapper.text()).toContain('Fix scheduler')
    await wrapper.get('input').setValue('absent')
    expect(wrapper.text()).not.toContain('Fix scheduler')
    expect(wrapper.text()).toContain('No matching issues')

    await button(wrapper, 'New issue').trigger('click')
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

  it('treats Project/Issue/Agent reads as one scoped Board state and offers retry on failure', async () => {
    let failProject = true
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/projects/p' && failProject) {
        return new Response(JSON.stringify({ error: { code: 'project_not_found', message: 'private path' } }), { status: 404 })
      }
      if (path.endsWith('/agents')) return new Response(JSON.stringify([]))
      if (path.endsWith('/issues')) return new Response(JSON.stringify([]))
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

  it('reconciles the assignment response immediately, then refetches durable Issue and Run state', async () => {
    let status = 'TODO'
    let assigned = false
    let fail = false
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (options.method === 'POST') {
        if (fail) {
          return new Response(JSON.stringify({ error: { code: 'execution_configuration_invalid', message: 'unsafe backend detail' } }), { status: 422 })
        }
        status = 'IN_PROGRESS'
        assigned = true
        return new Response(JSON.stringify({ issue: { ...issue, status, assignedAgentId: 'a' }, run }), { status: 202 })
      }
      if (path.endsWith('/agents')) {
        return new Response(JSON.stringify([agent, { ...agent, id: 'draft', name: 'Draft', state: 'DRAFT' }, { ...agent, id: 'disabled', name: 'Disabled', state: 'DISABLED' }]))
      }
      if (path.endsWith('/runs')) return new Response(JSON.stringify(assigned ? [run] : []))
      return new Response(JSON.stringify({ ...issue, status, assignedAgentId: assigned ? 'a' : null }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Priority 0')
    expect(wrapper.text()).toContain('No Runs yet')
    expect(wrapper.text()).toContain('Assign an Agent to this Issue to schedule the first Run.')
    expect(wrapper.text()).not.toContain('New work will appear here when it is created.')
    expect(wrapper.text()).toContain('Questions')
    expect(wrapper.find('option[value=draft]').exists()).toBe(false)
    expect(wrapper.find('option[value=disabled]').exists()).toBe(false)
    await formForField(wrapper, 'agent').trigger('submit')
    expect(fetch.mock.calls.filter(([, options]) => options.method === 'POST')).toHaveLength(0)

    await wrapper.get('[data-field=agent] select').setValue('a')
    fail = true
    await formForField(wrapper, 'agent').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('selected execution configuration is not runnable')
    expect(wrapper.text()).not.toContain('unsafe backend detail')

    fail = false
    await formForField(wrapper, 'agent').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Assignment accepted')
    expect(wrapper.text()).toContain('Board status: In Progress')
    expect(wrapper.text()).toContain('Run attempt 1: Queued')
    expect(wrapper.text()).toContain('Attempt 1 · Queued')
    expect(wrapper.text()).toContain('Queue reason: capacity')
    expect(fetch.mock.calls.filter(([, options]) => options.method === 'GET').length).toBeGreaterThanOrEqual(6)

    await button(wrapper, 'Edit issue').trigger('click')
    await button(wrapper, 'Cancel').trigger('click')
    await button(wrapper, 'Edit issue').trigger('click')
    await wrapper.findAll('form').at(-1)!.trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role=dialog]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('handles empty Agent state and prevents DONE Issues from starting Runs', async () => {
    let done = false
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/agents')) return new Response(JSON.stringify([]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ ...issue, status: done ? 'DONE' : 'TODO' }))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('No enabled Agents')
    wrapper.unmount()

    done = true
    const doneWrapper = mount(IssueDetail, { props: { projectId: 'p', issueId: issue.id }, global })
    await flushPromises()
    expect(button(doneWrapper, 'Assign Agent').attributes('disabled')).toBeDefined()
    expect(doneWrapper.text()).toContain('Reopen this Issue')
    doneWrapper.unmount()
  })

  it('refreshes on focus and interval without overlapping or leaking after unmount', async () => {
    vi.useFakeTimers()
    let resolve!: () => void
    const refresh = vi.fn(() => new Promise<void>(value => { resolve = value }))
    const wrapper = mount(defineComponent({ setup() { useRefresh(refresh, 100); return () => h('div') } }))

    window.dispatchEvent(new Event('focus'))
    expect(refresh).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(100)
    expect(refresh).toHaveBeenCalledTimes(1)
    resolve()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(100)
    expect(refresh).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    resolve()
    await flushPromises()
    window.dispatchEvent(new Event('focus'))
    await vi.advanceTimersByTimeAsync(300)
    expect(refresh).toHaveBeenCalledTimes(2)
  })
})
