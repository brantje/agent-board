import { ref, shallowRef, watch, onBeforeUnmount, toValue, type MaybeRefOrGetter } from 'vue'
import { apiRequest, providerModelsPath, type ApiError, type ProviderModel } from '../utils/api'

type ProviderModelList = { models: ProviderModel[] }

export function useProviderModels(providerId: MaybeRefOrGetter<string>, projectId?: MaybeRefOrGetter<string | undefined>) {
  const models = shallowRef<ProviderModel[]>([])
  const error = shallowRef<ApiError>()
  const pending = ref(false)
  let generation = 0
  let controller: AbortController | undefined

  async function refresh() {
    const id = String(toValue(providerId) ?? '').trim()
    const scope = toValue(projectId)
    const current = ++generation
    controller?.abort()
    controller = new AbortController()
    error.value = undefined
    if (!id) {
      models.value = []
      pending.value = false
      return
    }
    pending.value = models.value.length === 0
    try {
      const result = await apiRequest<ProviderModelList>(providerModelsPath(id, scope), { signal: controller.signal })
      if (current === generation) models.value = result.models ?? []
    } catch (failure) {
      if (current === generation) {
        error.value = failure as ApiError
        models.value = []
      }
    } finally {
      if (current === generation) pending.value = false
    }
  }

  watch(() => [toValue(providerId), toValue(projectId)] as const, (next, previous) => {
    if (previous !== undefined && (String(next[0] ?? '') !== String(previous[0] ?? '') || String(next[1] ?? '') !== String(previous[1] ?? ''))) {
      models.value = []
      error.value = undefined
    }
    void refresh()
  }, { immediate: true })
  onBeforeUnmount(() => { generation++; controller?.abort() })

  return { models, error, pending, refresh }
}
