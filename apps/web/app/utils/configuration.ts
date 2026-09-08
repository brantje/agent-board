/** Form inputs mirror packages/api/schemas/control-plane.yaml; response metadata is never submitted. */
export interface ConfigRecord {
  id: string
  name: string
  projectId?: string | null
  enabled?: boolean
  state?: string
  healthStatus?: string
  workspacePolicy?: string
  [key: string]: unknown
}

export interface Field {
  key: string
  label: string
  type?: 'number'|'select'|'textarea'|'checkbox'|'json'|'lines'
  required?: boolean
  options?: string[]
  resource?: ConfigKind
  initial?: string|boolean|number
  min?: number
  max?: number
  help?: string
}

interface Definition {
  title: string
  singular: string
  fields: Field[]
}

const name: Field = { key: 'name', label: 'Name', required: true }
const enabled: Field = { key: 'enabled', label: 'Enabled', type: 'checkbox', initial: true }
const reference = (key: string, label: string, resource: ConfigKind): Field => ({ key, label, resource, type: 'select', required: true })

export const definitions: Record<ConfigKind, Definition> = {
  projects: {
    title: 'Projects',
    singular: 'Project',
    fields: [
      name,
      { key: 'repositoryPath', label: 'Local repository path', required: true, help: 'Path visible to the backend within deployment-authorized repository roots.' },
      { key: 'defaultBranch', label: 'Default branch', initial: 'main', required: true },
      { key: 'workflowSettings', label: 'Workflow settings', type: 'json', initial: '{}', help: 'Optional workflow policy overrides supported by your Go server.' }
    ]
  },
  providers: {
    title: 'Providers',
    singular: 'Provider',
    fields: [
      name,
      { key: 'kind', label: 'Provider kind', initial: 'openai-compatible', required: true },
      { key: 'baseUrl', label: 'Base URL' },
      { key: 'credentialRef', label: 'Credential reference', help: 'Enter the secret reference to attach. Leave blank when editing to preserve the saved credential. Saved references and plaintext values are never returned.' },
      { key: 'safeMetadata', label: 'Safe metadata', type: 'json', initial: '{}', help: 'Non-secret provider metadata exposed by the public API.' },
      enabled
    ]
  },
  'model-profiles': {
    title: 'Model Profiles',
    singular: 'Model Profile',
    fields: [
      name,
      reference('providerId', 'Provider', 'providers'),
      { key: 'model', label: 'Model', required: true },
      { key: 'temperature', label: 'Temperature', type: 'number', min: 0, max: 2 },
      { key: 'maxTokens', label: 'Max tokens', type: 'number', min: 1 },
      { key: 'maxConcurrent', label: 'Capacity', type: 'number', min: 1, help: 'Leave empty for unlimited concurrent Runs.' },
      { key: 'generationSettings', label: 'Generation settings', type: 'json', initial: '{}', help: 'Additional non-secret generation settings supported by the provider/API.' },
      enabled
    ]
  },
  runtimes: {
    title: 'Runtimes',
    singular: 'Runtime',
    fields: [
      name,
      { key: 'kind', label: 'Kind', type: 'select', options: ['docker'], initial: 'docker' },
      { key: 'image', label: 'Image', initial: 'agent-board-agent-runner:latest', required: true },
      { key: 'networkPolicy', label: 'Network policy', type: 'select', options: ['none', 'restricted', 'outbound'], initial: 'none' },
      { key: 'cpuLimitMillis', label: 'CPU limit (millicores)', type: 'number', min: 1 },
      { key: 'memoryLimitBytes', label: 'Memory limit (bytes)', type: 'number', min: 1 },
      { key: 'pidLimit', label: 'PID limit', type: 'number', min: 1 },
      { key: 'timeoutSeconds', label: 'Timeout (seconds)', type: 'number', min: 1 },
      { key: 'allowedSecretRefs', label: 'Allowed secret references', type: 'lines', help: 'One reference per line.' },
      { key: 'capabilities', label: 'Capabilities', type: 'json', initial: '{}' },
      enabled
    ]
  },
  'executor-profiles': {
    title: 'Executor Profiles',
    singular: 'Executor Profile',
    fields: [
      name,
      { key: 'engine', label: 'Engine', type: 'select', options: ['opencode', 'scripted'], initial: 'opencode' },
      reference('modelProfileId', 'Model Profile', 'model-profiles'),
      reference('runtimeId', 'Runtime', 'runtimes'),
      { key: 'engineSettings', label: 'Engine settings', type: 'json', initial: '{}' },
      enabled
    ]
  },
  agents: {
    title: 'Agents',
    singular: 'Agent',
    fields: [
      name,
      { key: 'roleInstructions', label: 'Role / instructions', type: 'textarea' },
      reference('executorProfileId', 'Executor Profile', 'executor-profiles'),
      { key: 'concurrencyLimit', label: 'Concurrency limit', type: 'number', initial: 1, min: 1, required: true },
      { key: 'state', label: 'State', type: 'select', options: ['DRAFT', 'ENABLED', 'DISABLED', 'ARCHIVED'], initial: 'ENABLED' }
    ]
  }
}

