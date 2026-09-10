import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectList from '../app/components/ProjectList.vue'
import ProjectEditor from '../app/components/ProjectEditor.vue'
import ProjectSettings from '../app/components/ProjectSettings.vue'
import { event, MockEventSource, project } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const global = {
  stubs: {
    ...uiStubs,
    NuxtLink: { props: ['to'], template: '<a :href="to"><slot/></a>' }
  }
}
const button = (wrapper: ReturnType<typeof mount>, label: string) => wrapper.findAll('button').find(candidate => candidate.text() === label)!
const selectSource = async (wrapper: ReturnType<typeof mount>, source: 'local' | 'git') => {
  await wrapper.get(`[data-field=sourceType] input[value="${source}"]`).setValue(true)
}

afterEach(() => {
  MockEventSource.reset()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('ProjectEditor', () => {
  it('creates a local project with uppercase prefix and valid workflow settings', async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify(project), { status: 201 }))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { global })

    await wrapper.get('form').trigger('submit')
    expect(fetch.mock.calls.some(([, options]) => options.method === 'POST')).toBe(false)

    await wrapper.get('[data-field=name] input').setValue('Workspace')
    await wrapper.get('[data-field=issuePrefix] input').setValue('ab')
    await wrapper.get('[data-field=repositoryPath] input').setValue('/repo')
    await wrapper.get('[data-field=defaultBranch] input').setValue('main')
    await wrapper.get('[data-field=workflowSettings] textarea').setValue('{"reviewRequired":true}')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const postCall = fetch.mock.calls.find(([, options]) => options.method === 'POST')?.[1]
    expect(postCall?.method).toBe('POST')
    expect(JSON.parse(String(postCall?.body))).toEqual({
      name: 'Workspace',
      issuePrefix: 'AB',
      sourceType: 'local',
      repositoryPath: '/repo',
      defaultBranch: 'main',
      workflowSettings: { reviewRequired: true }
    })
    expect(wrapper.emitted('saved')?.[0]).toEqual([project])
  })

  it('shows Git source fields and requires a clone URL', async () => {
    const gitProject = {
      ...project,
      sourceType: 'git' as const,
      cloneUrl: 'https://example.com/acme/widget.git',
      sourceRef: 'release/v1',
      repositoryPath: '',
      defaultBranch: ''
    }
    const fetch = vi.fn(async () => new Response(JSON.stringify(gitProject), { status: 201 }))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { global })

    await wrapper.get('[data-field=name] input').setValue('Remote')
    await wrapper.get('[data-field=issuePrefix] input').setValue('REM')
    await selectSource(wrapper, 'git')

    expect(wrapper.find('[data-field=repositoryPath]').exists()).toBe(false)
    expect(wrapper.find('[data-field=defaultBranch]').exists()).toBe(false)
    expect(wrapper.find('[data-field=cloneUrl]').exists()).toBe(true)
    expect(wrapper.find('[data-field=sourceRef]').exists()).toBe(true)

    await wrapper.get('form').trigger('submit')
    expect(fetch.mock.calls.some(([, options]) => options.method === 'POST')).toBe(false)

    await wrapper.get('[data-field=cloneUrl] input').setValue(' https://example.com/acme/widget.git ')
    await wrapper.get('[data-field=sourceRef] input').setValue(' release/v1 ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const postCall = fetch.mock.calls.find(([, options]) => options.method === 'POST')?.[1]
    expect(JSON.parse(String(postCall?.body))).toEqual({
      name: 'Remote',
      issuePrefix: 'REM',
      sourceType: 'git',
      cloneUrl: 'https://example.com/acme/widget.git',
      sourceRef: 'release/v1',
      workflowSettings: {}
    })
  })

  it('rejects invalid prefix and invalid workflow JSON', async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify(project)))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { global })

    await wrapper.get('[data-field=name] input').setValue('Workspace')
    await wrapper.get('[data-field=issuePrefix] input').setValue('1A')
    await wrapper.get('[data-field=repositoryPath] input').setValue('/repo')
    await wrapper.get('[data-field=defaultBranch] input').setValue('main')
    await wrapper.get('form').trigger('submit')
    expect(fetch.mock.calls.some(([, options]) => options.method === 'POST')).toBe(false)

    await wrapper.get('[data-field=issuePrefix] input').setValue('AB')
    await wrapper.get('[data-field=workflowSettings] textarea').setValue('[]')
    await wrapper.get('form').trigger('submit')
    expect(fetch.mock.calls.some(([, options]) => options.method === 'POST')).toBe(false)
  })

  it('prefills repository path from deployment settings only for new local projects', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/repository-settings') {
        return new Response(JSON.stringify({ defaultRepositoryPath: '/repositories', repositoryRoots: ['/repositories'] }))
      }
      return new Response(JSON.stringify(project))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { global })
    await flushPromises()
    expect((wrapper.get('[data-field=repositoryPath] input').element as HTMLInputElement).value).toBe('/repositories')
  })

  it('loads and edits an existing Git source with PATCH', async () => {
    const gitProject = {
      ...project,
      sourceType: 'git' as const,
      cloneUrl: 'https://example.com/acme/widget.git',
      sourceRef: 'main',
      repositoryPath: '',
      defaultBranch: ''
    }
    const fetch = vi.fn(async () => new Response(JSON.stringify(gitProject)))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { props: { project: gitProject }, global })
    await flushPromises()

    expect((wrapper.get('[data-field=cloneUrl] input').element as HTMLInputElement).value).toBe(gitProject.cloneUrl)
    expect((wrapper.get('[data-field=sourceRef] input').element as HTMLInputElement).value).toBe('main')
    await wrapper.get('[data-field=sourceRef] input').setValue('feature/source')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const patchCall = fetch.mock.calls.find(([, options]) => options.method === 'PATCH')?.[1]
    expect(JSON.parse(String(patchCall?.body))).toMatchObject({
      sourceType: 'git',
      cloneUrl: gitProject.cloneUrl,
      sourceRef: 'feature/source'
    })
    expect(JSON.parse(String(patchCall?.body))).not.toHaveProperty('repositoryPath')
    expect(JSON.parse(String(patchCall?.body))).not.toHaveProperty('defaultBranch')
  })

  it('switches an existing Git source to local without sending inactive Git fields', async () => {
    const gitProject = {
      ...project,
      sourceType: 'git' as const,
      cloneUrl: 'https://example.com/acme/widget.git',
      sourceRef: 'main',
      repositoryPath: '',
      defaultBranch: ''
    }
    const fetch = vi.fn(async () => new Response(JSON.stringify(project)))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { props: { project: gitProject }, global })
    await flushPromises()

    await selectSource(wrapper, 'local')
    await wrapper.get('[data-field=repositoryPath] input').setValue('/repo/imported')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const patchCall = fetch.mock.calls.find(([, options]) => options.method === 'PATCH')?.[1]
    const body = JSON.parse(String(patchCall?.body))
    expect(body).toMatchObject({ sourceType: 'local', repositoryPath: '/repo/imported', defaultBranch: 'main' })
    expect(body).not.toHaveProperty('cloneUrl')
    expect(body).not.toHaveProperty('sourceRef')
  })

  it('edits local projects with PATCH and keeps issue prefix immutable', async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify(project)))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { props: { project }, global })
    await flushPromises()

    expect((wrapper.get('[data-field=issuePrefix] input').element as HTMLInputElement).disabled).toBe(true)
    await wrapper.get('[data-field=name] input').setValue('Renamed')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const patchCall = fetch.mock.calls.find(([, options]) => options.method === 'PATCH')?.[1]
    expect(patchCall?.method).toBe('PATCH')
    const body = JSON.parse(String(patchCall?.body))
    expect(body).toEqual({
      name: 'Renamed',
      sourceType: 'local',
      repositoryPath: '/repo',
      defaultBranch: 'main',
      workflowSettings: {},
      allowInternalRunner: true
    })
    expect(body).not.toHaveProperty('issuePrefix')
  })

  it('guards duplicate submits while saving and clears loading after failure', async () => {
    let resolvePost: ((response: Response) => void) | undefined
    const fetch = vi.fn((path: string, options: RequestInit = {}) => {
      if (options.method === 'POST') {
        return new Promise<Response>(resolve => { resolvePost = resolve })
      }
      return Promise.resolve(new Response(JSON.stringify({ defaultRepositoryPath: '', repositoryRoots: [] })))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { global })
    await flushPromises()

    await wrapper.get('[data-field=name] input').setValue('Workspace')
    await wrapper.get('[data-field=issuePrefix] input').setValue('AB')
    await wrapper.get('[data-field=repositoryPath] input').setValue('/repo')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')

    expect(fetch.mock.calls.filter(([, options]) => options.method === 'POST')).toHaveLength(1)
    expect((button(wrapper, 'Create project').element as HTMLButtonElement).disabled).toBe(true)

    resolvePost?.(new Response(JSON.stringify(project), { status: 201 }))
    await flushPromises()
    expect((button(wrapper, 'Create project').element as HTMLButtonElement).disabled).toBe(false)

    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'validation_error', message: 'raw detail' } }), { status: 400 })))
    const failed = mount(ProjectEditor, { props: { project }, global })
    await failed.get('form').trigger('submit')
    await flushPromises()
    expect((button(failed, 'Save project').element as HTMLButtonElement).disabled).toBe(false)
    expect(failed.text()).toContain('Check the form values')
    expect(failed.text()).not.toContain('raw detail')
  })

  it('emits cancel', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(project))))
    const wrapper = mount(ProjectEditor, { props: { project }, global })
    await button(wrapper, 'Cancel').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })
})

