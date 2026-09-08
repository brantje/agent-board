import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ConfigManager from '../app/components/ConfigManager.vue'
import { CUSTOM_PROVIDER_KIND, definitions, type ConfigKind } from '../app/utils/configuration'
import { uiStubs } from './ui-stubs'

const global = { stubs: uiStubs }
const button = (wrapper: ReturnType<typeof mount>, label: string) => wrapper.findAll('button').find(candidate => candidate.text() === label)!

afterEach(() => vi.unstubAllGlobals())

describe('configuration screens', () => {
  it.each(Object.keys(definitions) as ConfigKind[])('creates and edits %s with intentional methods', async kind => {
    const records = [{
      id: 'one',
      name: 'Existing',
      projectId: null,
      enabled: true,
      state: 'ENABLED',
      healthStatus: 'UNKNOWN',
      repositoryPath: '/repo',
      defaultBranch: 'main',
      issuePrefix: 'AB',
      image: 'runner',
      networkPolicy: 'none',
      workspacePolicy: 'issue',
      safeMetadata: {},
      generationSettings: {}
    }]
    const fetch = vi.fn(async (_path: string, options: RequestInit) => new Response(JSON.stringify(options.method === 'GET' ? records : records[0])))
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Existing')
    await button(wrapper, `New ${definitions[kind].singular.toLowerCase()}`).trigger('click')
    for (const field of definitions[kind].fields) {
      const control = wrapper.find(`[data-field="${field.key}"] input, [data-field="${field.key}"] textarea, [data-field="${field.key}"] select`)
      if (field.required && field.type !== 'number') {
        if (kind === 'providers' && field.key === 'kind') {
          await control.setValue('anthropic')
        } else {
          await control.setValue(field.resource ? 'one' : field.key === 'repositoryPath' ? '/repo' : field.key === 'issuePrefix' ? 'AB' : 'Value')
        }
      }
    }
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(fetch.mock.calls.some(([, options]) => options.method === 'POST')).toBe(true)
    expect(wrapper.text()).toContain('Saved')
    await button(wrapper, 'Edit').trigger('click')
    for (const field of definitions[kind].fields) {
      if (field.required && !['name', 'repositoryPath', 'defaultBranch', 'issuePrefix'].includes(field.key)) {
        const control = wrapper.find(`[data-field="${field.key}"] input, [data-field="${field.key}"] select`)
        if (kind === 'providers' && field.key === 'kind') {
          await control.setValue('anthropic')
        } else {
          await control.setValue(field.resource ? 'one' : field.type === 'number' ? '1' : 'value')
        }
      }
    }
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(fetch.mock.calls.some(([, options]) => options.method === (kind === 'projects' ? 'PATCH' : 'PUT'))).toBe(true)
    wrapper.unmount()
  })

  it('shows Runtime configured health and server-owned workspace policy without relying on color', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([{ id: 'r', name: 'Docker', projectId: 'p', enabled: false, healthStatus: 'UNHEALTHY', kind: 'docker', image: 'runner:latest', networkPolicy: 'restricted', workspacePolicy: 'issue', capabilities: { git: true }, cpuLimitMillis: 1000, memoryLimitBytes: 256, pidLimit: 128, timeoutSeconds: 60 }]))))
    const wrapper = mount(ConfigManager, { props: { kind: 'runtimes', projectId: 'p' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Configuration: Disabled')
    expect(wrapper.text()).toContain('Health: Unhealthy')
    expect(wrapper.text()).toContain('Network: Restricted · Workspace: Issue')
    expect(wrapper.text()).toContain('Identity')
    expect(wrapper.text()).toContain('r')
    expect(wrapper.text()).toContain('Kind')
    expect(wrapper.text()).toContain('Capabilities')
    await button(wrapper, 'Edit').trigger('click')
    expect(wrapper.text()).toContain('server-controlled')
  })

  it('shows reference errors, retries, distinguishes scope, and disables shared edits in project scope', async () => {
    let fail = true
    vi.stubGlobal('fetch', vi.fn(async (path: string) => new Response(JSON.stringify(path.includes('executor-profiles') ? [] : [{ id: 'a', name: 'Shared', projectId: null }]), { status: fail && path.includes('executor-profiles') ? 403 : 200 })))
    const wrapper = mount(ConfigManager, { props: { kind: 'agents', projectId: 'p' }, global })
    await flushPromises()
    await button(wrapper, 'View shared').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('permission')
    fail = false
    await button(wrapper, 'Retry').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Manage this resource from global Settings')
    expect(button(wrapper, 'Save')).toBeUndefined()
    expect(wrapper.get('input').attributes('disabled')).toBeDefined()
    await button(wrapper, 'Cancel').trigger('click')
    expect(wrapper.find('[role=dialog]').exists()).toBe(false)
  })

  it('validates drafts and sends provider API keys on the provider request only', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (options.method === 'GET' || !options.method) return new Response('[]')
      return new Response('{"error":{"code":"invalid_argument","message":"do not reflect never-show"}}', { status: 400 })
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'providers' }, global })
    await flushPromises()
    await button(wrapper, 'New provider').trigger('click')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.text()).toContain('Check the highlighted')
    await wrapper.get('[data-field=name] input').setValue('Provider')
    await wrapper.get('[data-field=kind] select').setValue('anthropic')
    await wrapper.get('input[type=password]').setValue('never-show')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Server code: invalid_argument')
    expect(wrapper.text()).not.toContain('do not reflect never-show')
    expect((wrapper.get('input[type=password]').element as HTMLInputElement).value).toBe('')
    const providerCall = fetch.mock.calls.find(([path, options]) => path === '/api/providers' && options.method === 'POST')!
    expect(JSON.parse(String(providerCall[1].body))).toMatchObject({ name: 'Provider', kind: 'anthropic', credential: 'never-show' })
    expect(fetch.mock.calls.some(([path]) => path === '/api/secrets')).toBe(false)
  })

  it('shows labeled provider kinds and a custom provider id field', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[]')))
    const wrapper = mount(ConfigManager, { props: { kind: 'providers' }, global })
    await flushPromises()
    await button(wrapper, 'New provider').trigger('click')
    const kindSelect = wrapper.get('[data-field=kind] select')
    expect(kindSelect.text()).toContain('Anthropic')
    expect(kindSelect.text()).toContain('Custom (OpenAI-compatible)')
    await kindSelect.setValue('anthropic')
    expect(wrapper.find('[data-field=providerKindId]').exists()).toBe(false)
    await kindSelect.setValue(CUSTOM_PROVIDER_KIND)
    expect(wrapper.find('[data-field=providerKindId] input').exists()).toBe(true)
  })

  it('saves built-in and custom provider kinds', async () => {
    const fetch = vi.fn(async (_path: string, options: RequestInit = {}) => {
      if (options.method === 'GET' || !options.method) return new Response('[]')
      return new Response('{"id":"one","name":"Provider","kind":"anthropic","enabled":true}')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'providers' }, global })
    await flushPromises()
    await button(wrapper, 'New provider').trigger('click')
    await wrapper.get('[data-field=name] input').setValue('Built-in provider')
    await wrapper.get('[data-field=kind] select').setValue('anthropic')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const builtInCall = fetch.mock.calls.find(([path, options]) => path === '/api/providers' && options.method === 'POST')!
    expect(JSON.parse(String(builtInCall[1].body))).toMatchObject({ name: 'Built-in provider', kind: 'anthropic' })

    await button(wrapper, 'New provider').trigger('click')
    await wrapper.get('[data-field=name] input').setValue('Custom provider')
    await wrapper.get('[data-field=kind] select').setValue(CUSTOM_PROVIDER_KIND)
    await wrapper.get('[data-field=providerKindId] input').setValue('lmstudio')
    await wrapper.get('[data-field=baseUrl] input').setValue('http://127.0.0.1:1234/v1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const customCall = fetch.mock.calls.filter(([path, options]) => path === '/api/providers' && options.method === 'POST').at(-1)!
    expect(JSON.parse(String(customCall[1].body))).toMatchObject({
      name: 'Custom provider',
      kind: 'lmstudio',
      baseUrl: 'http://127.0.0.1:1234/v1'
    })
  })

  it('hydrates custom provider kind when editing an unknown built-in id', async () => {
    vi.stubGlobal('fetch', vi.fn(async (_path: string, options: RequestInit = {}) => new Response(JSON.stringify(
      options.method === 'GET' || !options.method
        ? [{ id: 'one', name: 'Local', kind: 'lmstudio', enabled: true, safeMetadata: {} }]
        : { id: 'one', name: 'Local', kind: 'lmstudio', enabled: true, safeMetadata: {} }
    ))))
    const wrapper = mount(ConfigManager, { props: { kind: 'providers' }, global })
    await flushPromises()
    await button(wrapper, 'Edit').trigger('click')
    expect((wrapper.get('[data-field=kind] select').element as HTMLSelectElement).value).toBe(CUSTOM_PROVIDER_KIND)
    expect((wrapper.get('[data-field=providerKindId] input').element as HTMLInputElement).value).toBe('lmstudio')
  })

  it('preserves response-backed metadata when editing provider and model profile', async () => {
    const cases: Array<{kind: ConfigKind; record: Record<string, unknown>; expected: Record<string, unknown>}> = [
      { kind: 'providers', record: { id: 'one', name: 'P', kind: 'openai-compatible', enabled: true, safeMetadata: { region: 'eu' } }, expected: { safeMetadata: { region: 'eu' } } },
      { kind: 'model-profiles', record: { id: 'one', name: 'M', providerId: 'provider', model: 'gpt-test', enabled: true, generationSettings: { seed: 7 } }, expected: { generationSettings: { seed: 7 } } }
    ]
    for (const testCase of cases) {
      const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
        if (testCase.kind === 'model-profiles' && path === '/api/providers') return new Response('[{"id":"provider","name":"Provider","enabled":true}]')
        return new Response(JSON.stringify(options.method === 'GET' || !options.method ? [testCase.record] : testCase.record))
      })
      vi.stubGlobal('fetch', fetch)
      const wrapper = mount(ConfigManager, { props: { kind: testCase.kind }, global })
      await flushPromises()
      await button(wrapper, 'Edit').trigger('click')
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      const mutation = fetch.mock.calls.find(([, options]) => options.method === 'PUT')!
      expect(JSON.parse(String(mutation[1].body))).toMatchObject(testCase.expected)
      wrapper.unmount()
      vi.unstubAllGlobals()
    }
  })

  it('rejects saving an existing configuration that references a disabled dependency', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path === '/api/executor-profiles') return new Response(JSON.stringify([{ id: 'e', name: 'Executor', engine: 'opencode', modelProfileId: 'm', runtimeId: 'r', enabled: true, engineSettings: {} }]))
      if (path === '/api/model-profiles') return new Response(JSON.stringify([{ id: 'm', name: 'Disabled model', enabled: false }]))
      if (path === '/api/runtimes') return new Response(JSON.stringify([{ id: 'r', name: 'Runtime', enabled: true }]))
      return new Response('{}', { status: options.method === 'PUT' ? 200 : 404 })
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'executor-profiles' }, global })
    await flushPromises()
    await button(wrapper, 'Edit').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Check the highlighted form values')
    expect(fetch.mock.calls.some(([, options]) => options.method === 'PUT')).toBe(false)
  })

  it('supports project settings selection and empty state', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[{"id":"p","name":"Project","issuePrefix":"AB","repositoryPath":"/repo","defaultBranch":"main"}]')))
    const wrapper = mount(ConfigManager, { props: { kind: 'projects', resourceId: 'p' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Project')
    expect(wrapper.text()).toContain('Backend-managed repository context')
    expect(wrapper.text()).not.toContain('Shared')
    await wrapper.setProps({ resourceId: 'missing' })
    expect(wrapper.text()).toContain('No projects yet')
    expect(wrapper.text()).toContain(definitions.projects.emptyDescription)
    expect(wrapper.text()).not.toContain('New work will appear here when it is created.')
  })

  it.each(Object.keys(definitions) as ConfigKind[])('shows a resource-specific empty state for %s', async kind => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('[]')))
    const wrapper = mount(ConfigManager, { props: { kind }, global })
    await flushPromises()
    expect(wrapper.text()).toContain(`No ${definitions[kind].title.toLowerCase()} yet`)
    expect(wrapper.text()).toContain(definitions[kind].emptyDescription)
    expect(wrapper.text()).not.toContain('New work will appear here when it is created.')
    wrapper.unmount()
  })
})
