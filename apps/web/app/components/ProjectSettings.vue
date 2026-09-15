<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Project } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { useProjectPermissions } from '../composables/useProjectPermissions'
import { useResource } from '../composables/useResource'
import ProjectAccessSettings from './ProjectAccessSettings.vue'
import ProjectEditor from './ProjectEditor.vue'

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
const editorKey = ref(0)
const workflowSaving = ref(false)
const workflowError = ref<Error | null>(null)
const strictOrder = computed(() => data.value?.workflowSettings?.strictOrder === true)

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

async function saveStrictOrder(value: boolean) {
  if (!canAdmin.value || !data.value || workflowSaving.value) return
  workflowSaving.value = true
  workflowError.value = null
  saved.value = false
  try {
    const project = await apiRequest<Project>(apiPath('projects', undefined, props.projectId), {
      method: 'PATCH',
      body: {
        workflowSettings: {
          ...data.value.workflowSettings,
          strictOrder: value
        }
      }
    })
    data.value = project
    saved.value = true
  } catch (value) {
    workflowError.value = value instanceof Error ? value : new Error('Unable to save workflow settings.')
  } finally {
    workflowSaving.value = false
  }
}
</script>

<template>
  <PageFrame title="Project" description="Project settings · repository source context">
    <UAlert v-if="saved" title="Saved" color="success" class="mb-4" />
    <AsyncState
      :pending="pending || rolePending"
      :error="error || roleError"
      :empty="!data || !role"
      empty-title="Project unavailable"
      empty-description="This Project is unavailable or belongs to another project scope."
      @retry="retry"
    >
      <template v-if="data && role">
        <ProjectEditor
          v-if="canAdmin"
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
              <p class="text-sm text-muted">Project settings are read-only for your {{ role }} role.</p>
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

        <UCard data-testid="workflow-settings" class="mt-4">
          <template #header>
            <div>
              <h2 class="text-base font-semibold">Workflow</h2>
              <p class="text-sm text-muted">Control how queued agent work follows the shared Board order.</p>
            </div>
          </template>
          <UAlert
            v-if="workflowError"
            title="Unable to save workflow settings"
            :description="workflowError.message"
            color="error"
            class="mb-4"
          />
          <UFormField
            label="Strict board order"
            name="strictOrder"
            description="Agents consider queued Issues in Board order and run the first Issue that currently passes the existing dependency, workspace, capacity, runner, and other admission checks."
          >
            <USwitch
              :model-value="strictOrder"
              :disabled="!canAdmin || workflowSaving"
              @update:model-value="value => saveStrictOrder(Boolean(value))"
            />
          </UFormField>
          <p v-if="!canAdmin" class="mt-2 text-xs text-muted">Project workflow settings are read-only for your {{ role }} role.</p>
        </UCard>

        <ProjectAccessSettings :project-id="projectId" :effective-role="role" />
      </template>
    </AsyncState>
  </PageFrame>
</template>
