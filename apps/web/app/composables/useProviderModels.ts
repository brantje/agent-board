import { ref, shallowRef, watch, onBeforeUnmount, toValue, type MaybeRefOrGetter } from 'vue'
import { apiRequest, providerModelsPath, type ApiError, type ProviderModel } from '../utils/api'

type ProviderModelList = { models: ProviderModel[] }

export function useProviderModels(providerId: MaybeRefOrGetter<string>) {
  const models = shallowRef<ProviderModel[]>([])
  const error = shallowRef<ApiError>()
  const pending = ref(false)
  let generation = 0
  let controller: AbortController | undefined

  async function refresh() {
    const id = String(toValue(providerId) ?? '').trim()
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
      const result = await apiRequest<ProviderModelList>(providerModelsPath(id), { signal: controller.signal })
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

  watch(() => toValue(providerId), (next, previous) => {
    if (previous !== undefined && String(next ?? '') !== String(previous ?? '')) {
      models.value = []
      error.value = undefined
    }
    void refresh()
  }, { immediate: true })
  onBeforeUnmount(() => { generation++; controller?.abort() })

  return { models, error, pending, refresh }
}
