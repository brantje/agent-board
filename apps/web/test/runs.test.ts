import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RunList from '../app/components/RunList.vue'
import RunDetail from '../app/components/RunDetail.vue'
import { runStatusLabel } from '../app/utils/runs'
import { event, evidence, MockEventSource, project, run } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const global = {
  stubs: {
    ...uiStubs,
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' },
    QuestionPanel: { props: ['projectId', 'runId', 'issueId'], template: '<section>Questions for {{runId || issueId}}</section>' },
    ActivityTimeline: { props: ['events'], template: '<ol><li v-for="item in events" :key="item.id">{{item.type}} {{item.payload?.kind}} {{item.payload?.message}}</li></ol>' }
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  MockEventSource.reset()
})

describe('run status labels', () => {
  it('keeps Run status copy distinct from Board columns', () => {
    expect(runStatusLabel('QUEUED')).toBe('Run · Queued')
    expect(runStatusLabel('READY_FOR_REVIEW')).toBe('Run · Ready For Review')
    expect(runStatusLabel('READY_FOR_REVIEW')).not.toBe('Review')
  })
})

describe('RunList', () => {
  it('composes global Runs from projects then per-project lists and links into Run detail', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/projects') return new Response(JSON.stringify([project]))
      if (path === '/api/projects/project-a/runs') {
        return new Response(JSON.stringify([
          { ...run, status: 'FAILED', failureReason: 'engine', queueReason: null },
          { ...run, id: 'run-2', attempt: 2, status: 'QUEUED', queueReason: 'capacity' }
        ]))
      }
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(RunList, { global })
    await flushPromises()
    expect(wrapper.get('a[href="/projects/project-a/runs/run-1"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Run · Failed')
    expect(wrapper.text()).toContain('engine')
    expect(wrapper.text()).toContain('Queue reason: capacity')
    expect(wrapper.text()).toContain('Attempt 2')
    wrapper.unmount()
  })

  it('shows a Run-specific empty state instead of generic new-work copy', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => new Response(JSON.stringify(path === '/api/projects' ? [project] : []))))
    const wrapper = mount(RunList, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('No Runs yet')
    expect(wrapper.text()).toContain('Runs appear after an Agent is assigned to an Issue. Execution continues on the server.')
    expect(wrapper.text()).not.toContain('New work will appear here when it is created.')
    wrapper.unmount()
  })

  it('scopes a project Runs index without a global API', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path === '/api/projects/project-a/runs') return new Response(JSON.stringify([run]))
      return new Response(JSON.stringify([project]))
    }))
    const wrapper = mount(RunList, { props: { projectId: 'project-a' }, global })
    await flushPromises()
    expect(wrapper.get('a[href="/projects/project-a/runs/run-1"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps partial Project failures visible on the global Runs index', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/projects') return new Response(JSON.stringify([project, { ...project, id: 'project-b', name: 'Other' }]))
      if (path === '/api/projects/project-b/runs') return new Response(JSON.stringify({ error: { code: 'project_not_found' } }), { status: 404 })
      if (path === '/api/projects/project-a/runs') return new Response(JSON.stringify([run]))
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(RunList, { global })
    await flushPromises()
    expect(wrapper.get('a[href="/projects/project-a/runs/run-1"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Other')
    expect(wrapper.text()).toContain('unavailable or belongs to another project')
    wrapper.unmount()
  })

  it('keeps all-project Run fetch failures visible instead of the empty state', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/projects') return new Response(JSON.stringify([project, { ...project, id: 'project-b', name: 'Other' }]))
      return new Response(JSON.stringify({ error: { code: 'project_not_found' } }), { status: 404 })
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(RunList, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('Other')
    expect(wrapper.text()).toContain('unavailable or belongs to another project')
    expect(wrapper.text()).not.toContain('No Runs yet')
    wrapper.unmount()
  })

  it('shows an error when the Project list cannot be loaded', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'project_not_found' } }), { status: 404 })))
    const wrapper = mount(RunList, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('unavailable or belongs to another project')
    wrapper.unmount()
  })
})

describe('RunDetail', () => {
  it('renders the two-region working-state layout, evidence disclosure, and live timeline', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    const fileChange = event({ id: 'file', type: 'file.modified', sequence: 2, payload: { path: 'README.md', artifactId: 'art-1' } })
    const snapshot = evidence({
      events: [
        event({ id: 'one', type: 'agent.message', sequence: 1, payload: { kind: 'progress', message: 'Editing files' } }),
        fileChange
      ],
      fileChanges: [fileChange],
      artifacts: [{ id: 'art-1', name: 'README.md', kind: 'candidate_file', mediaType: 'text/plain', sizeBytes: 1, digest: null, safeMetadata: {}, contentPath: 'artifacts/readme', createdAt: '2026-01-01T00:01:00.000Z' }],
      tests: [event({ id: 'test-1', type: 'test.completed', sequence: 3, payload: { status: 'PASSED', command: ['go', 'test'] } })]
    })
    const fetch = vi.fn(async (path: string) => {
      if (path.endsWith('/evidence')) return new Response(JSON.stringify(snapshot))
      if (path.includes('/raw-output/chunk-1')) return new Response('hello log', { headers: { 'Content-Type': 'text/plain' } })
      if (path.includes('/questions')) return new Response(JSON.stringify([]))
      if (path.includes('/reviews')) return new Response(JSON.stringify([]))
      return new Response(JSON.stringify(snapshot.run))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(RunDetail, { props: { projectId: 'project-a', runId: 'run-1' }, global })
    await flushPromises()

    expect(wrapper.find('.detail-grid').exists()).toBe(true)
    expect(wrapper.text()).toContain('Run · Running')
    expect(wrapper.text()).toContain('Commands 1')
    expect(wrapper.text()).toContain('Files 1')
    expect(wrapper.text()).toContain('Tests 1')
    expect(wrapper.text()).toContain('instance-1')
    expect(wrapper.text()).toContain('runtime-1')
    expect(wrapper.text()).toContain('git status')
    expect(wrapper.text()).toContain('go test')
    expect(wrapper.text()).toContain('Questions for run-1')
    expect(wrapper.get('a[href="/projects/project-a/issues/AB-1"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/api/projects/project-a/runs/run-1/artifacts/art-1"]').exists()).toBe(true)

    await wrapper.findAll('button').find(value => value.text() === 'Load log')!.trigger('click')
    await flushPromises()
    expect(fetch.mock.calls.some(([path]) => path === '/api/projects/project-a/runs/run-1/raw-output/chunk-1')).toBe(true)
    expect(wrapper.text()).toContain('hello log')
    expect(wrapper.text()).toContain('live')
    wrapper.unmount()
  })

  it('shows reconnecting without rewriting the Run as FAILED', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/evidence')) return new Response(JSON.stringify(evidence({ run: { ...run, status: 'RUNNING' }, events: [event({ id: 'one', type: 'run.started', sequence: 1 })] })))
      return new Response(JSON.stringify([]))
    }))
    const wrapper = mount(RunDetail, { props: { projectId: 'project-a', runId: 'run-1' }, global })
    await flushPromises()
    MockEventSource.instances[0]?.fail()
    await flushPromises()
    expect(wrapper.text()).toContain('Reconnecting')
    expect(wrapper.text()).toContain('not a Run failure')
    expect(wrapper.text()).toContain('Run · Running')
    expect(wrapper.text()).not.toContain('Run · Failed')
    wrapper.unmount()
  })
})
