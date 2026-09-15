import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectBoard from '../app/components/ProjectBoard.vue'
import type { Issue } from '../app/types/api'
import { placeIssueOnBoard } from '../app/utils/issues'
import { event, MockEventSource } from './execution-fixtures'
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

afterEach(() => {
  vi.unstubAllGlobals()
  MockEventSource.reset()
})

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

  it('refetches canonical state and exposes the safe API error when persistence fails', async () => {
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

  it('does not overwrite a newer realtime board refresh when an optimistic placement later fails', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    let serverIssues = [makeIssue('AB-1', 'TODO', 0), makeIssue('AB-2', 'TODO', 1)]
    let finishPatch: ((response: Response) => void) | undefined
    const pendingPatch = new Promise<Response>(resolve => { finishPatch = resolve })
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (String(path).endsWith('/board') && options.method === 'PATCH') return pendingPatch
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

    serverIssues = [makeIssue('AB-2', 'TODO', 0), makeIssue('AB-1', 'TODO', 1)]
    MockEventSource.instances[0]?.emit(event({ id: 'board-race', type: 'issue.updated', projectId: 'p', sequence: null }))
    await flushPromises()
    expect(wrapper.get('[data-status="TODO"]').findAll('[data-board-issue]').map(value => value.attributes('data-board-issue'))).toEqual(['AB-2', 'AB-1'])

    finishPatch?.(new Response(JSON.stringify({ error: { code: 'conflict', message: 'failed' } }), { status: 409 }))
    await flushPromises()

    expect(wrapper.get('[data-status="TODO"]').findAll('[data-board-issue]').map(value => value.attributes('data-board-issue'))).toEqual(['AB-2', 'AB-1'])
    expect(wrapper.text()).toContain('Unable to move issue')
  })

  it('provides keyboard-accessible reorder and cross-column placement through the same endpoint', async () => {
    let serverIssues = [makeIssue('AB-1', 'TODO', 0), makeIssue('AB-2', 'TODO', 1), makeIssue('AB-3', 'IN_PROGRESS', 0)]
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (String(path).endsWith('/board') && options.method === 'PATCH') {
        const issueId = String(path).split('/').at(-2) || ''
        const body = JSON.parse(options.body as string) as { status: string; beforeIssueId: string | null }
        serverIssues = placeIssueOnBoard(serverIssues, issueId, body.status, body.beforeIssueId)
        return new Response(JSON.stringify(serverIssues.find(value => value.id === issueId)))
      }
      if (String(path).endsWith('/issues')) return new Response(JSON.stringify(serverIssues))
      if (String(path).endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Workspace', workflowSettings: {} }))
    })
    vi.stubGlobal('fetch', fetch)

    const wrapper = mount(ProjectBoard, { props: { projectId: 'p', canMutate: true }, global })
    await flushPromises()

    await wrapper.get('button[aria-label="Move AB-2 up"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-status="TODO"]').findAll('[data-board-issue]').map(value => value.attributes('data-board-issue'))).toEqual(['AB-2', 'AB-1'])

    await wrapper.get('button[aria-label="Move AB-2 right"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-status="TODO"]').findAll('[data-board-issue]').map(value => value.attributes('data-board-issue'))).toEqual(['AB-1'])
    expect(wrapper.get('[data-status="IN_PROGRESS"]').findAll('[data-board-issue]').map(value => value.attributes('data-board-issue'))).toEqual(['AB-2', 'AB-3'])

    const boardCalls = fetch.mock.calls.filter(([path, options]) => String(path).endsWith('/board') && options.method === 'PATCH')
    expect(boardCalls).toHaveLength(2)
    expect(JSON.parse(boardCalls[0]?.[1].body as string)).toEqual({ status: 'TODO', beforeIssueId: 'AB-1' })
    expect(JSON.parse(boardCalls[1]?.[1].body as string)).toEqual({ status: 'IN_PROGRESS', beforeIssueId: 'AB-3' })
  })

  it('does not expose draggable or keyboard board controls to viewers and pauses reordering while filtered', async () => {
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
    expect(viewer.find('button[aria-label="Move AB-1 right"]').exists()).toBe(false)
    viewer.unmount()

    const member = mount(ProjectBoard, { props: { projectId: 'p', canMutate: true }, global })
    await flushPromises()
    expect(member.get('[data-board-issue="AB-1"]').attributes('draggable')).toBe('true')
    expect(member.find('button[aria-label="Move AB-1 right"]').exists()).toBe(true)
    await member.get('input[aria-label="Filter issues"]').setValue('AB-1')
    expect(member.get('[data-board-issue="AB-1"]').attributes('draggable')).toBe('false')
    expect(member.find('button[aria-label="Move AB-1 right"]').exists()).toBe(false)
    expect(member.text()).toContain('Clear the filter to reorder issues.')
  })
})
