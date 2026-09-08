import { describe, expect, it } from 'vitest'
import {
  CUSTOM_PROVIDER_KIND,
  definitions,
  draftFor,
  isBuiltInProviderKind,
  payloadFor,
  providerKindSelectItems,
  providerKindSelectValue,
  validateDraft,
  resourceOptions,
  canEdit
} from '../app/utils/configuration'

describe('intentional configuration inputs', () => {
  it('keeps blank capacity unlimited and omits response-only fields', () => {
    const draft = draftFor('model-profiles', { id: 'm', name: 'Model', providerId: 'p', model: 'test', maxConcurrent: null, healthStatus: 'HEALTHY' })
    expect(draft.maxConcurrent).toBe('')
    expect(payloadFor('model-profiles', draft)).toMatchObject({ maxConcurrent: null })
    expect(payloadFor('model-profiles', draft)).not.toHaveProperty('id')
    expect(payloadFor('runtimes', draftFor('runtimes', { name: 'Runtime', workspacePolicy: 'issue' }))).not.toHaveProperty('workspacePolicy')
  })

  it('uses direct Runtime selection and omits server-owned credential references', () => {
    expect(definitions['executor-profiles'].fields.map(field => field.key)).toContain('runtimeId')
    expect(definitions).not.toHaveProperty('runtime-profiles')
    expect(definitions.providers.fields.map(field => field.key)).not.toContain('credentialRef')
    expect(payloadFor('providers', draftFor('providers'))).not.toHaveProperty('credentialRef')
  })

  it('lists labeled OpenCode provider kinds with a custom option', () => {
    const kindField = definitions.providers.fields.find(field => field.key === 'kind')
    expect(kindField?.type).toBe('select')
    expect(kindField?.allowCustom).toBe(true)
    expect(kindField?.initial).toBeUndefined()
    expect(providerKindSelectItems.some(item => item.label === 'Anthropic' && item.value === 'anthropic')).toBe(true)
    expect(providerKindSelectItems.some(item => item.label === 'OpenRouter' && item.value === 'openrouter')).toBe(true)
    expect(providerKindSelectItems.some(item => item.label.includes('Custom') && item.value === CUSTOM_PROVIDER_KIND)).toBe(true)
    expect(isBuiltInProviderKind('anthropic')).toBe(true)
    expect(isBuiltInProviderKind('lmstudio')).toBe(false)
    expect(providerKindSelectValue('anthropic')).toBe('anthropic')
    expect(providerKindSelectValue('lmstudio')).toBe(CUSTOM_PROVIDER_KIND)
    expect(providerKindSelectValue('')).toBe('')
  })

  it('validates provider kind for built-in and custom OpenCode ids', () => {
    expect(validateDraft('providers', { ...draftFor('providers'), name: 'P', kind: '' }).map(error => error.name)).toContain('kind')
    expect(validateDraft('providers', { ...draftFor('providers'), name: 'P', kind: CUSTOM_PROVIDER_KIND }).map(error => error.name)).toContain('kind')
    expect(validateDraft('providers', { ...draftFor('providers'), name: 'P', kind: 'anthropic' })).toEqual([])
    expect(validateDraft('providers', { ...draftFor('providers'), name: 'P', kind: 'lmstudio' })).toEqual([])
    expect(payloadFor('providers', { ...draftFor('providers'), name: 'P', kind: 'lmstudio' })).toMatchObject({ kind: 'lmstudio' })
  })

  it('preserves public provider and model-profile metadata during edits', () => {
    const provider = draftFor('providers', { name: 'P', kind: 'openai-compatible', safeMetadata: { region: 'eu' } })
    expect(payloadFor('providers', provider).safeMetadata).toEqual({ region: 'eu' })
    const model = draftFor('model-profiles', { name: 'M', providerId: 'p', model: 'gpt-test', generationSettings: { seed: 7 } })
    expect(payloadFor('model-profiles', model).generationSettings).toEqual({ seed: 7 })
  })

  it('requires a valid immutable issue prefix on project create and omits it on edit', () => {
    expect(validateDraft('projects', draftFor('projects')).map(error => error.name)).toContain('issuePrefix')
    const draft = draftFor('projects', { name: 'Workspace', issuePrefix: 'ab', repositoryPath: '/repo', defaultBranch: 'main' })
    expect(validateDraft('projects', draft)).toEqual([])
    expect(payloadFor('projects', draft)).toMatchObject({ issuePrefix: 'AB' })
    expect(payloadFor('projects', draft, { editing: true })).not.toHaveProperty('issuePrefix')
    expect(validateDraft('projects', { ...draft, issuePrefix: '1A' }).map(error => error.name)).toContain('issuePrefix')
  })

  it('validates required, numeric, enum, JSON and array inputs', () => {
    expect(validateDraft('projects', draftFor('projects')).length).toBeGreaterThan(0)
    const draft = { ...draftFor('runtimes'), name: 'Docker', image: 'runner', cpuLimitMillis: 'bad', networkPolicy: 'invalid', capabilities: '[]' }
    expect(validateDraft('runtimes', draft).map(error => error.name)).toEqual(expect.arrayContaining(['cpuLimitMillis', 'networkPolicy', 'capabilities']))
    expect(payloadFor('runtimes', { ...draftFor('runtimes'), allowedSecretRefs: 'one\n two\n' }).allowedSecretRefs).toEqual(['one', 'two'])
    expect(validateDraft('model-profiles', { ...draftFor('model-profiles'), name: 'M', providerId: 'p', model: 'm', temperature: 3, maxTokens: 0 }).length).toBe(2)
    expect(validateDraft('providers', { ...draftFor('providers'), name: 'P', safeMetadata: '[]' }).map(error => error.name)).toContain('safeMetadata')
  })

  it('labels scope and marks directly unrunnable choices unavailable', () => {
    expect(resourceOptions([
      { id: 'a', name: 'A', enabled: false, projectId: null },
      { id: 'b', name: 'B', state: 'DRAFT', projectId: 'p' },
      { id: 'c', name: 'C', projectId: 'p' },
      { id: 'd', name: 'D', healthStatus: 'UNHEALTHY', projectId: null },
      { id: 'e', name: 'E', healthStatus: 'UNKNOWN', projectId: null }
    ])).toEqual([
      { label: 'A · Shared · Disabled', value: 'a', disabled: true },
      { label: 'B · Project · DRAFT', value: 'b', disabled: true },
      { label: 'C · Project', value: 'c', disabled: false },
      { label: 'D · Shared · Unhealthy', value: 'd', disabled: true },
      { label: 'E · Shared', value: 'e', disabled: false }
    ])
    expect(canEdit({ id: 'a', name: 'A', projectId: null }, 'p')).toBe(false)
    expect(canEdit({ id: 'a', name: 'A', projectId: 'p' }, 'p')).toBe(true)
    expect(canEdit({ id: 'a', name: 'A' })).toBe(true)
  })

  it('describes each empty configuration list in product terms', () => {
    expect(definitions.projects.emptyDescription).toBe('Create a Project with a local repository, default branch, and Issue prefix to open a board.')
    expect(definitions.providers.emptyDescription).toBe('Add a Provider with encrypted credentials so Model Profiles can call a model API.')
    expect(definitions['model-profiles'].emptyDescription).toBe('Create a Model Profile to select a Provider, model, and optional concurrent Run capacity.')
    expect(definitions.runtimes.emptyDescription).toBe('Define a Runtime image and execution policy so Executor Profiles can start agent-runner.')
    expect(definitions['executor-profiles'].emptyDescription).toBe('Combine an Engine, Model Profile, and Runtime into an Executor Profile that Agents can use.')
    expect(definitions.agents.emptyDescription).toBe('Create an Agent with role instructions and an Executor Profile before assigning Issues.')
    for (const kind of Object.keys(definitions) as (keyof typeof definitions)[]) {
      expect(definitions[kind].emptyDescription).not.toContain('New work will appear here')
    }
  })

  it('roundtrips supported fields for every resource without mutating source', () => {
    for (const kind of Object.keys(definitions) as (keyof typeof definitions)[]) {
      const initial = draftFor(kind)
      const payload = payloadFor(kind, initial)
      expect(payload).toHaveProperty('name')
      expect(definitions[kind].title).toBeTruthy()
    }
    expect(draftFor('runtimes', { allowedSecretRefs: ['a', 'b'], capabilities: { git: true } })).toMatchObject({ allowedSecretRefs: 'a\nb', capabilities: '{\n  "git": true\n}' })
    expect(validateDraft('runtimes', { ...draftFor('runtimes'), capabilities: '{' })[0]?.name).toBeTruthy()
  })
})