describe('ProjectList', () => {
  it('renders local and Git project source summaries', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[]')))
    const wrapper = mount(ProjectList, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('No projects yet')
    expect(wrapper.text()).toContain('Create a Project with a local repository or Git repository source to open a board.')
    wrapper.unmount()

    const gitProject = {
      ...project,
      id: 'project-git',
      name: 'Remote',
      sourceType: 'git' as const,
      cloneUrl: 'https://example.com/acme/widget.git',
      sourceRef: null,
      repositoryPath: '',
      defaultBranch: ''
    }
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([project, gitProject]))))
    const listed = mount(ProjectList, { global })
    await flushPromises()
    expect(listed.text()).toContain('AB · /repo · main')
    expect(listed.text()).toContain('https://example.com/acme/widget.git · remote default')
    expect(listed.text()).toContain('Local repository')
    expect(listed.text()).toContain('Git repository')
    expect(listed.text()).toContain('Open board')
    listed.unmount()
  })

  it('creates a project from the modal and refreshes the list', async () => {
    const created = { ...project, id: 'project-new', name: 'Created' }
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path === '/api/repository-settings') {
        return new Response(JSON.stringify({ defaultRepositoryPath: '/repositories', repositoryRoots: ['/repositories'] }))
      }
      if (options.method === 'POST') {
        return new Response(JSON.stringify(created), { status: 201 })
      }
      return new Response(JSON.stringify([]))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectList, { global })
    await flushPromises()

    await button(wrapper, 'New project').trigger('click')
    await wrapper.get('[data-field=name] input').setValue('Created')
    await wrapper.get('[data-field=issuePrefix] input').setValue('AB')
    await wrapper.get('[data-field=defaultBranch] input').setValue('main')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[role=dialog]').exists()).toBe(false)
    expect(fetch.mock.calls.some(([, options]) => options.method === 'POST')).toBe(true)
    expect(fetch.mock.calls.filter(([path, options]) => path === '/api/projects' && options.method === 'GET').length).toBeGreaterThanOrEqual(2)
    wrapper.unmount()
  })

  it('edits an existing project from the modal', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (options.method === 'PATCH') return new Response(JSON.stringify({ ...project, name: 'Renamed' }))
      return new Response(JSON.stringify([project]))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectList, { global })
    await flushPromises()

    await button(wrapper, 'Edit').trigger('click')
    await wrapper.get('[data-field=name] input').setValue('Renamed')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(fetch.mock.calls.some(([, options]) => options.method === 'PATCH')).toBe(true)
    expect(wrapper.find('[role=dialog]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('refreshes listed projects from Project SSE without a loading skeleton', async () => {
    vi.stubGlobal('EventSource', MockEventSource)
    let listed = [project]
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(listed))))
    const wrapper = mount(ProjectList, { global })
    await flushPromises()
    expect(wrapper.text()).toContain('Workspace')
    expect(MockEventSource.instances[0]?.url).toBe('/api/projects/project-a/events')

    listed = [{ ...project, name: 'Renamed live' }]
    MockEventSource.instances[0]?.emit(event({ id: 'evt-1', type: 'issue.updated', sequence: null }))
    await flushPromises()
    expect(wrapper.text()).not.toContain('Loading')
    expect(wrapper.text()).toContain('Renamed live')
    wrapper.unmount()
  })

  it('navigates to the board when the project row is clicked', async () => {
    const navigateTo = vi.fn()
    vi.stubGlobal('navigateTo', navigateTo)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([project]))))
    const wrapper = mount(ProjectList, { global })
    await flushPromises()

    await wrapper.get('[role=link]').trigger('click')
    expect(navigateTo).toHaveBeenCalledWith('/projects/project-a/board')

    await wrapper.get('[role=link]').trigger('keydown.enter')
    expect(navigateTo).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('opens edit from the action buttons without navigating the row', async () => {
    const navigateTo = vi.fn()
    vi.stubGlobal('navigateTo', navigateTo)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([project]))))
    const wrapper = mount(ProjectList, { global })
    await flushPromises()

    await button(wrapper, 'Edit').trigger('click')
    expect(navigateTo).not.toHaveBeenCalled()
    expect(wrapper.find('[role=dialog]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Edit project')
    wrapper.unmount()
  })
})

