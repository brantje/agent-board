import { flushPromises } from '@vue/test-utils'
import { effectScope, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../app/utils/api', async importOriginal => {
  const actual = await importOriginal<typeof import('../app/utils/api')>()
  return {
    ...actual,
    apiRequest: vi.fn(),
    providerModelsPath: (id: string, projectId?: string) => (
      projectId ? `/api/projects/${projectId}/providers/${id}/models` : `/api/providers/${id}/models`
    )
  }
})

import { ApiError, apiRequest } from '../app/utils/api'
import { useProviderListHealth, type ProviderHealthOverlay } from '../app/composables/useProviderListHealth'

describe('useProviderListHealth', () => {
  beforeEach(() => {
    vi.mocked(apiRequest).mockReset()
  })

  it('marks providers healthy with discovered totals', async () => {
    vi.mocked(apiRequest).mockResolvedValue({ models: [{ id: 'a' }, { id: 'b' }], total: 5 })
    const providers = ref([{ id: 'p1' }])
    const scope = effectScope()
    let overlays = ref<Record<string, ProviderHealthOverlay>>({})
    scope.run(() => {
      overlays = useProviderListHealth(providers).overlays
    })
    await flushPromises()
    expect(overlays.value.p1).toEqual({ checking: false, healthStatus: 'HEALTHY', filtered: 2, total: 5 })
    scope.stop()
  })

  it('falls back to model list length when total is missing', async () => {
    vi.mocked(apiRequest).mockResolvedValue({ models: [{ id: 'a' }] })
    const providers = ref([{ id: 'p1' }])
    const scope = effectScope()
    let overlays = ref<Record<string, ProviderHealthOverlay>>({})
    scope.run(() => {
      overlays = useProviderListHealth(providers).overlays
    })
    await flushPromises()
    expect(overlays.value.p1).toEqual({ checking: false, healthStatus: 'HEALTHY', filtered: 1, total: 1 })
    scope.stop()
  })

  it('marks providers unhealthy only for discovery failures', async () => {
    vi.mocked(apiRequest).mockRejectedValue(new ApiError(502, 'provider_model_discovery_failed', 'Unable to discover models from the Provider API.'))
    const providers = ref([{ id: 'p1' }])
    const scope = effectScope()
    let overlays = ref<Record<string, ProviderHealthOverlay>>({})
    scope.run(() => {
      overlays = useProviderListHealth(providers).overlays
    })
    await flushPromises()
    expect(overlays.value.p1).toEqual({ checking: false, healthStatus: 'UNHEALTHY' })
    scope.stop()
  })

  it('does not mark providers unhealthy for non-discovery probe failures', async () => {
    vi.mocked(apiRequest).mockRejectedValue(new ApiError(0, 'network', 'Unable to reach the Agent Board API.'))
    const providers = ref([{ id: 'p1', healthStatus: 'HEALTHY' }])
    const scope = effectScope()
    let overlays = ref<Record<string, ProviderHealthOverlay>>({})
    scope.run(() => {
      overlays = useProviderListHealth(providers).overlays
    })
    await flushPromises()
    expect(overlays.value.p1).toEqual({ checking: false })
    scope.stop()
  })

  it('scopes model probes to project providers', async () => {
    vi.mocked(apiRequest).mockResolvedValue({ models: [], total: 0 })
    const providers = ref([{ id: 'p1' }])
    const projectId = ref('proj')
    const scope = effectScope()
    scope.run(() => {
      useProviderListHealth(providers, projectId)
    })
    await flushPromises()
    expect(apiRequest).toHaveBeenCalledWith('/api/projects/proj/providers/p1/models', { signal: expect.any(AbortSignal) })
    scope.stop()
  })

  it('preserves prior overlay counts when a refresh probe fails transiently', async () => {
    vi.mocked(apiRequest)
      .mockResolvedValueOnce({ models: [{ id: 'a' }, { id: 'b' }], total: 4 })
      .mockRejectedValueOnce(new ApiError(0, 'network', 'Unable to reach the Agent Board API.'))
    const providers = ref([{ id: 'p1' }])
    const scope = effectScope()
    let overlays = ref<Record<string, ProviderHealthOverlay>>({})
    let refresh = async () => {}
    scope.run(() => {
      const result = useProviderListHealth(providers)
      overlays = result.overlays
      refresh = result.refresh
    })
    await flushPromises()
    await refresh()
    await flushPromises()
    expect(overlays.value.p1).toEqual({ checking: false, filtered: 2, total: 4, healthStatus: 'HEALTHY' })
    scope.stop()
  })

  it('aborts in-flight probes when the provider list changes', async () => {
    let resolveRequest: ((value: { models: Array<{ id: string }>; total: number }) => void) | undefined
    vi.mocked(apiRequest).mockImplementation(() => new Promise(resolve => {
      resolveRequest = resolve
    }))
    const providers = ref([{ id: 'p1' }])
    const scope = effectScope()
    scope.run(() => {
      useProviderListHealth(providers)
    })
    await flushPromises()
    providers.value = [{ id: 'p2' }]
    await flushPromises()
    resolveRequest?.({ models: [], total: 0 })
    await flushPromises()
    scope.stop()
  })
})