export type ConfigKind = 'projects'|'providers'|'model-profiles'|'runtimes'|'executor-profiles'|'agents'
export type Draft = Record<string, string|number|boolean>

export function draftFor(kind: ConfigKind, source: Record<string, unknown> = {}): Draft {
  return Object.fromEntries(definitions[kind].fields.map(field => {
    const value = field.key === 'credentialRef' ? undefined : source[field.key]
    return [field.key, value == null
      ? field.initial ?? ''
      : field.type === 'json'
        ? JSON.stringify(value, null, 2)
        : field.type === 'lines'
          ? (value as string[]).join('\n')
          : value]
  })) as Draft
}

export function payloadFor(kind: ConfigKind, draft: Draft): Record<string, unknown> {
  return Object.fromEntries(definitions[kind].fields
    .filter(field => field.key !== 'credentialRef' || draft[field.key])
    .map(field => {
      const value = draft[field.key]
      return [field.key,
        field.type === 'number'
          ? value === '' ? null : Number(value)
          : field.type === 'json'
            ? JSON.parse(String(value))
            : field.type === 'lines'
              ? String(value).split('\n').map(item => item.trim()).filter(Boolean)
              : typeof value === 'string' ? value.trim() : value]
    }))
}

export function validateDraft(kind: ConfigKind, draft: Draft) {
  return definitions[kind].fields.flatMap(field => {
    const value = draft[field.key]
    let invalid = field.required && !String(value ?? '').trim()
    if (field.type === 'number' && value !== '') {
      invalid ||= !Number.isFinite(Number(value))
        || (field.min !== undefined && Number(value) < field.min)
        || (field.max !== undefined && Number(value) > field.max)
        || (field.key !== 'temperature' && !Number.isInteger(Number(value)))
    }
    if (field.options) invalid ||= !field.options.includes(String(value))
    if (field.type === 'json') {
      try {
        const parsed = JSON.parse(String(value))
        invalid ||= parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)
      } catch {
        invalid = true
      }
    }
    return invalid ? [{ name: field.key, message: `Enter a valid ${field.label.toLowerCase()}.` }] : []
  })
}

export function resourceOptions(items: ConfigRecord[]) {
  return items.map(item => {
    const disabled = item.enabled === false || (item.state !== undefined && item.state !== 'ENABLED')
    const scope = item.projectId ? 'Project' : 'Shared'
    const state = item.enabled === false ? 'Disabled' : item.state && item.state !== 'ENABLED' ? item.state : undefined
    return {
      label: `${item.name} · ${scope}${state ? ` · ${state}` : ''}`,
      value: item.id,
      disabled
    }
  })
}

export function canEdit(item: ConfigRecord, projectId?: string) {
  return !projectId || item.projectId === projectId
}
