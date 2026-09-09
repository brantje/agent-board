import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RunUsageCard from '../app/components/RunUsageCard.vue'
import { useRunEvents } from '../app/composables/useRunEvents'
import { projectRunActivity } from '../app/utils/events'
import type { RunUsageEvidence } from '../app/types/api'
import { evidence, event, MockEventSource } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const usage: RunUsageEvidence = {
  contextTokens: 83_400,
  contextLimitTokens: 200_000,
  inputTokens: 412_000,
  outputTokens: 36_000,
  cacheReadTokens: 301_000,
  averageWaitMs: 1_800,
  tokensPerSecond: 48.2
}

afterEach(() => {
  vi.unstubAllGlobals()
  MockEventSource.reset()
})

describe('RunUsageCard', () => {
  it('renders compact run usage with exact token tooltips and context progress', () => {
    const wrapper = mount(RunUsageCard, { props: { usage }, global: { stubs: uiStubs } })

    expect(wrapper.text()).toContain('83.4K / 200K')
    expect(wrapper.text()).toContain('42%')
    expect(wrapper.text()).toContain('1.8s')
    expect(wrapper.text()).toContain('412K')
    expect(wrapper.text()).toContain('36K')
    expect(wrapper.text()).toContain('301K')
    expect(wrapper.text()).toContain('48.2')
    expect(wrapper.find('[title="83,400 / 200,000 tokens"]').exists()).toBe(true)
    expect(wrapper.find('progress').exists()).toBe(true)
  })

  it('does not invent a context limit or unavailable timing metrics', () => {
    const wrapper = mount(RunUsageCard, {
      props: {
        usage: {
          ...usage,
          contextTokens: 12_000,
          contextLimitTokens: null,
          averageWaitMs: null,
          tokensPerSecond: null
        }
      },
      global: { stubs: uiStubs }
    })

    expect(wrapper.text()).toContain('12K / —')
    expect(wrapper.find('progress').exists()).toBe(false)
    expect(wrapper.text()).toContain('Avg. waiting—')
    expect(wrapper.text()).toContain('Tokens/sec—')
  })

  it('renders an explicit empty state before the first model usage sample', () => {
    const wrapper = mount(RunUsageCard, { global: { stubs: uiStubs } })
    expect(wrapper.text()).toContain('No model usage recorded yet.')
  })
})

describe('run usage live updates', () => {
  it('keeps model.usage out of the activity timeline and refreshes only the usage projection', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const snapshot = evidence({ events: [event({ id: 'start', type: 'run.started', sequence: 1 })] })
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/evidence')) return new Response(JSON.stringify(snapshot))
      if (path.endsWith('/usage')) return new Response(JSON.stringify(usage))
      if (path.includes('/reviews')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify([]))
    })
    vi.stubGlobal('fetch', fetch)

    let live!: ReturnType<typeof useRunEvents>
    const wrapper = mount(defineComponent({
      setup() {
        live = useRunEvents('project-a', 'run-1')
        return () => h('div', String(live.evidence.value?.usage?.outputTokens ?? 'none'))
      }
    }))
    await flushPromises()

    const usageEvent = event({ id: 'usage-1', type: 'model.usage', sequence: 2, runId: 'run-1' })
    MockEventSource.instances[0]?.emit(usageEvent)
    await flushPromises()

    expect(fetch.mock.calls.some(([path]) => path === '/api/projects/project-a/runs/run-1/usage')).toBe(true)
    expect(live.evidence.value?.usage).toEqual(usage)
    expect(wrapper.text()).toBe('36000')
    expect(projectRunActivity(live.events.value).some(item => item.id === usageEvent.id)).toBe(false)

    wrapper.unmount()
  })
})
