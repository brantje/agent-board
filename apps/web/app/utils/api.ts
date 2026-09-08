export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message) }
}
const messages: Record<number, string> = {
  401: 'Sign in through your deployment to continue.',
  403: 'You do not have permission for this action.',
  404: 'This resource is unavailable or belongs to another project.',
  409: 'The state has changed or this action conflicts with workflow policy. Refresh and try again.',
  422: 'Check the form values and try again.',
  400: 'Check the form values, resource eligibility and repository configuration.'
}
export function apiPath(resource: string, projectId?: string, id?: string) {
  const segment = (value: string) => {
    if (!/^[a-zA-Z0-9_-]+$/.test(value)) throw new Error('Invalid API path segment')
    return value
  }
  return `/api/${projectId ? `projects/${segment(projectId)}/` : ''}${segment(resource)}${id ? `/${segment(id)}` : ''}`
}
export async function apiRequest<T>(path: string, options: { method?: string; body?: unknown; signal?: AbortSignal; headers?: Record<string, string> } = {}): Promise<T> {
  if (!path.startsWith('/api/') || path.includes('..') || path.includes('\\')) throw new Error('Invalid API path')
  let response: Response
  try {
    response = await fetch(path, {
      method: options.method ?? 'GET', credentials: 'same-origin', signal: options.signal,
      headers: { Accept: 'application/json', ...(options.body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...options.headers },
      body: options.body === undefined ? undefined : JSON.stringify(options.body)
    })
  } catch { throw new ApiError(0, 'network', 'Unable to reach the server. Check your connection and retry.') }
  if (!response.ok) throw new ApiError(response.status, 'request_failed', messages[response.status] ?? 'The server could not complete the request. Retry or check deployment logs.')
  if (response.status === 204) return undefined as T
  try { return await response.json() as T } catch { throw new ApiError(502, 'invalid_response', 'The server returned an invalid response. Retry or check deployment configuration.') }
}
