import { DragDropProvider } from '@dnd-kit/vue'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectBoard from '../app/components/ProjectBoard.vue'
import type { Issue } from '../app/types/api'
import { boardDragId, boardDropZoneId } from '../app/utils/board-order'
import { event, MockEventSource } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

vi.mock('@dnd-kit/vue', async () => {
  const actual = await vi.importActual<typeof import('@dnd-kit/vue')>('@dnd-kit/vue')
  const vue = await vi.importActual<typeof import('vue')>('vue')
  return {
    ...actual,
    DragDropProvider: vue.defineComponent({
      name: 'DragDropProvider',
      props: ['sensors'],
      emits: ['dragEnd'],
      setup(_props, { slots }) {
        return () => vue.h('div', { 'data-dnd-provider': '' }, slots.default?.())
      }
    }),
    useDraggable: () => ({ isDragging: vue.ref(false) }),
    useDroppable: () => ({ isDropTarget: vue.ref(false) })
  }
})

const global = {
  stubs: {
    ...uiStubs,
    BoardDraggableIssue: {
      props: ['issue', 'disabled'],
      template: '<div :data-card="issue.id" :data-disabled="String(disabled)">{{ issue.status }}</div>'
    },
    BoardDropZone: {
      props: ['status', 'index', 'disabled'],
      template: '<div :data-gap="`${status}:${index}`" :data-disabled="String(disabled)" />'
    },
    IssueEditor: { template: '<form />' }
  }
}

function issue(id: string, status = 'TODO'): Issue {
  return {
    id,
    projectId: 'p',
    number: Number(id.replace(/\D/g, '')) || 1,
    title: id,
    description: '',
    status,
    priority: 0,
    assignedTo: null,
    createdBy: null,
    createdAt: '',
    updatedAt: '',
    currentBranch: null,
    lastEvent: null
  }
}

function cardOrder(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('[data-card]').map(card => card.attributes('data-card'))
}

function dragEnd(wrapper: ReturnType<typeof mount>, issueId: string, status: string, index: number) {
  wrapper.getComponent(DragDropProvider).vm.$emit('dragEnd', {
    canceled: false,
    operation: {
      source: { id: boardDragId(issueId) },
      target: { id: boardDropZoneId(status, index) }
    }
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
  MockEventSource.reset()
})

describe('ProjectBoard drag ordering', () => {
  it('allows pointer activation from the linked card surface', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/issues')) return new Response(JSON.stringify([issue('AB-1')]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Project' }))
    }))
    const wrapper = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()

    const sensors = wrapper.getComponent(DragDropProvider).props('sensors') as Array<{ options?: { preventActivation?: () => boolean } }>
    expect(sensors[0]?.options?.preventActivation?.()).toBe(false)
    wrapper.unmount()
  })

  it('optimistically reorders and persists exact neighboring issue keys', async () => {
    const initial = [issue('AB-1'), issue('AB-2'), issue('AB-3')]
    let resolvePlacement: ((response: Response) => void) | undefined
    const fetch = vi.fn((path: string, options: RequestInit = {}) => {
      if (String(path).endsWith('/placement') && options.method === 'POST') {
        return new Promise<Response>(resolve => { resolvePlacement = resolve })
      }
      if (String(path).endsWith('/issues')) return Promise.resolve(new Response(JSON.stringify(initial)))
      if (String(path).endsWith('/runs')) return Promise.resolve(new Response(JSON.stringify([])))
      return Promise.resolve(new Response(JSON.stringify({ id: 'p', name: 'Project' })))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()

    dragEnd(wrapper, 'AB-2', 'TODO', 3)
    await nextTick()
    expect(cardOrder(wrapper)).toEqual(['AB-1', 'AB-3', 'AB-2'])

    const placementCall = fetch.mock.calls.find(([path, options]) => String(path).endsWith('/placement') && options.method === 'POST')
    expect(JSON.parse(String(placementCall?.[1]?.body))).toEqual({ beforeId: 'AB-3', afterId: null })

    resolvePlacement?.(new Response(JSON.stringify(issue('AB-2')), { status: 200 }))
    await flushPromises()
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('rolls back a failed optimistic move and refreshes canonical order', async () => {
    const initial = [issue('AB-1'), issue('AB-2'), issue('AB-3')]
    let resolvePlacement: ((response: Response) => void) | undefined
    const fetch = vi.fn((path: string, options: RequestInit = {}) => {
      if (String(path).endsWith('/placement') && options.method === 'POST') {
        return new Promise<Response>(resolve => { resolvePlacement = resolve })
      }
      if (String(path).endsWith('/issues')) return Promise.resolve(new Response(JSON.stringify(initial)))
      if (String(path).endsWith('/runs')) return Promise.resolve(new Response(JSON.stringify([])))
      return Promise.resolve(new Response(JSON.stringify({ id: 'p', name: 'Project' })))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()

    dragEnd(wrapper, 'AB-2', 'TODO', 3)
    await nextTick()
    expect(cardOrder(wrapper)).toEqual(['AB-1', 'AB-3', 'AB-2'])

    resolvePlacement?.(new Response(JSON.stringify({ error: { code: 'conflict', message: 'stale anchors' } }), { status: 409 }))
    await flushPromises()
    expect(cardOrder(wrapper)).toEqual(['AB-1', 'AB-2', 'AB-3'])
    expect(wrapper.text()).toContain('Unable to move issue')
    expect(wrapper.text()).not.toContain('stale anchors')
    wrapper.unmount()
  })

  it('converges multiple open boards through the existing project event stream', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    let current = [issue('AB-1'), issue('AB-2'), issue('AB-3')]
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/issues')) return new Response(JSON.stringify(current))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Project' }))
    }))
    const first = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    const second = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()
    expect(cardOrder(first)).toEqual(['AB-1', 'AB-2', 'AB-3'])
    expect(cardOrder(second)).toEqual(['AB-1', 'AB-2', 'AB-3'])

    current = [issue('AB-3'), issue('AB-1'), issue('AB-2')]
    for (const source of MockEventSource.instances) {
      source.emit(event({ id: 'evt-order', type: 'issue.updated', sequence: null, issueId: 'AB-3' }))
    }
    await flushPromises()

    expect(cardOrder(first)).toEqual(['AB-3', 'AB-1', 'AB-2'])
    expect(cardOrder(second)).toEqual(['AB-3', 'AB-1', 'AB-2'])
    first.unmount()
    second.unmount()
  })

  it('disables drag ordering for filtered and read-only boards', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/issues')) return new Response(JSON.stringify([issue('AB-1'), issue('AB-2')]))
      if (path.endsWith('/runs')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify({ id: 'p', name: 'Project' }))
    }))
    const mutable = mount(ProjectBoard, { props: { projectId: 'p' }, global })
    await flushPromises()
    expect(mutable.get('[data-card="AB-1"]').attributes('data-disabled')).toBe('false')
    await mutable.get('input').setValue('AB-1')
    expect(mutable.get('[data-card="AB-1"]').attributes('data-disabled')).toBe('true')
    expect(mutable.text()).toContain('Reordering paused while filtering')

    const viewer = mount(ProjectBoard, { props: { projectId: 'p', canMutate: false }, global })
    await flushPromises()
    expect(viewer.get('[data-card="AB-1"]').attributes('data-disabled')).toBe('true')
    expect(viewer.findAll('button').some(button => button.text() === 'New issue')).toBe(false)
    mutable.unmount()
    viewer.unmount()
  })
})
