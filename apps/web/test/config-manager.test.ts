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
      safeMetadata: {},
      generationSettings: {}
    }]
    const fetch = vi.fn(async (path: string, options: RequestInit) => {
      if (path.includes('/models')) return new Response(JSON.stringify({ models: [{ id: 'discovered-model' }] }))
      if (kind === 'model-profiles' && path === '/api/providers') {
        return new Response(JSON.stringify([{ id: 'one', name: 'Provider', enabled: true }]))
      }
      return new Response(JSON.stringify(options.method === 'GET' ? records : records[0]))
    })
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
        } else if (field.type === 'provider-model') {
          if (kind === 'model-profiles') {
            await wrapper.get('[data-field=providerId] select').setValue('one')
            await flushPromises()
          }
          const modelControl = wrapper.find('[data-field=model] input, [data-field=model] select')
          await modelControl.setValue('discovered-model')
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
        } else if (field.type === 'provider-model') {
          if (kind === 'model-profiles') {
            await wrapper.get('[data-field=providerId] select').setValue('one')
            await flushPromises()
          }
          const modelControl = wrapper.find('[data-field=model] input, [data-field=model] select')
          await modelControl.setValue('discovered-model')
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

  it('shows reference errors, retries, distinguishes scope, and disables shared edits in project scope', async () => {
    let fail = true
    vi.stubGlobal('fetch', vi.fn(async (path: string) => new Response(JSON.stringify(path.includes('model-profiles') ? [] : [{ id: 'a', name: 'Shared', projectId: null }]), { status: fail && path.includes('model-profiles') ? 403 : 200 })))
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

  it('creates project-owned providers and discovers models through project-scoped APIs', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path === '/api/projects/p/providers' && (!options.method || options.method === 'GET')) {
        return new Response('[{"id":"provider","name":"Project provider","enabled":true,"projectId":"p"}]')
      }
      if (path === '/api/projects/p/providers' && options.method === 'POST') {
        return new Response('{"id":"created","name":"Owned","kind":"anthropic","enabled":true,"projectId":"p"}')
      }
      if (path === '/api/projects/p/model-profiles') {
        return new Response(options.method === 'POST' ? '{"id":"profile"}' : '[]')
      }
      if (path === '/api/projects/p/providers/provider/models') {
        return new Response('{"models":[{"id":"model-a","name":"Model A"}]}')
      }
      return new Response('[]', { status: 404 })
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'providers', projectId: 'p' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Project configuration')
    expect(wrapper.text()).not.toContain('Global provider configuration')
    await button(wrapper, 'New provider').trigger('click')
    await wrapper.get('[data-field=name] input').setValue('Owned')
    await wrapper.get('[data-field=kind] select').setValue('anthropic')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const createCall = fetch.mock.calls.find(([path, options]) => path === '/api/projects/p/providers' && options.method === 'POST')!
    expect(JSON.parse(String(createCall[1].body))).toMatchObject({ name: 'Owned', kind: 'anthropic' })
    wrapper.unmount()

    const profile = mount(ConfigManager, { props: { kind: 'model-profiles', projectId: 'p' }, global })
    await flushPromises()
    await button(profile, 'New model profile').trigger('click')
    await profile.get('[data-field=providerId] select').setValue('provider')
    await flushPromises()
    expect(fetch.mock.calls.some(([calledPath]) => calledPath === '/api/projects/p/providers/provider/models')).toBe(true)
    await profile.get('[data-field=model] select').setValue('model-a')
    await profile.get('[data-field=name] input').setValue('Profile')
    await profile.get('form').trigger('submit')
    await flushPromises()
    const profileCreate = fetch.mock.calls.find(([calledPath, options]) => calledPath === '/api/projects/p/model-profiles' && options.method === 'POST')!
    expect(JSON.parse(String(profileCreate[1].body))).toMatchObject({ providerId: 'provider', model: 'model-a' })
    profile.unmount()
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
        if (testCase.kind === 'model-profiles' && path.includes('/models')) return new Response('{"models":[{"id":"gpt-test"}]}')
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

  it('loads provider models when selecting a provider for model profiles', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/model-profiles') return new Response('[]')
      if (path === '/api/providers') return new Response('[{"id":"provider","name":"Provider","enabled":true}]')
      if (path === '/api/providers/provider/models') return new Response('{"models":[{"id":"model-a","name":"Model A"},{"id":"model-b"}]}')
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'model-profiles' }, global })
    await flushPromises()
    await button(wrapper, 'New model profile').trigger('click')
    await wrapper.get('[data-field=providerId] select').setValue('provider')
    await flushPromises()
    expect(fetch.mock.calls.some(([calledPath]) => calledPath === '/api/providers/provider/models')).toBe(true)
    const modelSelect = wrapper.get('[data-field=model] select')
    expect(modelSelect.text()).toContain('Model A (model-a)')
    expect(modelSelect.text()).toContain('model-b')
    await modelSelect.setValue('model-a')
    await wrapper.get('[data-field=name] input').setValue('Profile')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const createCall = fetch.mock.calls.find(([calledPath, options]) => calledPath === '/api/model-profiles' && options.method === 'POST')!
    expect(JSON.parse(String(createCall[1].body))).toMatchObject({ providerId: 'provider', model: 'model-a', name: 'Profile' })
  })

  it('clears model and refetches when provider changes', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/model-profiles') return new Response('[]')
      if (path === '/api/providers') return new Response('[{"id":"provider-a","name":"A","enabled":true},{"id":"provider-b","name":"B","enabled":true}]')
      if (path === '/api/providers/provider-a/models') return new Response('{"models":[{"id":"model-a"}]}')
      if (path === '/api/providers/provider-b/models') return new Response('{"models":[{"id":"model-b"}]}')
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'model-profiles' }, global })
    await flushPromises()
    await button(wrapper, 'New model profile').trigger('click')
    await wrapper.get('[data-field=providerId] select').setValue('provider-a')
    await flushPromises()
    await wrapper.get('[data-field=model] select').setValue('model-a')
    await wrapper.get('[data-field=providerId] select').setValue('provider-b')
    await flushPromises()
    expect((wrapper.get('[data-field=model] select').element as HTMLSelectElement).value).toBe('')
    expect(fetch.mock.calls.filter(([calledPath]) => calledPath.endsWith('/models')).length).toBe(2)
  })

  it('falls back to manual model entry when discovery fails', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/model-profiles') return new Response('[]')
      if (path === '/api/providers') return new Response('[{"id":"provider","name":"Provider","enabled":true}]')
      if (path.includes('/models')) return new Response('{"error":{"code":"provider_model_discovery_failed","message":"Unable to discover models from the Provider API."}}', { status: 502 })
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'model-profiles' }, global })
    await flushPromises()
    await button(wrapper, 'New model profile').trigger('click')
    await wrapper.get('[data-field=providerId] select').setValue('provider')
    await flushPromises()
    expect(wrapper.text()).toContain('Unable to load models')
    const modelInput = wrapper.get('[data-field=model] input')
    await modelInput.setValue('custom-model')
    await wrapper.get('[data-field=name] input').setValue('Profile')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const createCall = fetch.mock.calls.find(([calledPath, options]) => calledPath === '/api/model-profiles' && options.method === 'POST')!
    expect(JSON.parse(String(createCall[1].body))).toMatchObject({ model: 'custom-model' })
  })

  it('preserves an existing model during edit when it is missing from discovery', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path === '/api/providers') return new Response('[{"id":"provider","name":"Provider","enabled":true}]')
      if (path.includes('/models')) return new Response('{"models":[{"id":"listed-model"}]}')
      if (options.method === 'GET' || !options.method) {
        return new Response('[{"id":"one","name":"M","providerId":"provider","model":"legacy-model","enabled":true,"generationSettings":{}}]')
      }
      return new Response('{"id":"one","name":"M","providerId":"provider","model":"legacy-model","enabled":true,"generationSettings":{}}')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'model-profiles' }, global })
    await flushPromises()
    await button(wrapper, 'Edit').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-field=model] select').text()).toContain('legacy-model')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const updateCall = fetch.mock.calls.find(([calledPath, options]) => options.method === 'PUT')!
    expect(JSON.parse(String(updateCall[1].body))).toMatchObject({ model: 'legacy-model' })
  })

  it('rejects saving an existing configuration that references a disabled dependency', async () => {
    const fetch = vi.fn(async (path: string, options: RequestInit = {}) => {
      if (path === '/api/agents') return new Response(JSON.stringify([{ id: 'a', name: 'Coder', engine: 'opencode', modelProfileId: 'm', engineSettings: {}, state: 'ENABLED' }]))
      if (path === '/api/model-profiles') return new Response(JSON.stringify([{ id: 'm', name: 'Disabled model', enabled: false }]))
      return new Response('{}', { status: options.method === 'PUT' ? 200 : 404 })
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'agents' }, global })
    await flushPromises()
    await button(wrapper, 'Edit').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Check the highlighted form values')
    expect(fetch.mock.calls.some(([, options]) => options.method === 'PUT')).toBe(false)
  })

  it('prefills project repository path from deployment settings for new projects', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/repository-settings') {
        return new Response(JSON.stringify({ defaultRepositoryPath: '/repositories', repositoryRoots: ['/repositories'] }))
      }
      return new Response('[]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'projects' }, global })
    await flushPromises()
    await button(wrapper, 'New project').trigger('click')
    await flushPromises()
    expect((wrapper.get('[data-field=repositoryPath] input').element as HTMLInputElement).value).toBe('/repositories')
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

  it('shows semantic provider health and persisted model counts', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path === '/api/providers/healthy/models') {
        return new Response('{"models":[{"id":"a"},{"id":"b"},{"id":"c"}],"total":3}')
      }
      if (path === '/api/providers/unhealthy/models') {
        return new Response('{"error":{"code":"provider_model_discovery_failed","message":"Unable to discover models from the Provider API."}}', { status: 502 })
      }
      return new Response(JSON.stringify([
        { id: 'healthy', name: 'Healthy', enabled: true, healthStatus: 'HEALTHY', filteredModelCount: 3, totalModelCount: 3, safeMetadata: {} },
        { id: 'unhealthy', name: 'Unhealthy', enabled: true, healthStatus: 'UNHEALTHY', safeMetadata: {} }
      ]))
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'providers' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Health: Healthy')
    expect(wrapper.text()).toContain('Health: Unhealthy')
    expect(wrapper.text()).toContain('Models: 3 / 3')
    expect(wrapper.text()).not.toContain('Models: 0 / 0')
    const badges = wrapper.findAll('[data-color]')
    expect(badges.some(badge => badge.attributes('data-color') === 'success' && badge.text().includes('Health: Healthy'))).toBe(true)
    expect(badges.some(badge => badge.attributes('data-color') === 'error' && badge.text().includes('Health: Unhealthy'))).toBe(true)
    expect(fetch.mock.calls.some(([calledPath]) => String(calledPath).includes('/models'))).toBe(true)
  })

  it('overlays live provider health and model counts after discovery', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path === '/api/providers/p1/models') {
        return new Response('{"models":[{"id":"a"},{"id":"b"}],"total":2}')
      }
      if (path.includes('/models')) return new Response('{"models":[],"total":0}')
      return new Response(JSON.stringify([{ id: 'p1', name: 'OpenRouter', enabled: true, healthStatus: 'UNKNOWN', safeMetadata: {} }]))
    }))
    const wrapper = mount(ConfigManager, { props: { kind: 'providers' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Health: Healthy')
    expect(wrapper.text()).toContain('Models: 2 / 2')
    const badges = wrapper.findAll('[data-color]')
    expect(badges.some(badge => badge.attributes('data-color') === 'success')).toBe(true)
  })

  it('marks provider discovery failures as unhealthy without a models badge', async () => {
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.includes('/models')) {
        return new Response('{"error":{"code":"provider_model_discovery_failed","message":"Unable to discover models from the Provider API."}}', { status: 502 })
      }
      return new Response(JSON.stringify([{ id: 'p1', name: 'Broken', enabled: true, healthStatus: 'UNKNOWN', safeMetadata: {} }]))
    }))
    const wrapper = mount(ConfigManager, { props: { kind: 'providers' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Health: Unhealthy')
    expect(wrapper.text()).not.toContain('Models:')
    expect(wrapper.findAll('[data-color]').some(badge => badge.attributes('data-color') === 'error')).toBe(true)
  })

  it('does not probe models outside the providers list', async () => {
    const fetch = vi.fn(async (path: string) => {
      if (path.includes('/models')) throw new Error('unexpected models fetch')
      if (path === '/api/model-profiles') return new Response('[{"id":"m","name":"Model","enabled":true}]')
      return new Response('[{"id":"a","name":"Agent","enabled":true,"state":"ENABLED","engine":"opencode","modelProfileId":"m","engineSettings":{},"concurrencyLimit":1}]')
    })
    vi.stubGlobal('fetch', fetch)
    const wrapper = mount(ConfigManager, { props: { kind: 'agents', projectId: 'p' }, global })
    await flushPromises()
    expect(wrapper.text()).toContain('Agent')
    expect(fetch.mock.calls.some(([calledPath]) => String(calledPath).includes('/models'))).toBe(false)
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