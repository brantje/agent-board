import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RunStatus from '../app/components/RunStatus.vue'
import { uiStubs } from './ui-stubs'

const global = { stubs: uiStubs }

describe('RunStatus', () => {
  it('renders the Board-derived icon, color, and activity label', () => {
    const wrapper = mount(RunStatus, { props: { status: 'READY_FOR_REVIEW' }, global })
    expect(wrapper.get('[data-run-status=READY_FOR_REVIEW]').attributes('data-color')).toBe('success')
    expect(wrapper.get('[data-run-status=READY_FOR_REVIEW]').attributes('data-icon')).toBe('i-lucide-scan-eye')
    expect(wrapper.text()).toContain('Ready for review')
    expect(wrapper.find('[data-icon="i-lucide-loader-circle"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('live')
  })

  it('shows a live spinner only for in-progress execution', async () => {
    const running = mount(RunStatus, { props: { status: 'RUNNING', live: true }, global })
    expect(running.text()).toContain('Agent is working')
    expect(running.get('[data-run-status=RUNNING]').attributes('data-icon')).toBe('i-lucide-play')
    expect(running.get('[data-run-status=RUNNING]').attributes('data-color')).toBe('warning')
    expect(running.find('[data-icon="i-lucide-loader-circle"]').exists()).toBe(true)
    expect(running.text()).toContain('live')

    const review = mount(RunStatus, { props: { status: 'READY_FOR_REVIEW', live: true }, global })
    expect(review.find('[data-icon="i-lucide-loader-circle"]').exists()).toBe(false)
    expect(review.text()).not.toContain('live')

    await running.setProps({ live: false })
    expect(running.find('[data-icon="i-lucide-loader-circle"]').exists()).toBe(false)
  })

  it('keeps failed input-wait distinct by icon while sharing the Blocked color family', () => {
    const failed = mount(RunStatus, { props: { status: 'FAILED' }, global })
    const waiting = mount(RunStatus, { props: { status: 'WAITING_FOR_INPUT' }, global })
    expect(failed.get('[data-run-status=FAILED]').attributes('data-color')).toBe('error')
    expect(waiting.get('[data-run-status=WAITING_FOR_INPUT]').attributes('data-color')).toBe('error')
    expect(failed.get('[data-run-status=FAILED]').attributes('data-icon')).toBe('i-lucide-circle-x')
    expect(waiting.get('[data-run-status=WAITING_FOR_INPUT]').attributes('data-icon')).toBe('i-lucide-octagon-alert')
    expect(failed.text()).toContain('Run failed')
    expect(waiting.text()).toContain('Agent needs input')
  })
})
