import { describe, expect, it } from 'vitest'
import { definitions, draftFor, payloadFor, validateDraft, resourceOptions, canEdit } from '../app/utils/configuration'

describe('intentional configuration inputs', () => {
  it('keeps blank capacity unlimited and omits response-only fields', () => {
    const draft = draftFor('model-profiles', { id: 'm', name: 'Model', providerId: 'p', model: 'test', maxConcurrent: null, healthStatus: 'HEALTHY' })
    expect(draft.maxConcurrent).toBe('')
    expect(payloadFor('model-profiles', draft)).toMatchObject({ maxConcurrent: null })
    expect(payloadFor('model-profiles', draft)).not.toHaveProperty('id')
    expect(payloadFor('runtimes', draftFor('runtimes', { name: 'Runtime', workspacePolicy: 'issue' }))).not.toHaveProperty('workspacePolicy')
  })

  it('uses direct Runtime selection and safe credentials', () => {
    expect(definitions['executor-profiles'].fields.map(field => field.key)).toContain('runtimeId')
    expect(definitions).not.toHaveProperty('runtime-profiles')
    expect(draftFor('providers', { credentialRef: 'hidden', name: 'P' }).credentialRef).toBe('')
    expect(payloadFor('providers', draftFor('providers'))).not.toHaveProperty('credentialRef')
    expect(payloadFor('providers', { ...draftFor('providers'), credentialRef: 'key' }).credentialRef).toBe('key')
  })

  it('preserves public provider and model-profile metadata during edits', () => {
    const provider = draftFor('providers', { name: 'P', kind: 'openai-compatible', safeMetadata: { region: 'eu' } })
    expect(payloadFor('providers', provider).safeMetadata).toEqual({ region: 'eu' })
    const model = draftFor('model-profiles', { name: 'M', providerId: 'p', model: 'gpt-test', generationSettings: { seed: 7 } })
    expect(payloadFor('model-profiles', model).generationSettings).toEqual({ seed: 7 })
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
