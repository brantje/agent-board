import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SquadManager from '../app/components/SquadManager.vue'
import { uiStubs } from './ui-stubs'

const leader = { id: 'agent-leader', projectId: 'project-a', name: 'Lead', roleInstructions: '', engine: 'opencode', modelProfileId: 'model-a', engineSettings: {}, concurrencyLimit: 1, state: 'ENABLED' }
const member = { ...leader, id: 'agent-member', name: 'Builder' }
const squad = {
  id: 'squad-a',
  projectId: 'project-a',
  name: 'Platform Squad',
  leaderAgentId: leader.id,
  members: [{ agentId: member.id, role: 'Backend' }],
  createdAt: '',
  updatedAt: ''
}

const global = {
  stubs: {
    ...uiStubs,
    IdentityAvatar: { props: ['name'], template: '<span>{{ name }}</span>' }
  }
}

afterEach(() => vi.unstubAllGlobals())

function responseFor(path: string) {
  if (path.endsWith('/squads')) return new Response(JSON.stringify([squad]))
  if (path.endsWith('/agents')) return new Response(JSON.stringify([leader, member]))
  return new Response('{}', { status: 404 })
}

describe('Squad management', () => {
  it('renders leader and members distinctly and keeps human Groups separate in copy', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => responseFor(path)))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Agent Squads')
    expect(wrapper.text()).toContain('distinct from human Groups')
    expect(wrapper.text()).toContain('Platform Squad')
    expect(wrapper.text()).toContain('Leader')
    expect(wrapper.text()).toContain('Lead')
    expect(wrapper.text()).toContain('Member')
    expect(wrapper.text()).toContain('Builder')
    expect(wrapper.text()).toContain('Backend')
  })

  it('keeps Squad configuration readable but not mutable for non-admins', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => responseFor(path)))
    const wrapper = mount(SquadManager, { props: { projectId: 'project-a', canMutate: false }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Platform Squad')
    expect(wrapper.findAll('button').some(button => button.text() === 'New squad')).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text() === 'Delete')).toBe(false)
    const view = wrapper.findAll('button').find(button => button.text() === 'View')
    expect(view).toBeDefined()
    await view!.trigger('click')
    expect(wrapper.text()).toContain('Read-only Squad')
    expect(wrapper.findAll('button').some(button => button.text() === 'Save Squad')).toBe(false)
  })

  it('creates Squads through the Project Squad API with one leader and member roles', async () => {
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
    await wrapper.findAll('button').find(button => button.text() === 'Add member')!.trigger('click')
    await wrapper.find('[data-field="member-0-agent"] select').setValue(member.id)
    await wrapper.find('[data-field="member-0-role"] input').setValue('Backend')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(requests).toEqual([{
      path: '/api/projects/project-a/squads',
      method: 'POST',
      body: {
        name: 'Platform Squad',
        leaderAgentId: leader.id,
        members: [{ agentId: member.id, role: 'Backend' }]
      }
    }])
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

  it('surfaces backend validation failures without inventing frontend authority', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string, init?: RequestInit) => {
      if (!init?.method || init.method === 'GET') return responseFor(path)
      return new Response(JSON.stringify({ error: { code: 'invalid_argument' } }), { status: 422 })
    }))

    const wrapper = mount(SquadManager, { props: { projectId: 'project-a' }, global })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Edit')!.trigger('click')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Unable to save Squad')
    expect(wrapper.text()).toContain('server rejected one or more values')
  })
})
