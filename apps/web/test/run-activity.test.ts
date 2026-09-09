import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ActivityTimeline from '../app/components/ActivityTimeline.vue'
import { countRunToolCalls, projectRunActivity, toolActivityLabel, toolActivityTarget } from '../app/utils/events'
import { event } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

describe('run activity projection', () => {
  it('projects reasoning and pairs tool lifecycle events by toolCallId', () => {
    const events = [
      event({ id: 'thought', type: 'agent.message', sequence: 1, payload: { kind: 'reasoning', message: 'Inspect the handler first.' } }),
      event({ id: 'started', type: 'tool.started', sequence: 2, payload: { toolCallId: 'call-1', name: 'read', input: { filePath: 'server/internal/handler/issue.go' } } }),
      event({ id: 'completed', type: 'tool.completed', sequence: 3, payload: { toolCallId: 'call-1', name: 'read', resultPreview: 'func Create()' } }),
      event({ id: 'legacy', type: 'tool.completed', sequence: 4, payload: { name: 'bash' } })
    ]
    const items = projectRunActivity(events)
    expect(items).toHaveLength(3)
    expect(items[0]).toMatchObject({ kind: 'thought', message: 'Inspect the handler first.' })
    expect(items[1]).toMatchObject({ kind: 'tool', label: 'Read', target: 'server/internal/handler/issue.go', status: 'completed', resultPreview: 'func Create()' })
    expect(items[2]).toMatchObject({ kind: 'event', title: 'Tool Completed' })
    expect(countRunToolCalls(events)).toBe(1)
  })

  it('keeps same-named calls distinct and projects failed calls', () => {
    const items = projectRunActivity([
      event({ id: 'a', type: 'tool.started', sequence: 1, payload: { toolCallId: 'a', name: 'edit', input: { path: 'a.go' } } }),
      event({ id: 'b', type: 'tool.started', sequence: 2, payload: { toolCallId: 'b', name: 'edit', input: { path: 'b.go' } } }),
      event({ id: 'af', type: 'tool.failed', sequence: 3, payload: { toolCallId: 'a', name: 'edit', reason: 'write failed' } })
    ])
    expect(items).toHaveLength(2)
    expect(items[0]).toMatchObject({ kind: 'tool', toolCallId: 'a', target: 'a.go', status: 'failed', reason: 'write failed' })
    expect(items[1]).toMatchObject({ kind: 'tool', toolCallId: 'b', target: 'b.go', status: 'running' })
  })

  it('formats known and unknown tools and targets without dumping arbitrary input', () => {
    expect(toolActivityLabel('grep')).toBe('Search')
    expect(toolActivityLabel('custom_tool')).toBe('Custom tool')
    expect(toolActivityTarget({ command: ['go', 'test', './...'] })).toBe('go test ./...')
    expect(toolActivityTarget({ nested: { huge: true } })).toBe('')
  })
})

describe('ActivityTimeline run feed', () => {
  it('renders reasoning with the brain icon and compact tool results', () => {
    const wrapper = mount(ActivityTimeline, {
      props: {
        events: [
          event({ id: 'thought', type: 'agent.message', sequence: 1, payload: { kind: 'reasoning', message: 'Inspect handlers.' } }),
          event({ id: 'start', type: 'tool.started', sequence: 2, payload: { toolCallId: 'call', name: 'read', input: { filePath: 'issue.go' } } }),
          event({ id: 'done', type: 'tool.completed', sequence: 3, payload: { toolCallId: 'call', name: 'read', resultPreview: 'func Create()' } })
        ]
      },
      global: { stubs: uiStubs }
    })
    expect(wrapper.find('.i-lucide-brain').exists()).toBe(true)
    expect(wrapper.text()).toContain('Inspect handlers.')
    expect(wrapper.text()).toContain('Read')
    expect(wrapper.text()).toContain('issue.go')
    expect(wrapper.text()).toContain('result: func Create()')
    expect(wrapper.findAll('li')).toHaveLength(2)
  })

  it('renders failed status and reason even when a result preview exists', () => {
    const wrapper = mount(ActivityTimeline, {
      props: {
        events: [
          event({ id: 'start', type: 'tool.started', sequence: 1, payload: { toolCallId: 'call', name: 'edit', input: { path: 'issue.go' } } }),
          event({ id: 'failed', type: 'tool.failed', sequence: 2, payload: { toolCallId: 'call', name: 'edit', resultPreview: 'partial update', reason: 'write failed' } })
        ]
      },
      global: { stubs: uiStubs }
    })
    const tool = wrapper.get('[data-tool-status="failed"]')
    expect(tool.text()).toContain('failed')
    expect(tool.text()).toContain('result: partial update')
    expect(tool.text()).toContain('error: write failed')
  })
})