describe('ProjectSettings', () => {
  it('loads the shared Project editor inside project settings', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(project))))
    const wrapper = mount(ProjectSettings, { props: { projectId: project.id }, global })
    await flushPromises()

    expect(wrapper.text()).toContain('Project')
    expect(wrapper.text()).toContain('repository source context')
    expect((wrapper.get('[data-field=name] input').element as HTMLInputElement).value).toBe('Workspace')
    expect((wrapper.get('[data-field=issuePrefix] input').element as HTMLInputElement).disabled).toBe(true)
    expect(wrapper.find('[data-field=sourceType]').exists()).toBe(true)
    expect(wrapper.find('[data-kind]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('saves allowInternalRunner with project edits', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (options.method === 'PATCH') {
        const body = JSON.parse(String(options.body))
        return new Response(JSON.stringify({ ...project, allowInternalRunner: body.allowInternalRunner }))
      }
      return new Response(JSON.stringify(project))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectSettings, { props: { projectId: project.id }, global })
    await flushPromises()

    const toggle = wrapper.get('[data-field=allowInternalRunner] input')
    await toggle.setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const patchCall = fetch.mock.calls.find(([, options]) => options.method === 'PATCH')
    expect(JSON.parse(String(patchCall?.[1]?.body)).allowInternalRunner).toBe(false)
    wrapper.unmount()
  })

  it('saves edits and discards unsaved changes on cancel', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (options.method === 'PATCH') return new Response(JSON.stringify({ ...project, name: 'Renamed' }))
      return new Response(JSON.stringify(project))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectSettings, { props: { projectId: project.id }, global })
    await flushPromises()

    await wrapper.get('[data-field=name] input').setValue('Renamed')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(fetch.mock.calls.some(([, options]) => options.method === 'PATCH')).toBe(true)
    expect(wrapper.text()).toContain('Saved')

    await wrapper.get('[data-field=name] input').setValue('Unsaved')
    await button(wrapper, 'Cancel').trigger('click')
    expect((wrapper.get('[data-field=name] input').element as HTMLInputElement).value).toBe('Renamed')
    wrapper.unmount()
  })

  it('shows load errors without rendering the editor', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('missing', { status: 404 })))
    const wrapper = mount(ProjectSettings, { props: { projectId: project.id }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('This resource is unavailable or belongs to another project.')
    expect(wrapper.find('form').exists()).toBe(false)
    wrapper.unmount()
  })
})