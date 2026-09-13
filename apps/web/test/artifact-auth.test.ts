import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiBlob } from '../app/utils/api'
import { clearAuthStorage, writeAuthStorage } from '../app/utils/auth-storage'

afterEach(() => {
  vi.unstubAllGlobals()
  clearAuthStorage(localStorage, sessionStorage)
})

describe('authenticated artifact transport', () => {
  it('reads protected binary content with the stored bearer token', async () => {
    writeAuthStorage(localStorage, sessionStorage, {
      accessToken: 'artifact-access-token',
      accessTokenExpiresAt: '2026-09-13T12:00:00Z',
      refreshToken: 'artifact-refresh-token',
      refreshTokenExpiresAt: '2026-10-13T12:00:00Z'
    }, false)
    const fetch = vi.fn().mockResolvedValue(new Response(new Blob(['artifact-content'], { type: 'text/plain' })))
    vi.stubGlobal('fetch', fetch)

    const blob = await apiBlob('/api/projects/p/runs/r/artifacts/a')

    expect(await blob.text()).toBe('artifact-content')
    expect(fetch).toHaveBeenCalledWith('/api/projects/p/runs/r/artifacts/a', expect.objectContaining({
      method: 'GET',
      credentials: 'same-origin',
      headers: expect.objectContaining({
        Accept: 'application/octet-stream',
        Authorization: 'Bearer artifact-access-token'
      })
    }))
  })

  it('preserves not-found isolation for protected binary content', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'project_not_found' } }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' }
    })))

    await expect(apiBlob('/api/projects/p/runs/r/artifacts/a')).rejects.toMatchObject({
      status: 404,
      code: 'project_not_found'
    })
  })
})
