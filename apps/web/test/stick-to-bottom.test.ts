import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ActivityTimeline from '../app/components/ActivityTimeline.vue'
import { useStickToBottom } from '../app/composables/useStickToBottom'
import { projectRunActivity } from '../app/utils/events'
import { event } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

function pane(scrollTop = 0, clientHeight = 100, scrollHeight = 400) {
  return { scrollTop, clientHeight, scrollHeight }
}

describe('useStickToBottom', () => {
  it('does not assign scrollTop when already flush with the bottom', () => {
    const el = pane(300, 100, 400)
    let api!: ReturnType<typeof useStickToBottom>
    mount(defineComponent({
      setup() {
        api = useStickToBottom(() => el)
        return () => h('div')
      }
    }))

    const writes: number[] = []
    Object.defineProperty(el, 'scrollTop', {
      configurable: true,
      get: () => 300,
      set: value => { writes.push(value) }
    })

    api.follow()
    expect(writes).toEqual([])
  })

  it('follows new content while at the bottom and pauses after the user scrolls away', async () => {
    const el = pane(0, 100, 400)
    let api!: ReturnType<typeof useStickToBottom>
    const wrapper = mount(defineComponent({
      setup() {
        api = useStickToBottom(() => el)
        return () => h('div')
      }
    }))

    api.follow()
    expect(el.scrollTop).toBe(400)

    el.scrollTop = 40
    el.scrollHeight = 500
    api.onScroll()
    api.follow()
    expect(el.scrollTop).toBe(40)

    el.scrollTop = 376
    el.scrollHeight = 400
    api.onScroll()
    el.scrollHeight = 520
    api.follow()
    expect(el.scrollTop).toBe(520)
    wrapper.unmount()
  })

  it('does not follow newly appended content during an active touch', () => {
    const el = pane(300, 100, 400)
    let api!: ReturnType<typeof useStickToBottom>
    const wrapper = mount(defineComponent({
      setup() {
        api = useStickToBottom(() => el)
        return () => h('div')
      }
    }))

    api.onTouchStart({ touches: [{ clientY: 200 }] })
    el.scrollHeight = 520
    api.follow()
    expect(el.scrollTop).toBe(299)

    api.onTouchEnd()
    expect(el.scrollTop).toBe(520)
    wrapper.unmount()
  })

  it('does not jump back to the bottom after the user scrolls away during a touch', () => {
    const el = pane(300, 100, 400)
    let api!: ReturnType<typeof useStickToBottom>
    const wrapper = mount(defineComponent({
      setup() {
        api = useStickToBottom(() => el)
        return () => h('div')
      }
    }))

    api.onTouchStart({ touches: [{ clientY: 200 }] })
    el.scrollTop = 40
    api.onScroll()
    el.scrollHeight = 520
    api.onTouchEnd()
    expect(el.scrollTop).toBe(40)
    wrapper.unmount()
  })

  it('prevents default at the bottom so nested touch scrolling does not chain to the parent page', () => {
    const el = pane(300, 100, 400)
    let api!: ReturnType<typeof useStickToBottom>
    mount(defineComponent({
      setup() {
        api = useStickToBottom(() => el)
        return () => h('div')
      }
    }))

    api.onTouchStart({ touches: [{ clientY: 200 }] })
    el.scrollTop = 300
    const move = {
      touches: [{ clientY: 160 }],
      cancelable: true,
      preventDefault: vi.fn(),
      stopPropagation: vi.fn()
    }
    api.onTouchMove(move)
    expect(move.preventDefault).toHaveBeenCalled()
    expect(move.stopPropagation).toHaveBeenCalled()
  })

  it('does not preventDefault while the nested feed can still scroll', () => {
    const el = pane(80, 100, 400)
    let api!: ReturnType<typeof useStickToBottom>
    mount(defineComponent({
      setup() {
        api = useStickToBottom(() => el)
        return () => h('div')
      }
    }))

    api.onTouchStart({ touches: [{ clientY: 200 }] })
    const move = {
      touches: [{ clientY: 160 }],
      cancelable: true,
      preventDefault: vi.fn(),
      stopPropagation: vi.fn()
    }
    api.onTouchMove(move)
    expect(move.preventDefault).not.toHaveBeenCalled()
    expect(move.stopPropagation).toHaveBeenCalled()
  })

  it('prevents default at the top so nested touch scrolling does not chain upward', () => {
    const el = pane(0, 100, 400)
    let api!: ReturnType<typeof useStickToBottom>
    mount(defineComponent({
      setup() {
        api = useStickToBottom(() => el)
        return () => h('div')
      }
    }))

    api.onTouchStart({ touches: [{ clientY: 120 }] })
    el.scrollTop = 0
    const move = {
      touches: [{ clientY: 180 }],
      cancelable: true,
      preventDefault: vi.fn(),
      stopPropagation: vi.fn()
    }
    api.onTouchMove(move)
    expect(move.preventDefault).toHaveBeenCalled()
  })
})

