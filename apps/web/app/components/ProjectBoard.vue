<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Agent, Issue, Project } from '../types/api'
import { apiPath } from '../utils/api'
import { boardColumnSurface, boardColumns } from '../utils/issues'
import { useResource } from '../composables/useResource'
import { useRefresh } from '../composables/useRefresh'

const props = defineProps<{ projectId: string }>()
const project = useResource<Project>(() => apiPath('projects', undefined, props.projectId))
const issues = useResource<Issue[]>(() => apiPath('issues', props.projectId))
const agents = useResource<Agent[]>(() => apiPath('agents', props.projectId))
const search = ref('')
const open = ref(false)

const title = computed(() => project.data.value ? `${project.data.value.name} / Board` : 'Project Board')
const columns = computed(() => boardColumns(issues.data.value || [], search.value))
const pending = computed(() => project.pending.value || issues.pending.value || agents.pending.value)
const error = computed(() => project.error.value || issues.error.value || agents.error.value)

function agentName(issue: Issue) {
  return agents.data.value?.find(agent => agent.id === issue.assignedAgentId)?.name
}

async function refreshAll() {
  await Promise.all([project.refresh(), issues.refresh(), agents.refresh()])
}

async function created() {
  open.value = false
  await refreshAll()
}

useRefresh(refreshAll)
</script>

<template>
  <PageFrame :title="title" description="Durable work · six Issue workflow states">
    <template #actions>
      <UInput v-model="search" aria-label="Filter issues" placeholder="Filter issues…" icon="i-lucide-search" />
      <UButton label="Refresh" variant="outline" color="neutral" @click="refreshAll" />
      <UButton label="New issue" icon="i-lucide-plus" @click="open = true" />
    </template>

    <AsyncState :pending="pending" :error="error" @retry="refreshAll">
      <div class="flex min-h-[calc(100dvh-10rem)] gap-3 overflow-x-auto pb-3" role="region" aria-label="Issue board" tabindex="0">
        <section
          v-for="column in columns"
          :key="column.status"
          :data-status="column.status"
          :class="['w-64 min-w-64 flex-1 border border-default', boardColumnSurface(column.status)]"
        >
          <header class="flex items-center justify-between gap-2 border-b border-default p-3">
            <h2 class="section-label">{{ column.label }}</h2>
            <UBadge :label="String(column.issues.length)" color="neutral" variant="subtle" />
          </header>
          <div class="space-y-2 p-2">
            <IssueCard v-for="issue in column.issues" :key="issue.id" :issue="issue" :agent-name="agentName(issue)" />
            <p v-if="!column.issues.length" class="px-2 py-4 text-xs text-muted">{{ search ? 'No matching issues' : 'No issues' }}</p>
          </div>
        </section>
      </div>
    </AsyncState>

    <UModal v-model:open="open" title="New issue" description="Create work in this Project.">
      <template #body>
        <IssueEditor :project-id="projectId" @saved="created" @cancel="open = false" />
      </template>
    </UModal>
  </PageFrame>
</template>
