import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectBoard from '../app/components/ProjectBoard.vue'
import type { Issue } from '../app/types/api'
import { placeIssueOnBoard } from '../app/utils/issues'
import { uiStubs } from './ui-stubs'

const makeIssue = (id: string, status: string, boardPosition: number): Issue => ({
  id,
  number: Number(id.replace(/\D/g, '')) || 1,
  projectId: 'p',
  title: id,
  description: '',
  status,
  priority: 0,
  boardPosition,
  assignedTo: null,
  createdBy: null,
  createdAt: '',
  updatedAt: '',
  currentBranch: null,
  lastEvent: null
})

const global = {
  stubs: {
    ...uiStubs,
    IssueCard: { props: ['issue'], template: '<article>{{ issue.id }}</article>' },
    IssueEditor: { template: '<form />' }
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('ProjectBoard drag ordering', () => {
  it('optimistically places an issue before the drop target and persists the exact placement', async () => {
    let serverIssues = [makeIssue('AB-1', 'TODO', 0), makeIssue('AB-2', 'TODO', 1), makeIssue('AB-3', 'IN_PROGRESS', 0)]
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (String(path).endsWith('/board') && options.method === 'PATCH') {
        const body = JSON.parse(options.body as string) as { status: string; beforeIssueId: string | null }
        serverIssues = placeIssueOnBoard(serverIssues, 'AB-1', body.status, body.beforeIssueId)
        return new Response(JSON.stringify(serverIssues.find(value => value.id === 'AB-1')))
      }
      if (String(path).endsWith('/issues')) return new Response(JSON.stringify(serverIssues))
      if (String(path).endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Workspace', workflowSettings: {} }))
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(ProjectBoard, { props: { projectId: 'p', canMutate: true }, global })
    await flushPromises()

    const transfer = { effectAllowed: '', dropEffect: '', setData: vi.fn() }
    await wrapper.get('[data-board-issue="AB-1"]').trigger('dragstart', { dataTransfer: transfer })
    await wrapper.get('[data-board-issue="AB-3"]').trigger('drop', { dataTransfer: transfer })
    await flushPromises()

    const call = fetch.mock.calls.find(([path, options]) => String(path).endsWith('/issues/AB-1/board') && options.method === 'PATCH')
    expect(call).toBeTruthy()
    expect(JSON.parse(call?.[1].body as string)).toEqual({ status: 'IN_PROGRESS', beforeIssueId: 'AB-3' })
    expect(wrapper.get('[data-status="TODO"]').text()).not.toContain('AB-1')
    expect(wrapper.get('[data-status="IN_PROGRESS"]').text()).toContain('AB-1')
    expect(wrapper.get('[data-status="IN_PROGRESS"]').findAll('[data-board-issue]').map(value => value.attributes('data-board-issue'))).toEqual(['AB-1', 'AB-3'])
  })

  it('restores the previous order and exposes the safe API error when persistence fails', async () => {
    const serverIssues = [makeIssue('AB-1', 'TODO', 0), makeIssue('AB-2', 'TODO', 1)]
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (String(path).endsWith('/board') && options.method === 'PATCH') {
        return new Response(JSON.stringify({ error: { code: 'conflict', message: 'raw detail' } }), { status: 409 })
      }
      if (String(path).endsWith('/issues')) return new Response(JSON.stringify(serverIssues))
      if (String(path).endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Workspace', workflowSettings: {} }))
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(ProjectBoard, { props: { projectId: 'p', canMutate: true }, global })
    await flushPromises()
    const transfer = { effectAllowed: '', dropEffect: '', setData: vi.fn() }
    await wrapper.get('[data-board-issue="AB-2"]').trigger('dragstart', { dataTransfer: transfer })
    await wrapper.get('[data-board-issue="AB-1"]').trigger('drop', { dataTransfer: transfer })
    await flushPromises()

    expect(wrapper.get('[data-status="TODO"]').findAll('[data-board-issue]').map(value => value.attributes('data-board-issue'))).toEqual(['AB-1', 'AB-2'])
    expect(wrapper.text()).toContain('conflicts with existing state')
    expect(wrapper.text()).not.toContain('raw detail')
  })

  it('does not expose draggable board cards to viewers and pauses reordering while filtered', async () => {
    const serverIssues = [makeIssue('AB-1', 'TODO', 0)]
    const fetch = vi.fn(async (path: string) => {
      if (String(path).endsWith('/issues')) return new Response(JSON.stringify(serverIssues))
      if (String(path).endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Workspace', workflowSettings: {} }))
    })
    vi.stubGlobal('fetch', fetch)

    const viewer = mount(ProjectBoard, { props: { projectId: 'p', canMutate: false }, global })
    await flushPromises()
    expect(viewer.get('[data-board-issue="AB-1"]').attributes('draggable')).toBe('false')
    viewer.unmount()

    const member = mount(ProjectBoard, { props: { projectId: 'p', canMutate: true }, global })
    await flushPromises()
    expect(member.get('[data-board-issue="AB-1"]').attributes('draggable')).toBe('true')
    await member.get('input[aria-label="Filter issues"]').setValue('AB-1')
    expect(member.get('[data-board-issue="AB-1"]').attributes('draggable')).toBe('false')
    expect(member.text()).toContain('Clear the filter to reorder issues.')
  })
})
