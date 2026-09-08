import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import IssueRelationships from '../app/components/IssueRelationships.vue'
import { uiStubs } from './ui-stubs'

const source = {
  id: 'AB-1',
  number: 1,
  projectId: 'p',
  title: 'Source Issue',
  description: '',
  status: 'TODO',
  priority: 0,
  assignedAgentId: null,
  createdAt: '',
  updatedAt: ''
}
const target = { ...source, id: 'AB-2', number: 2, title: 'Target Issue', priority: 2 }
const relationship = {
  id: 'relationship-1',
  projectId: 'p',
  sourceIssueId: source.id,
  targetIssueId: target.id,
  type: 'blocks',
  createdAt: ''
}
const global = {
  stubs: {
    ...uiStubs,
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
  }
}

const button = (wrapper: ReturnType<typeof mount>, label: string) => wrapper.findAll('button').find(value => value.text() === label)!

afterEach(() => vi.unstubAllGlobals())

describe('IssueRelationships', () => {
  it('excludes self targets and refetches durable state after create and delete', async () => {
    let relationships: typeof relationship[] = []
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (path === '/api/projects/p/issues' && options.method === 'GET') return new Response(JSON.stringify([source, target]))
      if (path === '/api/projects/p/issues/AB-1/relationships' && options.method === 'GET') return new Response(JSON.stringify(relationships))
      if (path === '/api/projects/p/issues/AB-1/relationships' && options.method === 'POST') {
        relationships = [relationship]
        return new Response(JSON.stringify(relationship), { status: 201 })
      }
      if (path === '/api/projects/p/issues/AB-1/relationships/relationship-1' && options.method === 'DELETE') {
        relationships = []
        return new Response(null, { status: 204 })
      }
      return new Response('not found', { status: 404 })
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(IssueRelationships, { props: { projectId: 'p', issueId: source.id }, global })
    await flushPromises()

    const targetSelect = wrapper.get('[data-field=relationshipTarget] select')
    expect(targetSelect.find('option[value=AB-1]').exists()).toBe(false)
    expect(targetSelect.find('option[value=AB-2]').text()).toBe('Target Issue')
    expect(wrapper.text()).toContain('No relationships authored from this Issue')

    await targetSelect.setValue(target.id)
    await wrapper.get('[data-field=relationshipType] select').setValue('blocks')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const post = fetch.mock.calls.find(([, options]) => options.method === 'POST')
    expect(post?.[0]).toBe('/api/projects/p/issues/AB-1/relationships')
    expect(post?.[1]).toMatchObject({ body: JSON.stringify({ targetIssueId: 'AB-2', type: 'blocks' }) })
    expect(wrapper.text()).toContain('blocks')
    expect(wrapper.text()).toContain('Target Issue')
    expect(wrapper.get('a').attributes('href')).toBe('/projects/p/issues/AB-2')

    await button(wrapper, 'Remove').trigger('click')
    await flushPromises()
    expect(fetch).toHaveBeenCalledWith('/api/projects/p/issues/AB-1/relationships/relationship-1', expect.objectContaining({ method: 'DELETE' }))
    expect(wrapper.text()).toContain('No relationships authored from this Issue')
  })

  it('uses stable local relationship errors without reflecting server payload details', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (path === '/api/projects/p/issues') return new Response(JSON.stringify([source, target]))
      if (options.method === 'GET') return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ error: { code: 'issue_relationship_exists', message: 'secret backend detail' } }), { status: 409 })
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(IssueRelationships, { props: { projectId: 'p', issueId: source.id }, global })
    await flushPromises()
    await wrapper.get('[data-field=relationshipTarget] select').setValue(target.id)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('relationship already exists')
    expect(wrapper.text()).not.toContain('secret backend detail')
  })
})
