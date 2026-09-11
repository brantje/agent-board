import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProjectEditor from '../app/components/ProjectEditor.vue'
import { project } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const global = {
  stubs: {
    ...uiStubs,
    UButton: {
      props: ['label', 'disabled', 'loading', 'type'],
      emits: ['click'],
      template: '<button :type="type || \'button\'" :disabled="disabled || loading" @click="$emit(\'click\')"><span>{{label}}</span><span v-if="loading" role="status">Saving</span><slot/></button>'
    }
  }
}

const selectSource = async (wrapper: ReturnType<typeof mount>, source: 'local' | 'git') => {
  await wrapper.get(`[data-field=sourceType] input[value="${source}"]`).setValue(true)
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ProjectEditor source review coverage', () => {
  it('switches a local Project to Git through the shared editor source path', async () => {
    const saved = {
      ...project,
      sourceType: 'git' as const,
      cloneUrl: 'https://example.com/acme/widget.git',
      sourceRef: 'release/v1',
      repositoryPath: '',
      defaultBranch: ''
    }
    const fetch = vi.fn(async () => new Response(JSON.stringify(saved)))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { props: { project }, global })

    expect(wrapper.find('[data-field=sourceType]').exists()).toBe(true)
    expect(wrapper.find('[data-field=repositoryPath]').exists()).toBe(true)
    expect(wrapper.find('[data-field=defaultBranch]').exists()).toBe(true)
    expect(wrapper.find('[data-field=cloneUrl]').exists()).toBe(false)

    await selectSource(wrapper, 'git')
    expect(wrapper.find('[data-field=repositoryPath]').exists()).toBe(false)
    expect(wrapper.find('[data-field=defaultBranch]').exists()).toBe(false)
    expect(wrapper.find('[data-field=cloneUrl]').exists()).toBe(true)
    expect(wrapper.find('[data-field=sourceRef]').exists()).toBe(true)

    await wrapper.get('[data-field=cloneUrl] input').setValue(' https://example.com/acme/widget.git ')
    await wrapper.get('[data-field=sourceRef] input').setValue(' release/v1 ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const patchCall = fetch.mock.calls.find(([, options]) => options.method === 'PATCH')?.[1]
    const body = JSON.parse(String(patchCall?.body))
    expect(body).toMatchObject({
      sourceType: 'git',
      cloneUrl: 'https://example.com/acme/widget.git',
      sourceRef: 'release/v1'
    })
    expect(body).not.toHaveProperty('repositoryPath')
    expect(body).not.toHaveProperty('defaultBranch')
  })

  it('hydrates a Git Project, changes its clone URL, and clears its optional ref', async () => {
    const gitProject = {
      ...project,
      sourceType: 'git' as const,
      cloneUrl: 'https://example.com/acme/widget.git',
      sourceRef: 'release/v1',
      repositoryPath: '',
      defaultBranch: ''
    }
    const saved = { ...gitProject, cloneUrl: 'https://example.com/acme/widget-v2.git', sourceRef: null }
    const fetch = vi.fn(async () => new Response(JSON.stringify(saved)))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { props: { project: gitProject }, global })

    expect((wrapper.get('[data-field=cloneUrl] input').element as HTMLInputElement).value).toBe(gitProject.cloneUrl)
    expect((wrapper.get('[data-field=sourceRef] input').element as HTMLInputElement).value).toBe('release/v1')

    await wrapper.get('[data-field=cloneUrl] input').setValue('https://example.com/acme/widget-v2.git')
    await wrapper.get('[data-field=sourceRef] input').setValue('')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const patchCall = fetch.mock.calls.find(([, options]) => options.method === 'PATCH')?.[1]
    expect(JSON.parse(String(patchCall?.body))).toMatchObject({
      sourceType: 'git',
      cloneUrl: 'https://example.com/acme/widget-v2.git',
      sourceRef: ''
    })
  })

  it('shows immediate loading, prevents duplicate saves, and clears loading on failure', async () => {
    let resolvePatch: ((response: Response) => void) | undefined
    const fetch = vi.fn((_path: string, options: RequestInit = {}) => {
      if (options.method === 'PATCH') {
        return new Promise<Response>(resolve => { resolvePatch = resolve })
      }
      return Promise.resolve(new Response(JSON.stringify(project)))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ProjectEditor, { props: { project }, global })

    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[role=status]').text()).toBe('Saving')
    expect(wrapper.find('progress').exists()).toBe(false)

    await wrapper.get('form').trigger('submit')
    expect(fetch.mock.calls.filter(([, options]) => options.method === 'PATCH')).toHaveLength(1)

    resolvePatch?.(new Response(JSON.stringify({ error: { code: 'validation_error', message: 'raw detail' } }), { status: 400 }))
    await flushPromises()

    expect(wrapper.find('[role=status]').exists()).toBe(false)
    expect((wrapper.get('button[type=submit]').element as HTMLButtonElement).disabled).toBe(false)
    expect(wrapper.text()).toContain('Unable to save Project')
    expect(wrapper.text()).toContain('Check the form values')
    expect(wrapper.text()).not.toContain('raw detail')
  })
})
