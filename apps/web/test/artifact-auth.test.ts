import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiBlob, downloadApiFile } from '../app/utils/api'
import { clearAuthStorage, writeAuthStorage } from '../app/utils/auth-storage'

afterEach(() => {
  vi.restoreAllMocks()
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

  it('downloads the authenticated blob without putting credentials in the URL', async () => {
    writeAuthStorage(localStorage, sessionStorage, {
      accessToken: 'download-access-token',
      accessTokenExpiresAt: '2026-09-13T12:00:00Z',
      refreshToken: 'download-refresh-token',
      refreshTokenExpiresAt: '2026-10-13T12:00:00Z'
    }, false)
    const fetch = vi.fn().mockResolvedValue(new Response(new Blob(['download-me'])))
    vi.stubGlobal('fetch', fetch)
    const createObjectURL = vi.fn().mockReturnValue('blob:artifact')
    const revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    await downloadApiFile('/api/projects/p/runs/r/artifacts/a', 'candidate.patch')

    expect(fetch).toHaveBeenCalledWith('/api/projects/p/runs/r/artifacts/a', expect.objectContaining({
      headers: expect.objectContaining({ Authorization: 'Bearer download-access-token' })
    }))
    expect(createObjectURL).toHaveBeenCalledOnce()
    expect(click).toHaveBeenCalledOnce()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:artifact')
    expect(document.querySelector('a[download="candidate.patch"]')).toBeNull()
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
