import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiRequest, ApiError, apiPath, apiQuery, apiText, providerModelsPath } from '../app/utils/api'
import { AUTH_STORAGE_KEY } from '../app/utils/auth-storage'

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
  sessionStorage.clear()
})

function storeSessionCredentials(accessToken = 'access-token') {
  sessionStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify({
    accessToken,
    accessTokenExpiresAt: '2026-09-13T09:00:00Z',
    refreshToken: 'refresh-token',
    refreshTokenExpiresAt: '2026-10-13T09:00:00Z'
  }))
}

describe('Go API transport', () => {
  it('encodes scoped paths and refuses unsafe path segments', () => {
    expect(apiPath('issues', 'project-1', 'AB-1')).toBe('/api/projects/project-1/issues/AB-1')
    expect(apiPath('providers')).toBe('/api/providers')
    expect(apiPath('providers', 'project-1')).toBe('/api/projects/project-1/providers')
    expect(providerModelsPath('provider-1')).toBe('/api/providers/provider-1/models')
    expect(providerModelsPath('provider-1', 'project-1')).toBe('/api/projects/project-1/providers/provider-1/models')
    expect(() => apiPath('../secrets')).toThrow()
  })

  it('sends stored bearer credentials with ordinary same-origin API requests', async () => {
    storeSessionCredentials('stored-access')
    const fetch = vi.fn().mockResolvedValue(new Response('{"id":"saved"}'))
    vi.stubGlobal('fetch', fetch)

    expect(await apiRequest('/api/projects', { method: 'POST', body: { name: 'P' } })).toEqual({ id: 'saved' })
    expect(fetch).toHaveBeenCalledWith('/api/projects', expect.objectContaining({
      method: 'POST',
      body: '{"name":"P"}',
      credentials: 'same-origin',
      headers: expect.objectContaining({ Authorization: 'Bearer stored-access' })
    }))
  })

  it('allows an explicit authorization header to override browser storage', async () => {
    storeSessionCredentials('stored-access')
    const fetch = vi.fn().mockResolvedValue(new Response('{}'))
    vi.stubGlobal('fetch', fetch)

    await apiRequest('/api/projects', { headers: { Authorization: 'Bearer current-access' } })
    expect(fetch).toHaveBeenCalledWith('/api/projects', expect.objectContaining({
      headers: expect.objectContaining({ Authorization: 'Bearer current-access' })
    }))
  })

  it('uses stable backend error codes for actionable messages without reflecting server payload text', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"error":{"code":"invalid_argument","message":"sk-secret"}}', { status: 400 })))
    try {
      await apiRequest('/api/providers')
      throw new Error('expected rejection')
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError)
      expect((error as ApiError).status).toBe(400)
      expect((error as ApiError).code).toBe('invalid_argument')
      expect((error as Error).message).toContain('server rejected')
      expect((error as Error).message).not.toContain('sk-secret')
    }
  })

  it.each([
    [403, 'forbidden'],
    [404, 'runtime_not_found'],
    [409, 'conflict'],
    [409, 'issue_done'],
    [422, 'execution_configuration_invalid'],
    [500, 'internal_error']
  ] as const)('returns a safe actionable error for status %s and code %s', async (status, code) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code, message: 'never reflect me' } }), { status })))
    try {
      await apiRequest('/api/providers')
      throw new Error('expected rejection')
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError)
      expect((error as ApiError).status).toBe(status)
      expect((error as ApiError).code).toBe(code)
      expect((error as Error).message).not.toContain('never reflect me')
      if (code === 'issue_done') expect((error as Error).message).toContain('Reopen')
    }
  })

  it('ignores malformed or unsafe error envelopes', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"error":{"code":"<script>","message":"unsafe"}}', { status: 400 })))
    await expect(apiRequest('/api/providers')).rejects.toMatchObject({ code: 'request_failed' })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('not json', { status: 400 })))
    await expect(apiRequest('/api/providers')).rejects.toMatchObject({ code: 'request_failed' })
  })

  it('handles network failures, malformed success responses, and no-content', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('secret-url')))
    await expect(apiRequest('/api/projects')).rejects.toThrow('Unable to reach')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('not json')))
    await expect(apiRequest('/api/projects')).rejects.toThrow('invalid response')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })))
    expect(await apiRequest('/api/projects')).toBeUndefined()
  })

  it('refuses external and non-API requests', async () => {
    await expect(apiRequest('https://evil.test/api')).rejects.toThrow('API path')
    await expect(apiRequest('/api/../private')).rejects.toThrow('API path')
  })

  it('encodes query strings and sends bearer credentials for bounded raw-output text', async () => {
    expect(apiQuery('/api/projects/p/questions', { issueId: 'AB-1', status: 'OPEN' })).toBe('/api/projects/p/questions?issueId=AB-1&status=OPEN')
    expect(apiQuery('/api/projects/p/runs/r/events', { afterSequence: '3' })).toBe('/api/projects/p/runs/r/events?afterSequence=3')
    expect(() => apiQuery('/api/../x')).toThrow()
    storeSessionCredentials('raw-output-access')
    const fetch = vi.fn().mockResolvedValue(new Response('log chunk', { headers: { 'Content-Type': 'text/plain' } }))
    vi.stubGlobal('fetch', fetch)
    expect(await apiText('/api/projects/p/runs/r/raw-output/chunk-1')).toBe('log chunk')
    expect(fetch).toHaveBeenCalledWith('/api/projects/p/runs/r/raw-output/chunk-1', expect.objectContaining({
      credentials: 'same-origin',
      headers: expect.objectContaining({ Authorization: 'Bearer raw-output-access' })
    }))
  })
})
