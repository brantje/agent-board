import { shallowRef, watch, onBeforeUnmount, toValue, type MaybeRefOrGetter } from 'vue'
import { apiRequest, providerModelsPath } from '../utils/api'
import type { ConfigRecord } from '../utils/configuration'

type ProviderModelList = { models: Array<{ id: string }>; total: number }

export type ProviderHealthOverlay = {
  checking: boolean
  healthStatus?: 'HEALTHY' | 'UNHEALTHY'
  filtered?: number
  total?: number
}

export function useProviderListHealth(providers: MaybeRefOrGetter<ConfigRecord[] | undefined>, projectId?: MaybeRefOrGetter<string | undefined>) {
  const overlays = shallowRef<Record<string, ProviderHealthOverlay>>({})
  let generation = 0
  const controllers = new Map<string, AbortController>()

  function clearControllers() {
    for (const controller of controllers.values()) controller.abort()
    controllers.clear()
  }

  async function probeProvider(id: string, current: number) {
    const scope = toValue(projectId)
    const controller = new AbortController()
    controllers.set(id, controller)
    overlays.value = { ...overlays.value, [id]: { checking: true } }
    try {
      const result = await apiRequest<ProviderModelList>(providerModelsPath(id, scope), { signal: controller.signal })
      if (current !== generation) return
      const total = result.total ?? result.models?.length ?? 0
      overlays.value = {
        ...overlays.value,
        [id]: { checking: false, healthStatus: 'HEALTHY', filtered: total, total }
      }
    } catch (failure) {
      if (current !== generation || controller.signal.aborted) return
      overlays.value = {
        ...overlays.value,
        [id]: { checking: false, healthStatus: 'UNHEALTHY' }
      }
    } finally {
      controllers.delete(id)
    }
  }

  async function refreshListed() {
    const current = ++generation
    clearControllers()
    const items = toValue(providers) ?? []
    const ids = items.map(item => String(item.id ?? '').trim()).filter(Boolean)
    const next: Record<string, ProviderHealthOverlay> = {}
    for (const id of ids) next[id] = { checking: true }
    overlays.value = next
    await Promise.all(ids.map(id => probeProvider(id, current)))
  }

  watch(() => [toValue(providers), toValue(projectId)] as const, () => { void refreshListed() }, { immediate: true, deep: true })
  onBeforeUnmount(() => { generation++; clearControllers() })

  return { overlays, refresh: refreshListed }
}
