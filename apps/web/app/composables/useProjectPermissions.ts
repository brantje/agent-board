import { computed, onBeforeUnmount, onMounted, shallowRef, toValue, watch, type MaybeRefOrGetter } from 'vue'
import type { ProjectRole, ProjectRoleResponse } from '../types/project-access'
import { apiPath, apiRequest, type ApiError } from '../utils/api'

export function useProjectPermissions(projectId: MaybeRefOrGetter<string | undefined>) {
  const role = shallowRef<ProjectRole>()
  const error = shallowRef<ApiError>()
  const pending = shallowRef(false)
  let generation = 0
  let controller: AbortController | undefined

  async function refresh() {
    const current = ++generation
    controller?.abort()
    const id = toValue(projectId)
    role.value = undefined
    error.value = undefined
    if (!id) {
      pending.value = false
      return
    }
    controller = new AbortController()
    pending.value = true
    try {
      const result = await apiRequest<ProjectRoleResponse>(`${apiPath('projects', undefined, id)}/access/effective-role`, { signal: controller.signal })
      if (current === generation) role.value = result.role
    } catch (failure) {
      if (current === generation) error.value = failure as ApiError
    } finally {
      if (current === generation) pending.value = false
    }
  }

  onMounted(refresh)
  watch(() => toValue(projectId), refresh)
  onBeforeUnmount(() => {
    generation++
    controller?.abort()
  })

  return {
    role,
    error,
    pending,
    canRead: computed(() => role.value === 'viewer' || role.value === 'member' || role.value === 'admin'),
    canMutate: computed(() => role.value === 'member' || role.value === 'admin'),
    canAdmin: computed(() => role.value === 'admin'),
    refresh
  }
}
