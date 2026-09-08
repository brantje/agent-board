import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ReviewDetail from '../app/components/ReviewDetail.vue'
import { candidateArtifacts, reviewDetail, run } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const global = { stubs: { ...uiStubs, NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }
const button = (wrapper: ReturnType<typeof mount>, label: string) => wrapper.findAll('button').find(value => value.text() === label)!

afterEach(() => vi.unstubAllGlobals())

describe('ReviewDetail', () => {
  it('shows complete candidate evidence and never treats NOT_RUN as success', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(reviewDetail()))))
    const wrapper = mount(ReviewDetail, { props: { projectId: 'project-a', reviewId: 'review-1' }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Tests were not run')
    expect(wrapper.text()).not.toMatch(/tests passed/i)
    expect(wrapper.text()).toContain('candidate-staged.patch')
    expect(wrapper.text()).toContain('candidate-unstaged.patch')
    expect(wrapper.text()).toContain('candidate-manifest.json')
    expect(wrapper.text()).toContain('new.txt')
    expect(wrapper.text()).toContain('gone.txt')
    expect(wrapper.text()).toContain('old.txt')
    expect(wrapper.text()).toContain('renamed.txt')
    expect(wrapper.text()).toContain('git status')
    expect(wrapper.text()).toContain('Candidate is ready.')
    expect(wrapper.get('a[href="/api/projects/project-a/runs/run-1/artifacts/art-staged"]').exists()).toBe(true)
    expect(candidateArtifacts.map(item => item.name).every(name => wrapper.text().includes(name))).toBe(true)
  })

  it('approves only from the API response and shows DONE from returned Issue state', async () => {
    const detail = reviewDetail()
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path.endsWith('/approve')) {
        return new Response(JSON.stringify({
          review: { ...detail.review, status: 'APPROVED', decidedAt: '2026-01-01T00:11:00.000Z' },
          decision: { id: 'd1', outcome: 'APPROVED', actorType: 'HUMAN', actorId: 'user', safeDetails: {}, createdAt: '2026-01-01T00:11:00.000Z' },
          run: { ...run, status: 'COMPLETED', completedAt: '2026-01-01T00:11:00.000Z' },
          issue: { id: 'AB-1', projectId: 'project-a', title: 'Fix', description: '', status: 'DONE', priority: 0, assignedAgentId: 'agent-1', createdAt: '', updatedAt: '' }
        }))
      }
      return new Response(JSON.stringify(detail))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ReviewDetail, { props: { projectId: 'project-a', reviewId: 'review-1' }, global })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Board status: Done')
    await button(wrapper, 'Approve').trigger('click')
    await flushPromises()
    expect(fetch.mock.calls.some(([path, options]) => path.endsWith('/approve') && options?.method === 'POST')).toBe(true)
    expect(wrapper.text()).toContain('Approved')
    expect(wrapper.text()).toContain('Board status: Done')
    expect(wrapper.text()).toContain('Run · Completed')
  })

  it('requests changes with feedback and surfaces conflict without pretending the Issue is DONE', async () => {
    const detail = reviewDetail()
    let conflict = true
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path.endsWith('/request-changes')) {
        if (conflict) return new Response(JSON.stringify({ error: { code: 'conflict', message: 'stale' } }), { status: 409 })
        return new Response(JSON.stringify({
          review: { ...detail.review, status: 'CHANGES_REQUESTED' },
          decision: { id: 'd2', outcome: 'CHANGES_REQUESTED', actorType: 'HUMAN', actorId: 'user', safeDetails: { feedback: 'Fix tests' }, createdAt: '2026-01-01T00:12:00.000Z' },
          run: { ...run, id: 'run-2', attempt: 2, status: 'QUEUED' },
          issue: { id: 'AB-1', projectId: 'project-a', title: 'Fix', description: '', status: 'IN_PROGRESS', priority: 0, assignedAgentId: 'agent-1', createdAt: '', updatedAt: '' },
          jobId: 'job-9'
        }), { status: 202 })
      }
      return new Response(JSON.stringify(detail))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ReviewDetail, { props: { projectId: 'project-a', reviewId: 'review-1' }, global })
    await flushPromises()
    await wrapper.get('textarea').setValue('Fix tests')
    await button(wrapper, 'Request changes').trigger('click')
    await wrapper.findAll('form').at(-1)!.trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('conflicts with existing state')
    expect(wrapper.text()).not.toContain('DONE')
    expect(wrapper.text()).not.toContain('stale')

    conflict = false
    await wrapper.findAll('form').at(-1)!.trigger('submit')
    await flushPromises()
    expect(JSON.parse(String(fetch.mock.calls.at(-1)?.[1]?.body))).toEqual({ feedback: 'Fix tests' })
    expect(wrapper.text()).toContain('Changes requested')
    expect(wrapper.text()).toContain('Run · Queued')
    expect(wrapper.text()).toContain('job-9')
    expect(wrapper.text()).toContain('Board status: In Progress')
    expect(wrapper.get('a[href="/projects/project-a/runs/run-2"]').exists()).toBe(true)
  })
})
