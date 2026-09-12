import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import GroupsPage from '../app/pages/settings/groups.vue'
import { uiStubs } from './ui-stubs'

const group = { id: '00000000-0000-0000-0000-000000000010', name: 'engineering', createdAt: '2026-09-12T12:00:00Z', updatedAt: '2026-09-12T12:00:00Z' }
const disabled = { id: '00000000-0000-0000-0000-000000000020', username: 'disabled', email: 'disabled@example.com', displayName: 'Disabled User', deploymentRole: 'member' as const, status: 'disabled' as const, forcePasswordChange: false }

function mountGroups(auth: Record<string, unknown>) {
  vi.stubGlobal('useAuth', () => auth)
  return mount(GroupsPage, {
    global: {
      stubs: {
        ...uiStubs,
        SettingsShell: { template: '<div><slot /></div>' }
      }
    }
  })
}

afterEach(() => vi.unstubAllGlobals())

describe('settings groups', () => {
  it('loads Groups and shows disabled Users in membership state', async () => {
    const auth = {
      groups: vi.fn().mockResolvedValue([group]),
      users: vi.fn().mockResolvedValue([disabled]),
      groupMembers: vi.fn().mockResolvedValue([disabled]),
      createGroup: vi.fn(), updateGroup: vi.fn(), deleteGroup: vi.fn(),
      addGroupMember: vi.fn(), removeGroupMember: vi.fn()
    }
    const wrapper = mountGroups(auth)
    expect(wrapper.text()).toContain('Loading groups')
    await flushPromises()
    expect(wrapper.text()).toContain('engineering')
    expect(wrapper.text()).toContain('Disabled User')
    expect(wrapper.text()).toContain('disabled')
    expect(auth.groupMembers).toHaveBeenCalledWith(group.id)
  })

  it('renders loading and request errors explicitly', async () => {
    let rejectGroups!: (error: Error) => void
    const groups = new Promise<never>((_, reject) => { rejectGroups = reject })
    const auth = {
      groups: vi.fn(() => groups), users: vi.fn().mockResolvedValue([]), groupMembers: vi.fn(),
      createGroup: vi.fn(), updateGroup: vi.fn(), deleteGroup: vi.fn(), addGroupMember: vi.fn(), removeGroupMember: vi.fn()
    }
    const wrapper = mountGroups(auth)
    expect(wrapper.text()).toContain('Loading groups')
    rejectGroups(new Error('directory unavailable'))
    await flushPromises()
    expect(wrapper.text()).toContain('Group request failed')
    expect(wrapper.text()).toContain('directory unavailable')
  })

  it('discards stale successful member responses after the selected Group changes', async () => {
    const platform = { ...group, id: '00000000-0000-0000-0000-000000000011', name: 'platform' }
    const stale = { ...disabled, id: '00000000-0000-0000-0000-000000000022', username: 'stale', email: 'stale@example.com', displayName: 'Stale User' }
    let resolveInitial!: (members: Array<typeof disabled>) => void
    const initialMembers = new Promise<Array<typeof disabled>>(resolve => { resolveInitial = resolve })
    const auth = {
      groups: vi.fn().mockResolvedValue([group, platform]),
      users: vi.fn().mockResolvedValue([]),
      groupMembers: vi.fn((groupId: string) => groupId === group.id ? initialMembers : Promise.resolve([disabled])),
      createGroup: vi.fn(), updateGroup: vi.fn(), deleteGroup: vi.fn(),
      addGroupMember: vi.fn(), removeGroupMember: vi.fn()
    }
    const wrapper = mountGroups(auth)
    await flushPromises()

    const groupSelect = wrapper.findAll('select')[0]!
    await groupSelect.setValue(platform.id)
    await flushPromises()
    expect(auth.groupMembers).toHaveBeenCalledWith(platform.id)
    expect(wrapper.text()).toContain('Disabled User')

    resolveInitial([stale])
    await flushPromises()
    expect(wrapper.text()).toContain('Disabled User')
    expect(wrapper.text()).not.toContain('Stale User')
  })

  it('clears previous Group members while a newly selected Group is loading', async () => {
    const platform = { ...group, id: '00000000-0000-0000-0000-000000000011', name: 'platform' }
    const engineeringMember = { ...disabled, id: '00000000-0000-0000-0000-000000000023', username: 'engineering-user', email: 'engineering@example.com', displayName: 'Engineering Member' }
    const pendingPlatformMembers = new Promise<Array<typeof disabled>>(() => {})
    const auth = {
      groups: vi.fn().mockResolvedValue([group, platform]),
      users: vi.fn().mockResolvedValue([]),
      groupMembers: vi.fn((groupId: string) => groupId === group.id ? Promise.resolve([engineeringMember]) : pendingPlatformMembers),
      createGroup: vi.fn(), updateGroup: vi.fn(), deleteGroup: vi.fn(),
      addGroupMember: vi.fn(), removeGroupMember: vi.fn()
    }
    const wrapper = mountGroups(auth)
    await flushPromises()
    expect(wrapper.text()).toContain('Engineering Member')

    await wrapper.findAll('select')[0]!.setValue(platform.id)
    await flushPromises()
    expect(wrapper.text()).toContain('Members · platform')
    expect(wrapper.text()).toContain('Loading members')
    expect(wrapper.text()).not.toContain('Engineering Member')
  })

  it('ignores stale member failures after a newer Group loads successfully', async () => {
    const platform = { ...group, id: '00000000-0000-0000-0000-000000000011', name: 'platform' }
    const platformMember = { ...disabled, id: '00000000-0000-0000-0000-000000000024', username: 'platform-user', email: 'platform@example.com', displayName: 'Platform Member' }
    let rejectInitial!: (error: Error) => void
    const initialMembers = new Promise<Array<typeof disabled>>((_, reject) => { rejectInitial = reject })
    const auth = {
      groups: vi.fn().mockResolvedValue([group, platform]),
      users: vi.fn().mockResolvedValue([]),
      groupMembers: vi.fn((groupId: string) => groupId === group.id ? initialMembers : Promise.resolve([platformMember])),
      createGroup: vi.fn(), updateGroup: vi.fn(), deleteGroup: vi.fn(),
      addGroupMember: vi.fn(), removeGroupMember: vi.fn()
    }
    const wrapper = mountGroups(auth)
    await flushPromises()

    await wrapper.findAll('select')[0]!.setValue(platform.id)
    await flushPromises()
    expect(wrapper.text()).toContain('Platform Member')

    rejectInitial(new Error('stale engineering failure'))
    await flushPromises()
    expect(wrapper.text()).toContain('Platform Member')
    expect(wrapper.text()).not.toContain('stale engineering failure')
    expect(wrapper.text()).not.toContain('Group request failed')
  })

  it('keeps local membership state after successful mutations when reconciliation fails', async () => {
    const active = { ...disabled, id: '00000000-0000-0000-0000-000000000021', username: 'active', email: 'active@example.com', displayName: 'Active User', status: 'active' as const }
    const auth = {
      groups: vi.fn().mockResolvedValue([group]),
      users: vi.fn().mockResolvedValue([active]),
      groupMembers: vi.fn()
        .mockResolvedValueOnce([])
        .mockRejectedValueOnce(new Error('add refresh unavailable'))
        .mockRejectedValueOnce(new Error('remove refresh unavailable')),
      createGroup: vi.fn(), updateGroup: vi.fn(), deleteGroup: vi.fn(),
      addGroupMember: vi.fn().mockResolvedValue(undefined),
      removeGroupMember: vi.fn().mockResolvedValue(undefined)
    }
    const wrapper = mountGroups(auth)
    await flushPromises()

    await wrapper.findAll('select')[1]!.setValue(active.id)
    await wrapper.findAll('button').find(button => button.text().includes('Add member'))!.trigger('click')
    await flushPromises()
    expect(auth.addGroupMember).toHaveBeenCalledWith(group.id, active.id)
    expect(wrapper.findAll('button').some(button => button.text() === 'Remove')).toBe(true)
    expect(wrapper.text()).toContain('add refresh unavailable')

    await wrapper.findAll('button').find(button => button.text() === 'Remove')!.trigger('click')
    await flushPromises()
    expect(auth.removeGroupMember).toHaveBeenCalledWith(group.id, active.id)
    expect(wrapper.findAll('button').some(button => button.text() === 'Remove')).toBe(false)
    expect(wrapper.text()).toContain('remove refresh unavailable')
  })

  it('keeps created/deleted Group state when the following member read fails', async () => {
    const created = { ...group, id: '00000000-0000-0000-0000-000000000012', name: 'design' }
    const auth = {
      groups: vi.fn().mockResolvedValue([group]),
      users: vi.fn().mockResolvedValue([]),
      groupMembers: vi.fn()
        .mockResolvedValueOnce([])
        .mockRejectedValueOnce(new Error('create refresh unavailable'))
        .mockRejectedValueOnce(new Error('delete refresh unavailable')),
      createGroup: vi.fn().mockResolvedValue(created),
      updateGroup: vi.fn(),
      deleteGroup: vi.fn().mockResolvedValue(undefined),
      addGroupMember: vi.fn(), removeGroupMember: vi.fn()
    }
    const wrapper = mountGroups(auth)
    await flushPromises()

    const create = wrapper.get('[data-testid="create-group"]')
    await create.get('input').setValue('Design')
    await create.trigger('submit')
    await flushPromises()
    expect(auth.createGroup).toHaveBeenCalledWith('Design')
    expect(wrapper.text()).toContain('Members · design')
    expect(wrapper.text()).toContain('create refresh unavailable')
    expect(auth.groups).toHaveBeenCalledTimes(1)

    await wrapper.findAll('button').find(button => button.text() === 'Delete group')!.trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'Confirm delete')!.trigger('click')
    await flushPromises()
    expect(auth.deleteGroup).toHaveBeenCalledWith(created.id)
    expect(wrapper.text()).not.toContain('design')
    expect(wrapper.text()).toContain('Members · engineering')
    expect(wrapper.text()).toContain('delete refresh unavailable')
  })

  it('creates, renames, adds/removes members and confirms deletion through the shared auth client', async () => {
    const active = { ...disabled, id: '00000000-0000-0000-0000-000000000021', username: 'active', displayName: 'Active User', status: 'active' as const }
    const auth = {
      groups: vi.fn().mockResolvedValue([group]),
      users: vi.fn().mockResolvedValue([active]),
      groupMembers: vi.fn().mockResolvedValueOnce([]).mockResolvedValueOnce([]).mockResolvedValueOnce([active]).mockResolvedValue([]),
      createGroup: vi.fn().mockResolvedValue(group),
      updateGroup: vi.fn().mockResolvedValue({ ...group, name: 'platform' }),
      deleteGroup: vi.fn().mockResolvedValue(undefined),
      addGroupMember: vi.fn().mockResolvedValue(undefined),
      removeGroupMember: vi.fn().mockResolvedValue(undefined)
    }
    const wrapper = mountGroups(auth)
    await flushPromises()

    const create = wrapper.get('[data-testid="create-group"]')
    await create.get('input').setValue('Engineering')
    await create.trigger('submit')
    await flushPromises()
    expect(auth.createGroup).toHaveBeenCalledWith('Engineering')

    const inputs = wrapper.findAll('input')
    await inputs[1]!.setValue('Platform')
    const forms = wrapper.findAll('form')
    await forms[1]!.trigger('submit')
    await flushPromises()
    expect(auth.updateGroup).toHaveBeenCalledWith(group.id, 'Platform')

    const selects = wrapper.findAll('select')
    await selects[1]!.setValue(active.id)
    const add = wrapper.findAll('button').find(button => button.text().includes('Add member'))!
    await add.trigger('click')
    await flushPromises()
    expect(auth.addGroupMember).toHaveBeenCalledWith(group.id, active.id)

    const remove = wrapper.findAll('button').find(button => button.text() === 'Remove')!
    await remove.trigger('click')
    await flushPromises()
    expect(auth.removeGroupMember).toHaveBeenCalledWith(group.id, active.id)

    const deleteButton = wrapper.findAll('button').find(button => button.text() === 'Delete group')!
    await deleteButton.trigger('click')
    const confirm = wrapper.findAll('button').find(button => button.text() === 'Confirm delete')!
    await confirm.trigger('click')
    await flushPromises()
    expect(auth.deleteGroup).toHaveBeenCalledWith(group.id)
  })
})
