import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SquadManager from '../app/components/SquadManager.vue'
import { uiStubs } from './ui-stubs'

const leader = { id: 'agent-leader', projectId: 'project-a', name: 'Lead', roleInstructions: '', engine: 'opencode', modelProfileId: 'model-a', engineSettings: {}, concurrencyLimit: 1, state: 'ENABLED' }
const member = { ...leader, id: 'agent-member', name: 'Builder' }
const human = { type: 'USER' as const, id: 'user-member', name: 'Alice Example' }
const assignees = [
  { type: 'AGENT' as const, id: leader.id, name: leader.name },
  { type: 'AGENT' as const, id: member.id, name: member.name },
  human
]
const squad = {
  id: 'squad-a',
  projectId: 'project-a',
  name: 'Platform Squad',
  leaderAgentId: leader.id,
  members: [
    { type: 'AGENT' as const, id: member.id, role: 'Backend' },
    { type: 'USER' as const, id: human.id, role: 'Product' }
  ],
  createdAt: '',
  updatedAt: ''
}

const global = {
  stubs: {
    ...uiStubs,
    IdentityAvatar: { props: ['kind', 'name'], template: '<span :data-kind="kind">{{ name }}</span>' }
  }
}

afterEach(() => vi.unstubAllGlobals())

function responseFor(path: string) {
  if (path.endsWith('/squads')) return new Response(JSON.stringify([squad]))
  if (path.endsWith('/agents')) return new Response(JSON.stringify([leader, member]))
  if (path.endsWith('/assignees')) return new Response(JSON.stringify(assignees))
  return new Response('{}', { status: 404 })
}

