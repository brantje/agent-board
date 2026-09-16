import { mount } from '@vue/test-utils'
import { toValue } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import type { Issue } from '../app/types/api'
import BoardDraggableIssue from '../app/components/BoardDraggableIssue.vue'
import { uiStubs } from './ui-stubs'

const captured = vi.hoisted(() => ({ input: undefined as any }))

vi.mock('@dnd-kit/vue', async () => {
  const vue = await vi.importActual<typeof import('vue')>('vue')
  return {
    useDraggable: (input: unknown) => {
      captured.input = input
      return { isDragging: vue.ref(false) }
    }
  }
})

const issue: Issue = {
  id: 'AB-1',
  projectId: 'project-1',
  number: 1,
  title: 'Draggable issue',
  description: '',
  status: 'TODO',
  priority: 0,
  assignedTo: null,
  createdBy: null,
  createdAt: '',
  updatedAt: '',
  currentBranch: null,
  lastEvent: null
}

const global = {
  stubs: {
    ...uiStubs,
    IssueCard: {
      props: ['issue'],
      template: '<a data-issue-card href="#issue">{{ issue.id }}</a>'
    }
  }
}

describe('BoardDraggableIssue', () => {
  it('uses a dedicated handle so linked issue-card content does not block pointer activation', () => {
    const wrapper = mount(BoardDraggableIssue, { props: { issue }, global })
    const root = wrapper.get('[data-board-draggable="AB-1"]')
    const handle = wrapper.get('[data-board-drag-handle]')

    expect(wrapper.get('[data-issue-card]').element.tagName).toBe('A')
    expect(toValue(captured.input.element)).toBe(root.element)
    expect(toValue(captured.input.handle)).toBe(handle.element)
    expect(captured.input.disabled()).toBe(false)
    expect(handle.attributes('aria-label')).toBe('Drag AB-1')

    wrapper.unmount()
  })

  it('hides the handle and disables dragging on read-only or filtered boards', () => {
    const wrapper = mount(BoardDraggableIssue, { props: { issue, disabled: true }, global })

    expect(wrapper.find('[data-board-drag-handle]').exists()).toBe(false)
    expect(captured.input.disabled()).toBe(true)

    wrapper.unmount()
  })
})
