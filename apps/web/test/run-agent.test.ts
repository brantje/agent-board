import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RunAgentCard from '../app/components/RunAgentCard.vue'
import { runAgentInfo } from '../app/utils/runs'
import { evidence } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

describe('runAgentInfo', () => {
  it('reads immutable agent, model, provider and runner from provenance context', () => {
    expect(runAgentInfo(evidence().provenance)).toEqual({
      name: 'Coder',
      engine: 'opencode',
      model: 'openai/gpt-test',
      provider: 'OpenRouter',
      runner: 'lab-host'
    })
  })

  it('reads runner name from provenance context', () => {
    expect(runAgentInfo({
      context: {
        agent: { name: 'Coder', engine: 'opencode' },
        runner: { id: 'runner-1', name: 'build-host' }
      }
    })).toMatchObject({ name: 'Coder', runner: 'build-host' })
  })

  it('falls back to model name and provider kind when richer labels are absent', () => {
    expect(runAgentInfo({
      context: {
        agent: { name: 'Builder', engine: 'scripted' },
        model: { name: 'local-model' },
        provider: { kind: 'openai' }
      }
    })).toEqual({
      name: 'Builder',
      engine: 'scripted',
      model: 'local-model',
      provider: 'openai',
      runner: undefined
    })
  })

  it('returns null when provenance has no agent configuration', () => {
    expect(runAgentInfo(null)).toBeNull()
    expect(runAgentInfo({})).toBeNull()
    expect(runAgentInfo({ context: { agent: { name: '   ' } } })).toBeNull()
  })

  it('reads a flat provenance snapshot without a nested context wrapper', () => {
    expect(runAgentInfo({
      agent: { name: 'Coder', engine: 'opencode' },
      model: { model: 'openai/gpt-test' }
    })).toMatchObject({ name: 'Coder', engine: 'opencode', model: 'openai/gpt-test' })
  })
})

describe('RunAgentCard', () => {
  it('renders human-readable agent identity above usage consumers', () => {
    const wrapper = mount(RunAgentCard, {
      props: { provenance: evidence().provenance },
      global: { stubs: uiStubs }
    })

    expect(wrapper.get('[data-run-agent]').text()).toContain('Coder')
    expect(wrapper.text()).toContain('Engine')
    expect(wrapper.text()).toContain('opencode')
    expect(wrapper.text()).toContain('openai/gpt-test')
    expect(wrapper.text()).toContain('OpenRouter')
    expect(wrapper.text()).toContain('Runner')
    expect(wrapper.text()).toContain('lab-host')
    expect(wrapper.text()).not.toContain('agent-1')
  })

  it('keeps an explicit empty state when provenance has not been recorded', () => {
    const wrapper = mount(RunAgentCard, { global: { stubs: uiStubs } })
    expect(wrapper.text()).toContain('No agent recorded.')
  })

  it('labels an agent without a name when other provenance fields exist', () => {
    const wrapper = mount(RunAgentCard, {
      props: { provenance: { context: { agent: { engine: 'opencode' } } } },
      global: { stubs: uiStubs }
    })
    expect(wrapper.text()).toContain('Unnamed agent')
    expect(wrapper.text()).toContain('opencode')
  })

  it('omits missing engine, provider and runner rows', () => {
    const wrapper = mount(RunAgentCard, {
      props: { provenance: { context: { agent: { name: 'Coder' }, model: { name: 'gpt-test' } } } },
      global: { stubs: uiStubs }
    })
    expect(wrapper.text()).toContain('Coder')
    expect(wrapper.text()).toContain('gpt-test')
    expect(wrapper.text()).not.toContain('Engine')
    expect(wrapper.text()).not.toContain('Provider')
    expect(wrapper.text()).not.toContain('Runner')
  })
})