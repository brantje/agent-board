<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Agent, Delegation } from '../types/api'
import { ApiError, apiPath, apiRequest } from '../utils/api'
import { useProjectEvents } from '../composables/useProjectEvents'
import { useResource } from '../composables/useResource'

const props = defineProps<{ projectId: string; runId: string }>()
const outgoing = useResource<Delegation[]>(() => `${apiPath('runs', props.projectId, props.runId)}/delegations`)
const agents = useResource<Agent[]>(() => apiPath('agents', props.projectId))
const incoming = ref<Delegation>()
const incomingError = ref<ApiError>()
let incomingGeneration = 0

const agentName = (id: string) => agents.data.value?.find(agent => agent.id === id)?.name || id
const hasDelegation = computed(() => Boolean(incoming.value || outgoing.data.value?.length))

async function loadIncoming() {
  const generation = ++incomingGeneration
  incomingError.value = undefined
  try {
    const result = await apiRequest<Delegation>(`${apiPath('runs', props.projectId, props.runId)}/delegation`)
    if (generation !== incomingGeneration) return
    incoming.value = result
  } catch (failure) {
    if (generation !== incomingGeneration) return
    const error = failure as ApiError
    if (error.status === 404) {
      incoming.value = undefined
      return
    }
    incoming.value = undefined
    incomingError.value = error
  }
}

async function refreshDelegation() {
  await Promise.all([outgoing.refresh(), agents.refresh(), loadIncoming()])
}

onMounted(loadIncoming)
watch(() => [props.projectId, props.runId], loadIncoming)
onBeforeUnmount(() => { incomingGeneration++ })
useProjectEvents(() => props.projectId, async event => {
  if (!event.type.startsWith('delegation.') && event.runId !== props.runId) return
  await refreshDelegation()
})
</script>

<template>
  <UCard v-if="hasDelegation || outgoing.error.value || incomingError" data-run-delegation>
    <h2 class="section-label mb-3">Delegation</h2>
    <UAlert
      v-if="outgoing.error.value || incomingError"
      title="Delegation details unavailable"
      :description="(outgoing.error.value || incomingError)?.message"
      color="error"
      class="mb-3"
    />

    <div v-if="incoming" class="space-y-2 text-sm">
      <p class="font-medium">Delegated subtask</p>
      <p>Parent Agent: {{ agentName(incoming.parentAgentId) }}</p>
      <p>Task: {{ incoming.task }}</p>
      <p>Workspace access: {{ incoming.workspaceAccess }}</p>
      <NuxtLink
        :to="`/projects/${projectId}/runs/${incoming.parentRunId}`"
        class="hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
      >
        Open parent Run
      </NuxtLink>
    </div>

    <div v-for="delegation in outgoing.data.value || []" :key="delegation.id" class="mt-4 space-y-2 border-t border-default pt-4 text-sm first:mt-0 first:border-t-0 first:pt-0">
      <p class="font-medium">Delegated to {{ agentName(delegation.targetAgentId) }}</p>
      <p>{{ delegation.task }}</p>
      <p>Run: {{ delegation.delegatedRunStatus }} · outcome {{ delegation.outcome || 'pending' }}</p>
      <p>Workspace access: {{ delegation.workspaceAccess }}</p>
      <p v-if="delegation.resultSummary">Result: {{ delegation.resultSummary }}</p>
      <p v-if="delegation.workspaceChangesAccepted !== null">
        Workspace changes accepted: {{ delegation.workspaceChangesAccepted ? 'yes' : 'no' }}
      </p>
      <p v-if="delegation.workspaceRevision" class="font-mono break-all">Revision: {{ delegation.workspaceRevision }}</p>
      <NuxtLink
        :to="`/projects/${projectId}/runs/${delegation.delegatedRunId}`"
        class="hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
      >
        Open delegated Run evidence
      </NuxtLink>
    </div>
  </UCard>
</template>
