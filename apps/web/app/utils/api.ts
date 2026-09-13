import { readAuthStorage } from './auth-storage'

export type ProviderModel = { id: string; name?: string | null }

export function providerModelsPath(providerId: string, projectId?: string) {
  return `${apiPath('providers', projectId, providerId)}/models`
}

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

const statusMessages: Record<number, string> = {
  400: 'Check the form values, resource eligibility and repository configuration.',
  401: 'Sign in through your deployment to continue.',
  403: 'You do not have permission for this action.',
  404: 'This resource is unavailable or belongs to another project.',
  409: 'The state has changed or this action conflicts with workflow policy. Refresh and try again.',
  422: 'Check the form values and selected execution configuration, then try again.'
}

const codeMessages: Record<string, string> = {
  invalid_request: 'The server rejected the request format. Refresh and try again.',
  invalid_id: 'A selected resource identifier is invalid. Refresh and choose the resource again.',
  invalid_argument: 'The server rejected one or more values. Check required fields, limits, references and policy settings.',
  conflict: 'The resource conflicts with existing state. Refresh and try again.',
  project_not_found: 'This Project is unavailable or belongs to another project scope.',
  execution_configuration_invalid: 'The selected execution configuration is not runnable. Check the Agent, Model Profile and Provider.',
  agent_unavailable: 'The selected Agent is not currently runnable. Check its state and referenced configuration.',
  issue_done: 'Done Issues cannot start Runs. Reopen the Issue into Todo before assigning an Agent.',
  issue_relationship_target_not_found: 'The target Issue no longer exists in this Project. Refresh and choose another Issue.',
  issue_relationship_self_reference: 'An Issue cannot be related to itself.',
  issue_relationship_exists: 'That Issue relationship already exists. Refresh the relationship list.',
  issue_relationship_not_found: 'That Issue relationship no longer exists. Refresh the relationship list.'
}

type ErrorEnvelope = {
  error?: {
    code?: unknown
  }
}

function safeErrorCode(value: unknown) {
  return typeof value === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(value) ? value : 'request_failed'
}

function messageFor(status: number, code: string) {
  const codeMessage = codeMessages[code]
  if (codeMessage) return codeMessage
  if (code.endsWith('_not_found')) {
    return 'A referenced resource no longer exists or is unavailable in this scope. Refresh and choose another resource.'
  }
  return statusMessages[status] ?? 'The server could not complete the request. Retry or check deployment logs.'
}

async function errorCodeFor(response: Response) {
  try {
    const body = await response.json() as ErrorEnvelope
    return safeErrorCode(body.error?.code)
  } catch {
    return 'request_failed'
  }
}

export function apiPath(resource: string, projectId?: string, id?: string) {
  const segment = (value: string) => {
    if (!/^[a-zA-Z0-9_-]+$/.test(value)) throw new Error('Invalid API path segment')
    return value
  }
  return `/api/${projectId ? `projects/${segment(projectId)}/` : ''}${segment(resource)}${id ? `/${segment(id)}` : ''}`
}

function assertApiPath(path: string) {
  if (!path.startsWith('/api/') || path.includes('..') || path.includes('\\')) throw new Error('Invalid API path')
}

export function browserAuthorizationHeaders(): Record<string, string> {
  if (typeof window === 'undefined') return {}
  const { credentials } = readAuthStorage(window.localStorage, window.sessionStorage)
  return credentials?.accessToken ? { Authorization: `Bearer ${credentials.accessToken}` } : {}
}

export function apiQuery(path: string, params: Record<string, string | undefined | null> = {}) {
  assertApiPath(path)
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value) search.set(key, value)
  }
  const query = search.toString()
  return query ? `${path}?${query}` : path
}

async function parseFailedResponse(response: Response) {
  const code = await errorCodeFor(response)
  throw new ApiError(response.status, code, messageFor(response.status, code))
}

export async function apiRequest<T>(path: string, options: { method?: string; body?: unknown; signal?: AbortSignal; headers?: Record<string, string> } = {}): Promise<T> {
  assertApiPath(path)
  let response: Response
  try {
    response = await fetch(path, {
      method: options.method ?? 'GET',
      credentials: 'same-origin',
      signal: options.signal,
      headers: {
        Accept: 'application/json',
        ...(options.body !== undefined ? { 'Content-Type': 'application/json' } : {}),
        ...browserAuthorizationHeaders(),
        ...options.headers
      },
      body: options.body === undefined ? undefined : JSON.stringify(options.body)
    })
  } catch {
    throw new ApiError(0, 'network', 'Unable to reach the server. Check your connection and retry.')
  }
  if (!response.ok) await parseFailedResponse(response)
  if (response.status === 204) return undefined as T
  try {
    return await response.json() as T
  } catch {
    throw new ApiError(502, 'invalid_response', 'The server returned an invalid response. Retry or check deployment configuration.')
  }
}

export async function apiText(path: string, options: { signal?: AbortSignal } = {}): Promise<string> {
  assertApiPath(path)
  let response: Response
  try {
    response = await fetch(path, {
      method: 'GET',
      credentials: 'same-origin',
      signal: options.signal,
      headers: {
        Accept: 'text/plain',
        ...browserAuthorizationHeaders()
      }
    })
  } catch {
    throw new ApiError(0, 'network', 'Unable to reach the server. Check your connection and retry.')
  }
  if (!response.ok) await parseFailedResponse(response)
  try {
    return await response.text()
  } catch {
    throw new ApiError(502, 'invalid_response', 'The server returned an invalid response. Retry or check deployment configuration.')
  }
}

export async function apiBlob(path: string, options: { signal?: AbortSignal } = {}): Promise<Blob> {
  assertApiPath(path)
  let response: Response
  try {
    response = await fetch(path, {
      method: 'GET',
      credentials: 'same-origin',
      signal: options.signal,
      headers: {
        Accept: 'application/octet-stream',
        ...browserAuthorizationHeaders()
      }
    })
  } catch {
    throw new ApiError(0, 'network', 'Unable to reach the server. Check your connection and retry.')
  }
  if (!response.ok) await parseFailedResponse(response)
  try {
    return await response.blob()
  } catch {
    throw new ApiError(502, 'invalid_response', 'The server returned invalid binary content. Retry or check deployment configuration.')
  }
}
