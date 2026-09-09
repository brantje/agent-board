<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { Project, Question, Review, Run } from '../types/api'
import { apiPath, apiQuery, apiRequest } from '../utils/api'
import { composeInbox, type InboxItem } from '../utils/inbox'
import { runStatusLabel } from '../utils/runs'
import { isBoardActivityEvent } from '../utils/events'
import { useProjectEvents } from '../composables/useProjectEvents'

const pending = ref(true)
const error = ref<Error>()
const items = ref<InboxItem[]>([])
const partialErrors = ref<{ id: string; name: string; message: string }[]>([])
const projectIds = ref<string[]>([])

async function load() {
  pending.value = !items.value.length
  error.value = undefined
  try {
    const projects = await apiRequest<Project[]>(apiPath('projects'))
    projectIds.value = projects.map(project => project.id)
    const failures: { id: string; name: string; message: string }[] = []
    const groups = await Promise.all(projects.map(async project => {
      try {
        const [questions, reviews, runs] = await Promise.all([
          apiRequest<Question[]>(apiQuery(apiPath('questions', project.id), { status: 'OPEN' })),
          apiRequest<Review[]>(apiQuery(apiPath('reviews', project.id), { status: 'PENDING' })),
          apiRequest<Run[]>(apiPath('runs', project.id))
        ])
        return { project, questions, reviews, runs }
      } catch (failure) {
        failures.push({ id: project.id, name: project.name, message: (failure as Error).message })
        return undefined
      }
    }))
    items.value = composeInbox(groups.filter((group): group is NonNullable<typeof group> => Boolean(group)))
    partialErrors.value = failures
  } catch (failure) {
    error.value = failure as Error
    items.value = []
    partialErrors.value = []
    projectIds.value = []
  } finally {
    pending.value = false
  }
}

useProjectEvents(projectIds, event => {
  if (!isBoardActivityEvent(event.type)) return
  void load()
})
onMounted(load)
const empty = computed(() => !pending.value && !error.value && !items.value.length && !partialErrors.value.length)

function itemStatus(item: InboxItem) {
  return item.kind === 'run' ? runStatusLabel(item.status) : item.status.replaceAll('_', ' ')
}
</script>

<template>
  <PageFrame title="Inbox" description="Blocking questions, pending Reviews, and Runs that need attention. This list is composed from project APIs.">
    <AsyncState :pending="pending" :error="error" :empty="empty" empty-title="Nothing needs attention" empty-description="Blocking questions, pending Reviews, and Runs that need a decision will appear here." @retry="load">
      <UAlert
        v-for="failure in partialErrors"
        :key="failure.id"
        class="mb-3"
        color="warning"
        title="Some projects could not be loaded"
        :description="`${failure.name}: ${failure.message}`"
      />
      <div v-if="items.length" class="grid gap-3">
        <UCard v-for="item in items" :key="`${item.kind}-${item.id}`">
          <div class="flex flex-wrap items-center gap-3">
            <div class="min-w-0 flex-1">
              <NuxtLink :to="item.to" class="font-medium hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">{{ item.title }}</NuxtLink>
              <p class="text-xs text-muted">{{ item.projectName }} · {{ item.kind }}</p>
            </div>
            <RunStatus v-if="item.kind === 'run'" :status="item.status" :label="itemStatus(item)" />
            <UBadge v-else color="neutral" variant="subtle" :label="itemStatus(item)" />
          </div>
        </UCard>
      </div>
    </AsyncState>
  </PageFrame>
</template>
