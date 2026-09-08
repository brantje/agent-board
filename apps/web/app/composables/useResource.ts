import { ref, shallowRef, watch, onMounted, onBeforeUnmount, toValue, type MaybeRefOrGetter } from 'vue'
import { apiRequest, type ApiError } from '../utils/api'

/** Disposable presentation cache. Every mount and scope change reads durable Go state. */
export function useResource<T>(path: MaybeRefOrGetter<string>) {
  const data = shallowRef<T>()
  const error = shallowRef<ApiError>()
  const pending = ref(true)
  let generation = 0
  let controller: AbortController | undefined
  async function refresh() {
    const current = ++generation
    controller?.abort()
    controller = new AbortController()
    pending.value = true
    error.value = undefined
    try {
      const result = await apiRequest<T>(toValue(path), { signal: controller.signal })
      if (current === generation) data.value = result
    } catch (failure) {
      if (current === generation) { error.value = failure as ApiError; data.value = undefined }
    } finally { if (current === generation) pending.value = false }
  }
  onMounted(refresh)
  watch(() => toValue(path), () => { data.value = undefined; void refresh() })
  onBeforeUnmount(() => { generation++; controller?.abort() })
  return { data, error, pending, refresh }
}
