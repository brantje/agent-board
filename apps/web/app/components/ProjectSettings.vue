<script setup lang="ts">
import { ref } from 'vue'
import type { Project } from '../types/api'
import { apiPath } from '../utils/api'
import { useResource } from '../composables/useResource'
import ProjectAccessSettings from './ProjectAccessSettings.vue'
import ProjectEditor from './ProjectEditor.vue'

const props = defineProps<{ projectId: string }>()
const { data, pending, error, refresh } = useResource<Project>(() => apiPath('projects', undefined, props.projectId))
const saved = ref(false)
const editorKey = ref(0)

function savedProject(project: Project) {
  saved.value = true
  data.value = project
}

function cancel() {
  editorKey.value += 1
}
</script>

<template>
  <PageFrame title="Project" description="Project settings · repository source context">
    <UAlert v-if="saved" title="Saved" color="success" class="mb-4" />
    <AsyncState
      :pending="pending"
      :error="error"
      :empty="!data"
      empty-title="Project unavailable"
      empty-description="This Project is unavailable or belongs to another project scope."
      @retry="refresh"
    >
      <template v-if="data">
        <ProjectEditor :key="editorKey" :project="data" @saved="savedProject" @cancel="cancel" />
        <ProjectAccessSettings :project-id="projectId" />
      </template>
    </AsyncState>
  </PageFrame>
</template>