describe('Squad management', () => {
  it('renders a mixed roster with authoritative identity types and keeps access Groups separate', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => responseFor(path)))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('one Agent leader/executor with optional Agent and User members')
    expect(wrapper.text()).toContain('distinct from human access Groups')
    expect(wrapper.text()).toContain('Lead · Agent')
    expect(wrapper.text()).toContain('Builder · Agent')
    expect(wrapper.text()).toContain('Backend')
    expect(wrapper.text()).toContain('Alice Example · User')
    expect(wrapper.text()).toContain('Product')
    expect(wrapper.find('[data-kind="agent"]').exists()).toBe(true)
    expect(wrapper.find('[data-kind="user"]').exists()).toBe(true)
  })

  it('keeps persisted roster readable when the member directory fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/assignees')) return new Response(JSON.stringify({ error: { code: 'internal' } }), { status: 500 })
      return responseFor(path)
    }))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Platform Squad')
    expect(wrapper.text()).toContain('Lead · Agent')
    expect(wrapper.text()).toContain('Builder · Agent')
    expect(wrapper.text()).toContain('User unavailable · User')
    expect(wrapper.text()).toContain('Product')

    await wrapper.findAll('button').find(button => button.text() === 'Edit')!.trigger('click')
    expect(wrapper.text()).toContain('Member directory unavailable')
    expect(wrapper.text()).toContain('Existing Squad membership remains readable')
    expect(wrapper.find('[data-field="member-0-identity"] select').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').find(button => button.text() === 'Add member')!.attributes('disabled')).toBeDefined()
  })

  it('keeps Squad configuration readable but not mutable for non-admins', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => responseFor(path)))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a', canMutate: false }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Alice Example · User')
    expect(wrapper.findAll('button').some(button => button.text() === 'New squad')).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text() === 'Delete')).toBe(false)
    const view = wrapper.findAll('button').find(button => button.text() === 'View')
    expect(view).toBeDefined()
    await view!.trigger('click')
    expect(wrapper.text()).toContain('Read-only Squad')
    expect(wrapper.findAll('button').some(button => button.text() === 'Save Squad')).toBe(false)
  })

  it('keeps the leader selector Agent-only while member choices include Agents and Users', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => responseFor(path)))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'New squad')!.trigger('click')

    const leaderOptions = wrapper.find('[data-field="leaderAgentId"] select').findAll('option').map(option => option.text())
    expect(leaderOptions).toContain('Lead')
    expect(leaderOptions).toContain('Builder')
    expect(leaderOptions).not.toContain('Alice Example')

    await wrapper.find('[data-field="leaderAgentId"] select').setValue(leader.id)
    await wrapper.findAll('button').find(button => button.text() === 'Add member')!.trigger('click')
    const memberOptions = wrapper.find('[data-field="member-0-identity"] select').findAll('option').map(option => option.text())
    expect(memberOptions).toContain('Agent · Builder')
    expect(memberOptions).toContain('User · Alice Example')
    expect(memberOptions).not.toContain('Agent · Lead')
  })

  it('prevents duplicate typed identities from being selected in multiple member rows', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => responseFor(path)))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'New squad')!.trigger('click')
    await wrapper.find('[data-field="leaderAgentId"] select').setValue(leader.id)
    const addMember = () => wrapper.findAll('button').find(button => button.text() === 'Add member')!.trigger('click')
    await addMember()
    await addMember()
    await wrapper.find('[data-field="member-0-identity"] select').setValue(`USER:${human.id}`)

    const secondOptions = wrapper.find('[data-field="member-1-identity"] select').findAll('option').map(option => option.text())
    expect(secondOptions).not.toContain('User · Alice Example')
    expect(secondOptions).toContain('Agent · Builder')
  })

  it('creates Squads through the Project Squad API with typed mixed members and roles', async () => {
    const requests: Array<{ path: string; method: string; body?: unknown }> = []
    vi.stubGlobal('fetch', vi.fn(async (path: string, init?: RequestInit) => {
      if (!init?.method || init.method === 'GET') return responseFor(path)
      requests.push({ path, method: init.method, body: init.body ? JSON.parse(String(init.body)) : undefined })
      return new Response(JSON.stringify(squad), { status: 201 })
    }))

    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'New squad')!.trigger('click')
    await wrapper.find('[data-field="name"] input').setValue('Platform Squad')
    await wrapper.find('[data-field="leaderAgentId"] select').setValue(leader.id)
    const addMember = () => wrapper.findAll('button').find(button => button.text() === 'Add member')!.trigger('click')
    await addMember()
    await addMember()
    await wrapper.find('[data-field="member-0-identity"] select').setValue(`AGENT:${member.id}`)
    await wrapper.find('[data-field="member-0-role"] input').setValue('Backend')
    await wrapper.find('[data-field="member-1-identity"] select').setValue(`USER:${human.id}`)
    await wrapper.find('[data-field="member-1-role"] input').setValue('Product')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(requests).toEqual([{
      path: '/api/projects/project-a/squads',
      method: 'POST',
      body: {
        name: 'Platform Squad',
        leaderAgentId: leader.id,
        members: [
          { type: 'AGENT', id: member.id, role: 'Backend' },
          { type: 'USER', id: human.id, role: 'Product' }
        ]
      }
    }])
  })

  it('preserves typed mixed membership when opening an existing Squad for editing', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => responseFor(path)))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Edit')!.trigger('click')

    expect((wrapper.find('[data-field="member-0-identity"] select').element as HTMLSelectElement).value).toBe(`AGENT:${member.id}`)
    expect((wrapper.find('[data-field="member-1-identity"] select').element as HTMLSelectElement).value).toBe(`USER:${human.id}`)
    expect((wrapper.find('[data-field="member-1-role"] input').element as HTMLInputElement).value).toBe('Product')
  })

  it('blocks incomplete drafts before they reach the API and surfaces backend validation failures', async () => {
    const postRequests: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (path: string, init?: RequestInit) => {
      if (init?.method === 'POST') postRequests.push(path)
      if (!init?.method || init.method === 'GET') return responseFor(path)
      return new Response(JSON.stringify({ error: { code: 'invalid_argument' } }), { status: 422 })
    }))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'New squad')!.trigger('click')
    await wrapper.find('[data-field="name"] input').setValue('Platform Squad')
    await wrapper.find('[data-field="leaderAgentId"] select').setValue(leader.id)
    await wrapper.findAll('button').find(button => button.text() === 'Add member')!.trigger('click')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Choose an Agent or User for every member row.')
    expect(postRequests).toHaveLength(0)

    await wrapper.find('[data-field="member-0-identity"] select').setValue(`USER:${human.id}`)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Unable to save Squad')
    expect(wrapper.text()).toContain('server rejected one or more values')
  })

  it('renders persisted stale Users from their authoritative type without inferring Agent identity', async () => {
    const staleSquad = { ...squad, members: [{ type: 'USER' as const, id: 'gone-user', role: 'Product' }] }
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/squads')) return new Response(JSON.stringify([staleSquad]))
      return responseFor(path)
    }))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('User unavailable · User')
    expect(wrapper.find('[data-kind="user"]').exists()).toBe(true)
  })

  it('deletes Squads through the empty-response API path and refreshes persisted state', async () => {
    const requests: Array<{ path: string; method: string }> = []
    let listCalls = 0
    vi.stubGlobal('fetch', vi.fn(async (path: string, init?: RequestInit) => {
      if (path.endsWith('/squads') && (!init?.method || init.method === 'GET')) {
        listCalls += 1
        return new Response(JSON.stringify(listCalls === 1 ? [squad] : []))
      }
      if (path.endsWith('/agents')) return new Response(JSON.stringify([leader, member]))
      if (path.endsWith('/assignees')) return new Response(JSON.stringify(assignees))
      if (init?.method === 'DELETE') {
        requests.push({ path, method: init.method })
        return new Response(null, { status: 204 })
      }
      return new Response('{}', { status: 404 })
    }))

    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Delete')!.trigger('click')
    await flushPromises()

    expect(requests).toEqual([{ path: '/api/projects/project-a/squads/squad-a', method: 'DELETE' }])
    expect(wrapper.text()).not.toContain('Unable to delete Squad')
    expect(listCalls).toBe(2)
  })
})
