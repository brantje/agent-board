<script setup lang="ts">
import { ref } from 'vue'
import type { Project } from '../types/api'
import { apiPath } from '../utils/api'
import { useProjectPermissions } from '../composables/useProjectPermissions'
import { useResource } from '../composables/useResource'
import RunnerManager from './RunnerManager.vue'

const props = defineProps<{ projectId: string }>()
const { data, pending, error, refresh } = useResource<Project>(() => apiPath('projects', undefined, props.projectId))
const {
  role,
  pending: rolePending,
  error: roleError,
  canAdmin,
  refresh: refreshRole
} = useProjectPermissions(() => props.projectId)
const saved = ref(false)

function projectUpdated(project: Project) {
  saved.value = true
  data.value = project
}

function retry() {
  void refresh()
  void refreshRole()
}
</script>

<template>
  <PageFrame title="Runners" description="Project settings · execution capacity">
    <UAlert v-if="saved" title="Saved" color="success" class="mb-4" />
    <AsyncState
      :pending="pending || rolePending"
      :error="error || roleError"
      :empty="!data || !role"
      empty-title="Project unavailable"
      empty-description="This Project is unavailable or belongs to another project scope."
      @retry="retry"
    >
      <RunnerManager
        v-if="data && role"
        :project-id="projectId"
        :can-admin="canAdmin"
        :allow-internal-runner="data.allowInternalRunner"
        @project-updated="projectUpdated"
      />
    </AsyncState>
  </PageFrame>
</template>
