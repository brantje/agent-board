import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ActivityTimeline from '../app/components/ActivityTimeline.vue'
import { projectRunActivity } from '../app/utils/events'
import { event } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

describe('workspace transfer activity', () => {
  it('renders live transfer progress and replaces it with the completed state', async () => {
    const started = event({
      id: 'transfer-started',
      type: 'workspace.transfer.started',
      sequence: 1,
      payload: { direction: 'to_runner', runnerName: 'build-host', transferId: 'transfer-1' }
    })
    const progress = event({
      id: 'transfer-progress',
      type: 'workspace.transfer.progress',
      sequence: 2,
      payload: {
        direction: 'to_runner',
        runnerName: 'build-host',
        transferId: 'transfer-1',
        bytesTransferred: 65536,
        totalBytes: 131072
      }
    })

    const wrapper = mount(ActivityTimeline, {
      props: { items: projectRunActivity([started, progress]) },
      global: { stubs: uiStubs }
    })

    const transferring = wrapper.get('[data-workspace-transfer-status="progress"]')
    expect(transferring.text()).toContain('Transferring workspace to build-host')
    expect(transferring.text()).toContain('64.0 KiB / 128.0 KiB (50%)')
    expect(transferring.get('progress').attributes()).toMatchObject({ value: '50', max: '100' })
    expect(transferring.find('[data-icon="i-lucide-loader-circle"]').exists()).toBe(true)

    const completed = event({
      id: 'transfer-completed',
      type: 'workspace.transfer.completed',
      sequence: 3,
      payload: {
        direction: 'to_runner',
        runnerName: 'build-host',
        transferId: 'transfer-1',
        bytesTransferred: 131072,
        totalBytes: 131072
      }
    })
    await wrapper.setProps({ items: projectRunActivity([started, progress, completed]) })

    expect(wrapper.find('[data-workspace-transfer-status="progress"]').exists()).toBe(false)
    const done = wrapper.get('[data-workspace-transfer-status="completed"]')
    expect(done.text()).toContain('Workspace transferred to build-host')
    expect(done.text()).toContain('128.0 KiB (100%)')
    expect(done.find('progress').exists()).toBe(false)
    expect(done.find('[data-icon="i-lucide-circle-check"]').exists()).toBe(true)
  })
})
