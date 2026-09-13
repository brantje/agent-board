<script setup lang="ts">
import { ref } from 'vue'
import type { Project } from '../types/api'
import type { ProjectRoleResponse } from '../types/project-access'
import { apiPath } from '../utils/api'
import { useResource } from '../composables/useResource'
import ProjectAccessSettings from './ProjectAccessSettings.vue'
import ProjectEditor from './ProjectEditor.vue'

const props = defineProps<{ projectId: string }>()
const { data, pending, error, refresh } = useResource<Project>(() => apiPath('projects', undefined, props.projectId))
const {
  data: roleData,
  pending: rolePending,
  error: roleError,
  refresh: refreshRole
} = useResource<ProjectRoleResponse>(() => `/api/projects/${props.projectId}/access/effective-role`)
const saved = ref(false)
const editorKey = ref(0)

function savedProject(project: Project) {
  saved.value = true
  data.value = project
}

function cancel() {
  editorKey.value += 1
}

function retry() {
  void refresh()
  void refreshRole()
}
</script>

<template>
  <PageFrame title="Project" description="Project settings · repository source context">
    <UAlert v-if="saved" title="Saved" color="success" class="mb-4" />
    <AsyncState
      :pending="pending || rolePending"
      :error="error || roleError"
      :empty="!data || !roleData"
      empty-title="Project unavailable"
      empty-description="This Project is unavailable or belongs to another project scope."
      @retry="retry"
    >
      <template v-if="data && roleData">
        <ProjectEditor
          v-if="roleData.role === 'admin'"
          :key="editorKey"
          data-testid="project-settings-editor"
          :project="data"
          @saved="savedProject"
          @cancel="cancel"
        />
        <UCard v-else data-testid="project-settings-readonly">
          <template #header>
            <div>
              <h2 class="text-base font-semibold">Project information</h2>
              <p class="text-sm text-muted">Project settings are read-only for your {{ roleData.role }} role.</p>
            </div>
          </template>
          <dl class="grid gap-4 sm:grid-cols-2">
            <div>
              <dt class="text-sm text-muted">Name</dt>
              <dd class="font-medium">{{ data.name }}</dd>
            </div>
            <div>
              <dt class="text-sm text-muted">Issue prefix</dt>
              <dd class="font-mono">{{ data.issuePrefix }}</dd>
            </div>
            <div>
              <dt class="text-sm text-muted">Source</dt>
              <dd>{{ data.sourceType }}</dd>
            </div>
            <div v-if="data.sourceType === 'git'">
              <dt class="text-sm text-muted">Clone URL</dt>
              <dd class="font-mono break-all">{{ data.cloneUrl }}</dd>
            </div>
            <div v-else>
              <dt class="text-sm text-muted">Repository path</dt>
              <dd class="font-mono break-all">{{ data.repositoryPath }}</dd>
            </div>
            <div v-if="data.sourceType !== 'git'">
              <dt class="text-sm text-muted">Default branch</dt>
              <dd class="font-mono">{{ data.defaultBranch }}</dd>
            </div>
          </dl>
        </UCard>
        <ProjectAccessSettings :project-id="projectId" :effective-role="roleData.role" />
      </template>
    </AsyncState>
  </PageFrame>
</template>