describe('ActivityTimeline scrolling', () => {
  it('uses a vertical scroll region without a persistent scrollbar', () => {
    const items = projectRunActivity([
      event({ id: 'run', type: 'run.started', sequence: 1 }),
      event({
        id: 'server',
        type: 'tool.started',
        sequence: 2,
        payload: { name: 'opencode-server', command: ['opencode', 'serve', '--hostname', '127.0.0.1', '--port', '4096'] }
      })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const scroller = wrapper.get('[data-run-activity-scroll]')
    expect(scroller.classes()).toContain('overflow-x-hidden')
    expect(scroller.classes()).toContain('overflow-y-auto')
    expect(scroller.classes()).toContain('overscroll-none')
    expect(scroller.classes()).toContain('touch-pan-y')
    expect(scroller.classes()).toContain('[scrollbar-width:none]')
    expect(scroller.classes()).toContain('[&::-webkit-scrollbar]:hidden')
    expect(scroller.classes()).not.toContain('[scrollbar-gutter:stable]')
    expect(wrapper.get('[data-run-activity]').classes()).toContain('min-w-0')
    wrapper.unmount()
  })

  it('puts the feed in a scroll region and follows newly appended activity', async () => {
    const items = projectRunActivity([
      event({ id: 'start', type: 'tool.started', sequence: 1, payload: { name: 'bash', source: 'opencode' } })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const scroller = wrapper.get('[data-run-activity-scroll]')
    Object.defineProperties(scroller.element, {
      clientHeight: { configurable: true, get: () => 80 },
      scrollHeight: { configurable: true, get: () => 240 }
    })
    scroller.element.scrollTop = 0
    const later = projectRunActivity([
      event({ id: 'start', type: 'tool.started', sequence: 1, payload: { name: 'bash', source: 'opencode' } }),
      event({ id: 'done', type: 'tool.completed', sequence: 2, payload: { name: 'bash', source: 'opencode', summary: 'ls' } })
    ])
    await wrapper.setProps({ items: later })
    await nextTick()
    expect(scroller.element.scrollTop).toBe(240)

    scroller.element.scrollTop = 10
    await scroller.trigger('scroll')
    const paused = projectRunActivity([
      event({ id: 'start', type: 'tool.started', sequence: 1, payload: { name: 'bash', source: 'opencode' } }),
      event({ id: 'done', type: 'tool.completed', sequence: 2, payload: { name: 'bash', source: 'opencode', summary: 'ls' } }),
      event({ id: 'thought', type: 'agent.message', sequence: 3, payload: { kind: 'reasoning', message: 'Next file.' } })
    ])
    await wrapper.setProps({ items: paused })
    await nextTick()
    expect(scroller.element.scrollTop).toBe(10)
    wrapper.unmount()
  })

  it('accepts nested touch gestures on the feed', async () => {
    const items = projectRunActivity([
      event({ id: 'start', type: 'tool.started', sequence: 1, payload: { name: 'bash', source: 'opencode' } })
    ])
    const wrapper = mount(ActivityTimeline, {
      props: { items },
      global: { stubs: uiStubs }
    })
    const scroller = wrapper.get('[data-run-activity-scroll]')
    Object.defineProperties(scroller.element, {
      clientHeight: { configurable: true, get: () => 80 },
      scrollHeight: { configurable: true, get: () => 240 }
    })
    scroller.element.scrollTop = 160
    await scroller.trigger('touchstart', { touches: [{ clientY: 200 }] })
    await scroller.trigger('touchmove', { touches: [{ clientY: 140 }] })
    await scroller.trigger('touchend')
    await scroller.trigger('touchcancel')
    wrapper.unmount()
  })
})
