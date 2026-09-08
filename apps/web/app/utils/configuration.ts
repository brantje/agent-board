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
  type?: 'number'|'select'|'textarea'|'checkbox'|'json'|'lines'|'provider-model'
  required?: boolean
  options?: string[]
  selectItems?: Array<{ label: string; value: string }>
  allowCustom?: boolean
  resource?: ConfigKind
  initial?: string|boolean|number
  min?: number
  max?: number
  help?: string
  immutable?: boolean
}

interface Definition {
  title: string
  singular: string
  emptyDescription: string
  fields: Field[]
}

const name: Field = { key: 'name', label: 'Name', required: true }
const enabled: Field = { key: 'enabled', label: 'Enabled', type: 'checkbox', initial: true }
const reference = (key: string, label: string, resource: ConfigKind): Field => ({ key, label, resource, type: 'select', required: true })

export const CUSTOM_PROVIDER_KIND = '__custom__'

const builtInOpenCodeProviderLabels: Record<string, string> = {
  'amazon-bedrock': 'Amazon Bedrock',
  anthropic: 'Anthropic',
  azure: 'Azure OpenAI',
  cerebras: 'Cerebras',
  deepseek: 'DeepSeek',
  fireworks: 'Fireworks AI',
  'github-copilot': 'GitHub Copilot',
  gitlab: 'GitLab',
  google: 'Google',
  'google-vertex': 'Google Vertex AI',
  groq: 'Groq',
  mistral: 'Mistral',
  openai: 'OpenAI',
  openrouter: 'OpenRouter',
  opencode: 'OpenCode Zen',
  perplexity: 'Perplexity',
  together: 'Together AI',
  xai: 'xAI'
}

export const providerKindSelectItems = [
  ...Object.entries(builtInOpenCodeProviderLabels).map(([value, label]) => ({ label, value })),
  { label: 'Custom (OpenAI-compatible)', value: CUSTOM_PROVIDER_KIND }
]

export function isBuiltInProviderKind(kind: string) {
  return Object.prototype.hasOwnProperty.call(builtInOpenCodeProviderLabels, kind.trim())
}

export function providerKindSelectValue(kind: string) {
  const trimmed = String(kind ?? '').trim()
  if (!trimmed) return ''
  return isBuiltInProviderKind(trimmed) ? trimmed : CUSTOM_PROVIDER_KIND
}

export const definitions: Record<ConfigKind, Definition> = {
  projects: {
    title: 'Projects',
    singular: 'Project',
    emptyDescription: 'Create a Project with a local repository, default branch, and Issue prefix to open a board.',
    fields: [
      name,
      { key: 'issuePrefix', label: 'Issue prefix', required: true, immutable: true, help: 'Immutable public prefix for Issue keys such as AB-12. Use 2-10 uppercase letters and digits; must start with a letter.' },
      { key: 'repositoryPath', label: 'Local repository path', required: true, help: 'Backend-visible path within deployment-authorized repository roots. If the directory does not exist, Agent Board creates it and initializes a new Git repository on save.' },
      { key: 'defaultBranch', label: 'Default branch', initial: 'main', required: true },
      { key: 'workflowSettings', label: 'Workflow settings', type: 'json', initial: '{}', help: 'Optional workflow policy overrides supported by your Go server.' }
    ]
  },
  providers: {
    title: 'Providers',
    singular: 'Provider',
    emptyDescription: 'Add a Provider with encrypted credentials so Model Profiles can call a model API.',
    fields: [
      name,
      {
        key: 'kind',
        label: 'Provider kind',
        type: 'select',
        required: true,
        allowCustom: true,
        selectItems: providerKindSelectItems,
        help: 'OpenCode provider used when an Executor Profile runs with the OpenCode engine.'
      },
      {
        key: 'baseUrl',
        label: 'Base URL',
        help: 'Optional for built-in providers with default endpoints. Usually required for custom OpenAI-compatible endpoints.'
      },
      { key: 'safeMetadata', label: 'Safe metadata', type: 'json', initial: '{}', help: 'Non-secret provider metadata exposed by the public API.' },
      enabled
    ]
  },
  'model-profiles': {
    title: 'Model Profiles',
    singular: 'Model Profile',
    emptyDescription: 'Create a Model Profile to select a Provider, model, and optional concurrent Run capacity.',
    fields: [
      name,
      reference('providerId', 'Provider', 'providers'),
      { key: 'model', label: 'Model', type: 'provider-model', required: true, help: 'Models load from the Provider API when available. Enter a model ID manually when discovery is unavailable.' },
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
    emptyDescription: 'Define a Runtime image and execution policy so Executor Profiles can start agent-runner.',
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
    emptyDescription: 'Combine an Engine, Model Profile, and Runtime into an Executor Profile that Agents can use.',
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
    emptyDescription: 'Create an Agent with role instructions and an Executor Profile before assigning Issues.',
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
    const value = source[field.key]
    return [field.key, value == null
      ? field.initial ?? ''
      : field.type === 'json'
        ? JSON.stringify(value, null, 2)
        : field.type === 'lines'
          ? (value as string[]).join('\n')
          : value]
  })) as Draft
}

export function payloadFor(kind: ConfigKind, draft: Draft, options?: { editing?: boolean }): Record<string, unknown> {
  return Object.fromEntries(definitions[kind].fields
    .filter(field => !(options?.editing && field.immutable))
    .map(field => {
      const value = draft[field.key]
      return [field.key,
        field.type === 'number'
          ? value === '' ? null : Number(value)
          : field.type === 'json'
            ? JSON.parse(String(value))
            : field.type === 'lines'
              ? String(value).split('\n').map(item => item.trim()).filter(Boolean)
              : field.key === 'issuePrefix'
                ? String(value).trim().toUpperCase()
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
    if (field.key === 'issuePrefix') {
      invalid ||= !/^[A-Za-z][A-Za-z0-9]{1,9}$/.test(String(value).trim())
    }
    if (field.allowCustom) {
      invalid ||= !String(value ?? '').trim() || String(value).trim() === CUSTOM_PROVIDER_KIND
    } else if (field.options) {
      invalid ||= !field.options.includes(String(value))
    }
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
    const unavailableReasons = [
      item.enabled === false ? 'Disabled' : undefined,
      item.state !== undefined && item.state !== 'ENABLED' ? item.state : undefined,
      item.healthStatus === 'UNHEALTHY' ? 'Unhealthy' : undefined
    ].filter((reason): reason is string => Boolean(reason))
    const scope = item.projectId ? 'Project' : 'Shared'
    return {
      label: `${item.name} · ${scope}${unavailableReasons.length ? ` · ${unavailableReasons.join(' · ')}` : ''}`,
      value: item.id,
      disabled: unavailableReasons.length > 0
    }
  })
}

export function canEdit(item: ConfigRecord, projectId?: string) {
  return !projectId || item.projectId === projectId
}
