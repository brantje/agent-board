import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import QuestionPanel from '../app/components/QuestionPanel.vue'
import { event, MockEventSource, question, run } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const global = { stubs: { ...uiStubs, NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }
const button = (wrapper: ReturnType<typeof mount>, label: string) => wrapper.findAll('button').find(value => value.text() === label)!

afterEach(() => {
  vi.unstubAllGlobals()
  MockEventSource.reset()
})

describe('QuestionPanel', () => {
  it('loads open blocking questions for an Issue and replaces local state from the answer response', async () => {
    const open = question()
    const answered = { ...open, status: 'ANSWERED', answeredAt: '2026-01-01T00:03:00.000Z' }
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (options.method === 'POST') {
        return new Response(JSON.stringify({
          question: answered,
          decisionId: 'decision-1',
          run: { ...run, status: 'QUEUED' },
          resumeJobId: 'job-1'
        }))
      }
      if (path !== '/api/projects/project-a/questions?issueId=AB-1&status=OPEN') {
        throw new Error(`unexpected path ${path}`)
      }
      return new Response(JSON.stringify([open]))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(QuestionPanel, { props: { projectId: 'project-a', issueId: 'AB-1' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Which strategy?')
    expect(wrapper.text()).toContain('Blocking')
    expect(wrapper.text()).toContain('Recommended: Safe')
    await wrapper.get('input[type=radio][value=safe]').setValue()
    await button(wrapper, 'Answer').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(JSON.parse(String(fetch.mock.calls.find(([, options]) => options?.method === 'POST')?.[1]?.body))).toEqual({
      kind: 'SINGLE_CHOICE',
      optionIds: ['safe']
    })
    expect(wrapper.text()).not.toContain('Which strategy?')
  })

  it('submits custom alternate text for a choice question and surfaces API errors', async () => {
    const open = question({ custom: true })
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (options.method === 'POST') {
        return new Response(JSON.stringify({ error: { code: 'conflict', message: 'raw' } }), { status: 409 })
      }
      return new Response(JSON.stringify([open]))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(QuestionPanel, { props: { projectId: 'project-a', runId: 'run-1' }, global })
    await flushPromises()
    expect(fetch.mock.calls[0]?.[0]).toBe('/api/projects/project-a/questions?runId=run-1&status=OPEN')

    await wrapper.get('input[type=checkbox]').setValue()
    await wrapper.get('textarea').setValue('Use a third path')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(JSON.parse(String(fetch.mock.calls.find(([, options]) => options?.method === 'POST')?.[1]?.body))).toEqual({
      kind: 'SINGLE_CHOICE',
      text: 'Use a third path'
    })
    expect(wrapper.text()).toContain('conflicts with existing state')
    expect(wrapper.text()).not.toContain('raw')
    expect(wrapper.text()).toContain('Which strategy?')
  })

  it('submits free-text and multi-choice answers', async () => {
    const text = question({ id: 'q-text', kind: 'TEXT', options: [], recommendation: undefined, custom: false, prompt: 'Explain the approach' })
    const multi = question({ id: 'q-multi', kind: 'MULTI_CHOICE', prompt: 'Pick checks', custom: false, recommendation: undefined })
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (options.method === 'POST') {
        const body = JSON.parse(String(options.body))
        return new Response(JSON.stringify({
          question: { ...(body.kind === 'TEXT' ? text : multi), status: 'ANSWERED' },
          decisionId: 'd',
          run
        }))
      }
      return new Response(JSON.stringify([text, multi]))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(QuestionPanel, { props: { projectId: 'project-a', issueId: 'AB-1' }, global })
    await flushPromises()

    await wrapper.get('textarea').setValue('Because the lease must persist')
    await wrapper.findAll('form')[0]!.trigger('submit')
    await flushPromises()
    expect(JSON.parse(String(fetch.mock.calls[1]?.[1]?.body))).toEqual({ kind: 'TEXT', text: 'Because the lease must persist' })

    const boxes = wrapper.findAll('input[type=checkbox]')
    await boxes[0]!.setValue()
    await boxes[1]!.setValue()
    await wrapper.findAll('form').at(-1)!.trigger('submit')
    await flushPromises()
    expect(JSON.parse(String(fetch.mock.calls.at(-1)?.[1]?.body))).toEqual({ kind: 'MULTI_CHOICE', optionIds: ['safe', 'fast'] })
  })

  it('shows a Question-specific empty state instead of generic new-work copy', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[]')))
    const wrapper = mount(QuestionPanel, { props: { projectId: 'project-a', issueId: 'AB-1' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('No open questions')
    expect(wrapper.text()).toContain('When a Run needs a human decision, the Agent\'s question will appear here.')
    expect(wrapper.text()).not.toContain('New work will appear here when it is created.')
  })

  it('refreshes open questions from Project SSE without a loading skeleton', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    let questions: ReturnType<typeof question>[] = []
    const fetch = vi.fn(async (path: string) => {
      if (path.includes('/questions')) return new Response(JSON.stringify(questions))
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(QuestionPanel, { props: { projectId: 'project-a', issueId: 'AB-1' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('No open questions')
    expect(MockEventSource.instances[0]?.url).toBe('/api/projects/project-a/events')

    questions = [question({ issueId: 'AB-1' })]
    MockEventSource.instances[0]?.emit(event({ id: 'evt-q', type: 'question.created', sequence: null, runId: run.id }))
    await flushPromises()
    expect(wrapper.text()).not.toContain('Loading')
    expect(wrapper.text()).toContain('Which strategy?')
    wrapper.unmount()
  })

  it('refreshes run-scoped questions only for matching runId events', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    let questions: ReturnType<typeof question>[] = []
    const fetch = vi.fn(async (path: string) => {
      if (path.includes('/questions')) return new Response(JSON.stringify(questions))
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(QuestionPanel, { props: { projectId: 'project-a', runId: 'run-1' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('No open questions')

    questions = [question({ runId: 'run-1', prompt: 'Pick a marker' })]
    MockEventSource.instances[0]?.emit(event({ id: 'evt-other', type: 'question.created', sequence: null, runId: 'run-2' }))
    await flushPromises()
    expect(wrapper.text()).toContain('No open questions')

    MockEventSource.instances[0]?.emit(event({ id: 'evt-q', type: 'question.created', sequence: null, runId: 'run-1' }))
    await flushPromises()
    expect(wrapper.text()).toContain('Pick a marker')
    wrapper.unmount()
  })
})
