<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { Project, Run } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { runStatusLabel } from '../utils/runs'
import { useRefresh } from '../composables/useRefresh'

const props = defineProps<{ projectId?: string }>()
const pending = ref(true)
const error = ref<Error>()
const rows = ref<(Run & { projectName?: string })[]>([])
const partialErrors = ref<{ id: string; name: string; message: string }[]>([])

async function load() {
  pending.value = !rows.value.length
  error.value = undefined
  try {
    if (props.projectId) {
      rows.value = await apiRequest<Run[]>(apiPath('runs', props.projectId))
      partialErrors.value = []
      return
    }
    const projects = await apiRequest<Project[]>(apiPath('projects'))
    const results = await Promise.all(projects.map(async project => {
      try {
        const runs = await apiRequest<Run[]>(apiPath('runs', project.id))
        return { ok: true as const, project, runs }
      } catch (failure) {
        return { ok: false as const, project, error: failure as Error }
      }
    }))
    rows.value = results.flatMap(result => result.ok ? result.runs.map(item => ({ ...item, projectName: result.project.name })) : [])
    partialErrors.value = results.flatMap(result => result.ok ? [] : [{ id: result.project.id, name: result.project.name, message: result.error.message }])
  } catch (failure) {
    error.value = failure as Error
    rows.value = []
  } finally {
    pending.value = false
  }
}

useRefresh(load)
onMounted(load)
const empty = computed(() => !pending.value && !error.value && !rows.value.length)
</script>

<template>
  <PageFrame :title="projectId ? 'Project Runs' : 'Runs'" description="Run status is not a Board column. Execution continues on the server.">
    <AsyncState :pending="pending" :error="error" :empty="empty" empty-title="No Runs yet" empty-description="Runs appear after an Agent is assigned to an Issue. Execution continues on the server." @retry="load">
      <UAlert
        v-for="failure in partialErrors"
        :key="failure.id"
        class="mb-3"
        color="warning"
        title="Some projects could not be loaded"
        :description="`${failure.name}: ${failure.message}`"
      />
      <div class="grid gap-3">
        <UCard v-for="run in rows" :key="run.id">
          <div class="flex flex-wrap items-center gap-3">
            <div class="min-w-0 flex-1">
              <NuxtLink :to="`/projects/${run.projectId}/runs/${run.id}`" class="font-medium hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">
                Attempt {{ run.attempt }} · {{ runStatusLabel(run.status) }}
              </NuxtLink>
              <p class="text-xs text-muted">
                <span v-if="run.projectName">{{ run.projectName }} · </span>
                Issue <span class="font-mono">{{ run.issueId }}</span>
                <template v-if="run.agentId"> · Agent <span class="font-mono">{{ run.agentId }}</span></template>
              </p>
            </div>
            <UBadge color="neutral" variant="subtle" :label="runStatusLabel(run.status)" />
            <UButton label="Open Run" :to="`/projects/${run.projectId}/runs/${run.id}`" variant="outline" />
          </div>
          <p v-if="run.queueReason" class="mt-2 text-sm">Queue reason: {{ run.queueReason }}</p>
          <p v-if="run.failureReason" class="mt-2 text-sm text-error">Execution failure: {{ run.failureReason }}</p>
          <p class="mt-2 text-xs text-muted">Created {{ run.createdAt }}<template v-if="run.startedAt"> · Started {{ run.startedAt }}</template></p>
        </UCard>
      </div>
    </AsyncState>
  </PageFrame>
</template>
