import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiRequest, ApiError, apiPath } from '../app/utils/api'

afterEach(() => vi.unstubAllGlobals())
describe('Go API transport', () => {
  it('encodes scoped paths and refuses unsafe path segments', () => {
    expect(apiPath('issues', 'project-1', 'issue-1')).toBe('/api/projects/project-1/issues/issue-1')
    expect(apiPath('providers')).toBe('/api/providers')
    expect(() => apiPath('../secrets')).toThrow()
  })
  it('sends JSON mutations to the same-origin Go API without ambient privilege', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response('{"id":"saved"}'))
    vi.stubGlobal('fetch', fetch)
    expect(await apiRequest('/api/projects', { method: 'POST', body: { name: 'P' } })).toEqual({ id: 'saved' })
    expect(fetch).toHaveBeenCalledWith('/api/projects', expect.objectContaining({ method: 'POST', body: '{"name":"P"}', credentials: 'same-origin' }))
    expect(() => apiPath('providers', '..')).toThrow()
  })
  it.each([401, 403, 404, 409, 422, 500])('returns a safe actionable error for %s without reflecting payloads', async status => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"error":{"code":"unsafe","message":"sk-secret"}}', { status })))
    try { await apiRequest('/api/providers'); throw new Error('expected rejection') } catch (error) {
      expect(error).toBeInstanceOf(ApiError)
      expect((error as ApiError).status).toBe(status)
      expect((error as Error).message).not.toContain('sk-secret')
    }
  })
  it('handles network failures, malformed responses, and no-content', async () => {
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
})
